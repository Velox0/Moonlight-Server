package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	wsConnections = make(map[string]*WebSocketClientInfo) // key: clientKey
	wsMutex       sync.RWMutex
)

type WebSocketClientInfo struct {
	*ClientInfo
	Conn           *websocket.Conn
	LastHeartbeat  time.Time
	Connected      bool
	ReconnectCount int
	MaxReconnects  int
}

func getWebSocketUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin:     func(r *http.Request) bool { return true },
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}

type WebSocketMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	TaskID  string          `json:"task_id,omitempty"`
}

// WebSocket heartbeat handler
func wsHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	if !config.WS.Enabled {
		http.Error(w, "WebSocket is disabled", http.StatusServiceUnavailable)
		return
	}

	wsMutex.RLock()
	currentConnections := len(wsConnections)
	wsMutex.RUnlock()
	if currentConnections >= config.WS.MaxConnections {
		http.Error(w, "Maximum WebSocket connections reached", http.StatusServiceUnavailable)
		return
	}

	upgrader := getWebSocketUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(time.Duration(config.WS.ReadTimeout) * time.Second))
	conn.SetWriteDeadline(time.Now().Add(time.Duration(config.WS.WriteTimeout) * time.Second))
	handleWebSocketConnection(conn)
}

// Connection health checker
func startConnectionHealthChecker() {
	ticker := time.NewTicker(time.Duration(config.WS.ConnectionCheckInterval) * time.Second)
	go func() {
		for range ticker.C {
			checkWebSocketConnections()
		}
	}()
}

func checkWebSocketConnections() {
	wsMutex.Lock()
	defer wsMutex.Unlock()
	now := time.Now()
	timeout := time.Duration(config.WS.ReadTimeout) * time.Second
	for key, wsClient := range wsConnections {
		if !wsClient.Connected || now.Sub(wsClient.LastHeartbeat) > timeout {
			wsClient.Connected = false
			wsClient.Conn.Close()
			delete(wsConnections, key)
			log.Printf("Removed disconnected/timed-out client: %s", key)
		}
	}
}

// Handles a WebSocket connection
func handleWebSocketConnection(conn *websocket.Conn) {
	var clientInfo *WebSocketClientInfo
	for {
		var msg WebSocketMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if clientInfo != nil {
				key := clientKey(clientInfo.IP, clientInfo.NodeID)
				wsMutex.Lock()
				delete(wsConnections, key)
				wsMutex.Unlock()
				clientInfo.Mutex.Lock()
				clientInfo.Valid = false
				clientInfo.Mutex.Unlock()
				log.Printf("Client disconnected: %s", key)
			}
			break
		}
		switch msg.Type {
		case "register":
			clientInfo = handleClientRegistration(conn, msg)
		case "heartbeat":
			handleClientHeartbeat(clientInfo, msg)
		case "task_response":
			handleTaskResponse(clientInfo, msg)
		default:
			log.Printf("Unknown message type: %s", msg.Type)
		}
	}
	// Note: connection closed
}

func handleClientRegistration(conn *websocket.Conn, msg WebSocketMessage) *WebSocketClientInfo {
	var req struct {
		IP     string `json:"ip"`
		NodeID string `json:"node_id"`
		Token  string `json:"token"`
		Region string `json:"region"`
		Port   int    `json:"port"`
	}
	err := json.Unmarshal(msg.Payload, &req)
	if err != nil || req.IP == "" || req.Region == "" || req.Token == "" {
		sendError(conn, "invalid registration request")
		return nil
	}
	if !validToken(req.Token) {
		sendError(conn, "unauthorized token")
		return nil
	}
	if req.Port == 0 {
		req.Port = 3000
	}
	if req.NodeID == "" {
		req.NodeID = assignNodeID()
	}
	key := clientKey(req.IP, req.NodeID)
	clientsMutex.Lock()
	client, exists := clients[key]
	if !exists {
		client = &ClientInfo{
			IP: req.IP, NodeID: req.NodeID, Token: req.Token,
			Region: req.Region, Port: req.Port, Valid: true, Protocol: ProtocolWS,
		}
		clients[key] = client
	} else {
		client.Mutex.Lock()
		client.Region, client.Token, client.Port, client.Valid = req.Region, req.Token, req.Port, true
		client.Protocol = ProtocolWS
		client.Mutex.Unlock()
	}
	client.Mutex.Lock()
	client.LastSeen = time.Now()
	client.Mutex.Unlock()
	clientsMutex.Unlock()
	wsClientInfo := &WebSocketClientInfo{
		ClientInfo:     client,
		Conn:           conn,
		LastHeartbeat:  time.Now(),
		Connected:      true,
		ReconnectCount: 0,
		MaxReconnects:  5,
	}
	wsMutex.Lock()
	wsConnections[key] = wsClientInfo
	wsMutex.Unlock()
	response := WebSocketMessage{
		Type:    "registered",
		Payload: json.RawMessage(`{"status": "ok"}`),
	}
	wsClientInfo.Conn.WriteJSON(response)
	log.Printf("Client registered via WebSocket: %s", key)
	return wsClientInfo
}

func handleClientHeartbeat(clientInfo *WebSocketClientInfo, msg WebSocketMessage) {
	if clientInfo == nil {
		return
	}
	now := time.Now()
	clientInfo.Mutex.Lock()
	clientInfo.LastSeen = now
	clientInfo.Mutex.Unlock()
	clientInfo.LastHeartbeat = now
	response := WebSocketMessage{
		Type:    "heartbeat_ack",
		Payload: json.RawMessage(`{"timestamp": "` + now.Format(time.RFC3339) + `"}`),
	}
	clientInfo.Conn.WriteJSON(response)
	log.Printf("Heartbeat from client %s", clientKey(clientInfo.IP, clientInfo.NodeID))
}

func handleTaskResponse(clientInfo *WebSocketClientInfo, msg WebSocketMessage) {
	if clientInfo == nil {
		log.Printf("handleTaskResponse called with nil clientInfo")
		return
	}
	var taskResp TaskResponse
	err := json.Unmarshal(msg.Payload, &taskResp)
	if err != nil {
		log.Printf("Invalid task response format from client %s: %v", clientKey(clientInfo.IP, clientInfo.NodeID), err)
		return
	}
	StoreTaskResponse(taskResp.TaskID, &taskResp)
	log.Printf("Received task response for task %s from client %s", taskResp.TaskID, clientKey(clientInfo.IP, clientInfo.NodeID))
}

func sendError(conn *websocket.Conn, message string) {
	response := WebSocketMessage{
		Type:    "error",
		Payload: json.RawMessage(`{"message": "` + message + `"}`),
	}
	conn.WriteJSON(response)
}

func sendTaskToClient(client *ClientInfo, task Task) error {
	key := clientKey(client.IP, client.NodeID)
	wsMutex.RLock()
	wsClientInfo, exists := wsConnections[key]
	wsMutex.RUnlock()
	if !exists {
		return fmt.Errorf("no WebSocket connection for client %s", key)
	}
	if !wsClientInfo.Connected {
		return fmt.Errorf("WebSocket connection for client %s is not connected", key)
	}
	taskMsg := WebSocketMessage{
		Type:    "task",
		TaskID:  task.ID,
		Payload: json.RawMessage(task.Payload),
	}
	err := wsClientInfo.Conn.WriteJSON(taskMsg)
	if err != nil {
		wsClientInfo.Connected = false
		log.Printf("Failed to send task to client %s: %v", key, err)
		return fmt.Errorf("failed to send task to client: %v", err)
	}
	log.Printf("Task %s sent to client %s via WebSocket", task.ID, key)
	return nil
}
