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
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for now
		},
	}
	wsConnections = make(map[string]*WebSocketClientInfo) // key: clientKey
	wsMutex       sync.RWMutex
)

// WebSocketClientInfo extends ClientInfo with WebSocket connection and state
type WebSocketClientInfo struct {
	*ClientInfo
	Conn           *websocket.Conn
	LastHeartbeat  time.Time
	Connected      bool
	ReconnectCount int
	MaxReconnects  int
}

// getWebSocketUpgrader returns a configured WebSocket upgrader based on config
func getWebSocketUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for now
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}

// WebSocketMessage represents messages sent between server and clients
type WebSocketMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	TaskID  string          `json:"task_id,omitempty"`
}

// WebSocket heartbeat handler
func wsHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	// Check if WebSocket is enabled
	if !config.WS.Enabled {
		http.Error(w, "WebSocket is disabled", http.StatusServiceUnavailable)
		return
	}

	// Check connection limit
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

	// Set timeouts with more reasonable values
	conn.SetReadDeadline(time.Now().Add(time.Duration(config.WS.ReadTimeout) * time.Second))
	conn.SetWriteDeadline(time.Now().Add(time.Duration(config.WS.WriteTimeout) * time.Second))

	// Handle WebSocket connection
	handleWebSocketConnection(conn)
}

// Start connection health checker
func startConnectionHealthChecker() {
	ticker := time.NewTicker(time.Duration(config.WS.ConnectionCheckInterval) * time.Second)
	go func() {
		for range ticker.C {
			checkWebSocketConnections()
		}
	}()
}

// Check and clean up stale WebSocket connections
func checkWebSocketConnections() {
	wsMutex.Lock()
	defer wsMutex.Unlock()

	now := time.Now()
	timeout := time.Duration(config.WS.ReadTimeout) * time.Second

	for key, wsClient := range wsConnections {
		if !wsClient.Connected {
			delete(wsConnections, key)
			log.Printf("Removed disconnected client: %s", key)
			continue
		}

		// Check if client has been silent for too long
		if now.Sub(wsClient.LastHeartbeat) > timeout {
			log.Printf("Client %s timed out, marking as disconnected", key)
			wsClient.Connected = false
			wsClient.Conn.Close()
			delete(wsConnections, key)
		}
	}
}

// Handle individual WebSocket connection
func handleWebSocketConnection(conn *websocket.Conn) {
	var clientInfo *WebSocketClientInfo

	for {
		var msg WebSocketMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if clientInfo != nil {
				// Remove client from active connections
				key := clientKey(clientInfo.IP, clientInfo.NodeID)
				wsMutex.Lock()
				delete(wsConnections, key)
				wsMutex.Unlock()

				// Mark client as invalid
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

		// Debug: Log that we're continuing to listen
		if clientInfo != nil {
			log.Printf("Continuing to listen for messages from client %s", clientKey(clientInfo.IP, clientInfo.NodeID))
		}
	}
}

// Handle client registration via WebSocket
func handleClientRegistration(conn *websocket.Conn, msg WebSocketMessage) *WebSocketClientInfo {
	var req struct {
		IP     string `json:"ip"`
		NodeID string `json:"node_id"`
		Token  string `json:"token"`
		Region string `json:"region"`
		Port   int    `json:"port"`
	}

	err := json.Unmarshal(msg.Payload, &req)
	if err != nil || req.IP == "" || req.NodeID == "" || req.Region == "" || req.Token == "" {
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

	key := clientKey(req.IP, req.NodeID)

	// Create or update client info
	clientsMutex.Lock()
	client, exists := clients[key]
	if !exists {
		client = &ClientInfo{
			IP:       req.IP,
			NodeID:   req.NodeID,
			Token:    req.Token,
			Region:   req.Region,
			Port:     req.Port,
			Valid:    true,
			LastSeen: time.Now(),
		}
		clients[key] = client
	} else {
		client.Mutex.Lock()
		client.Region = req.Region
		client.Token = req.Token
		client.Port = req.Port
		client.Valid = true
		client.LastSeen = time.Now()
		client.Mutex.Unlock()
	}
	clientsMutex.Unlock()

	// Create WebSocket client info
	wsClientInfo := &WebSocketClientInfo{
		ClientInfo:     client,
		Conn:           conn,
		LastHeartbeat:  time.Now(),
		Connected:      true,
		ReconnectCount: 0,
		MaxReconnects:  5,
	}

	// Store WebSocket connection
	wsMutex.Lock()
	wsConnections[key] = wsClientInfo
	wsMutex.Unlock()

	// Send registration confirmation
	response := WebSocketMessage{
		Type:    "registered",
		Payload: json.RawMessage(`{"status": "ok"}`),
	}
	wsClientInfo.Conn.WriteJSON(response)

	log.Printf("Client registered via WebSocket: %s", key)
	return wsClientInfo
}

// Handle client heartbeat via WebSocket
func handleClientHeartbeat(clientInfo *WebSocketClientInfo, msg WebSocketMessage) {
	if clientInfo == nil {
		return
	}

	now := time.Now()

	// Update both LastSeen and LastHeartbeat
	clientInfo.Mutex.Lock()
	clientInfo.LastSeen = now
	clientInfo.Mutex.Unlock()

	clientInfo.LastHeartbeat = now

	// Send heartbeat response
	response := WebSocketMessage{
		Type:    "heartbeat_ack",
		Payload: json.RawMessage(`{"timestamp": "` + now.Format(time.RFC3339) + `"}`),
	}
	clientInfo.Conn.WriteJSON(response)

	log.Printf("Heartbeat from client %s", clientKey(clientInfo.IP, clientInfo.NodeID))
}

// Handle task response from client
func handleTaskResponse(clientInfo *WebSocketClientInfo, msg WebSocketMessage) {
	if clientInfo == nil {
		log.Printf("handleTaskResponse called with nil clientInfo")
		return
	}

	// Parse task response
	var taskResp TaskResponse
	err := json.Unmarshal(msg.Payload, &taskResp)
	if err != nil {
		log.Printf("Invalid task response format from client %s: %v", clientKey(clientInfo.IP, clientInfo.NodeID), err)
		return
	}

	// Store task response for the original requester
	StoreTaskResponse(taskResp.TaskID, &taskResp)
	log.Printf("Received task response for task %s from client %s", taskResp.TaskID, clientKey(clientInfo.IP, clientInfo.NodeID))
}

// Send error message to WebSocket client
func sendError(conn *websocket.Conn, message string) {
	response := WebSocketMessage{
		Type:    "error",
		Payload: json.RawMessage(`{"message": "` + message + `"}`),
	}
	conn.WriteJSON(response)
}

// Send task to client via WebSocket
func sendTaskToClient(client *ClientInfo, task Task) error {
	key := clientKey(client.IP, client.NodeID)

	wsMutex.RLock()
	wsClientInfo, exists := wsConnections[key]
	wsMutex.RUnlock()

	if !exists {
		return fmt.Errorf("no WebSocket connection for client %s", key)
	}

	// Check if connection is still valid
	if !wsClientInfo.Connected {
		return fmt.Errorf("WebSocket connection for client %s is not connected", key)
	}

	// Create task message
	taskMsg := WebSocketMessage{
		Type:    "task",
		TaskID:  task.ID,
		Payload: json.RawMessage(task.Payload),
	}

	// Send task to client
	err := wsClientInfo.Conn.WriteJSON(taskMsg)
	if err != nil {
		// Mark connection as disconnected
		wsClientInfo.Connected = false
		log.Printf("Failed to send task to client %s: %v", key, err)
		return fmt.Errorf("failed to send task to client: %v", err)
	}

	log.Printf("Task %s sent to client %s via WebSocket", task.ID, key)
	return nil
}
