package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
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

func setWSWriteDeadline(conn *websocket.Conn) {
	cfg := getConfig()
	if cfg == nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(time.Duration(cfg.WS.WriteTimeout) * time.Second))
}

func writeWSMessage(conn *websocket.Conn, message WebSocketMessage) error {
	setWSWriteDeadline(conn)
	return conn.WriteJSON(message)
}

func websocketRemoteIP(conn *websocket.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

// WebSocket heartbeat handler
func wsHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	cfg := getConfig()
	if cfg == nil || !cfg.WS.Enabled {
		http.Error(w, "WebSocket is disabled", http.StatusServiceUnavailable)
		return
	}

	wsMutex.RLock()
	currentConnections := len(wsConnections)
	wsMutex.RUnlock()
	if currentConnections >= cfg.WS.MaxConnections {
		http.Error(w, "Maximum WebSocket connections reached", http.StatusServiceUnavailable)
		return
	}

	upgrader := getWebSocketUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(time.Duration(cfg.WS.ReadTimeout) * time.Second))
	conn.SetWriteDeadline(time.Now().Add(time.Duration(cfg.WS.WriteTimeout) * time.Second))
	handleWebSocketConnection(conn)
}

// Connection health checker
func startConnectionHealthChecker() {
	go func() {
		for {
			cfg := getConfig()
			interval := 60
			if cfg != nil && cfg.WS.ConnectionCheckInterval > 0 {
				interval = cfg.WS.ConnectionCheckInterval
			}
			time.Sleep(time.Duration(interval) * time.Second)
			checkWebSocketConnections()
		}
	}()
}

func checkWebSocketConnections() {
	wsMutex.Lock()
	now := time.Now()
	timeoutSeconds := 300
	if cfg := getConfig(); cfg != nil && cfg.WS.ReadTimeout > 0 {
		timeoutSeconds = cfg.WS.ReadTimeout
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	var staleClients []*WebSocketClientInfo
	var staleKeys []string
	for key, wsClient := range wsConnections {
		if !wsClient.Connected || now.Sub(wsClient.LastHeartbeat) > timeout {
			wsClient.Connected = false
			wsClient.Conn.Close()
			delete(wsConnections, key)
			staleClients = append(staleClients, wsClient)
			staleKeys = append(staleKeys, key)
		}
	}
	wsMutex.Unlock()

	for idx, key := range staleKeys {
		cleanupDisconnectedWSClient(staleClients[idx], key)
		log.Printf("Removed disconnected/timed-out client: %s", key)
	}
}

func cleanupDisconnectedWSClient(wsClient *WebSocketClientInfo, key string) {
	if wsClient != nil {
		wsClient.Mutex.Lock()
		wsClient.Valid = false
		wsClient.Mutex.Unlock()
	}

	clientsMutex.Lock()
	if existing, exists := clients[key]; exists {
		existing.Mutex.Lock()
		isWS := existing.Protocol == ProtocolWS
		existing.Mutex.Unlock()
		if isWS {
			delete(clients, key)
		}
	}
	clientsMutex.Unlock()
}

// Handles a WebSocket connection
func handleWebSocketConnection(conn *websocket.Conn) {
	var clientInfo *WebSocketClientInfo
	for {
		if cfg := getConfig(); cfg != nil {
			if err := conn.SetReadDeadline(time.Now().Add(time.Duration(cfg.WS.ReadTimeout) * time.Second)); err != nil {
				log.Printf("Failed to set read deadline: %v", err)
				break
			}
		}

		var msg WebSocketMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if clientInfo != nil {
				key := clientKey(clientInfo.IP, clientInfo.NodeID)
				wsMutex.Lock()
				delete(wsConnections, key)
				wsMutex.Unlock()
				cleanupDisconnectedWSClient(clientInfo, key)
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
	var req WebSocketRegistrationRequest
	err := json.Unmarshal(msg.Payload, &req)
	if err != nil || req.Region == "" || req.Token == "" {
		sendError(conn, "invalid registration request")
		return nil
	}
	if !validToken(req.Token) {
		sendError(conn, "unauthorized token")
		return nil
	}
	remoteIP := websocketRemoteIP(conn)

	clientsMutex.Lock()
	nodeID := assignNodeID()

	key := clientKey(remoteIP, nodeID)
	client, exists := clients[key]
	if !exists {
		client = &ClientInfo{
			IP: remoteIP, NodeID: nodeID, Token: req.Token,
			Region: req.Region, Port: req.ListenPort, Valid: true, Protocol: ProtocolWS,
		}
		clients[key] = client
	} else {
		client.Mutex.Lock()
		client.Region, client.Token, client.Valid = req.Region, req.Token, true
		if req.ListenPort > 0 {
			client.Port = req.ListenPort
		}
		client.IP = remoteIP
		client.Protocol = ProtocolWS
		client.ConsecutiveFailures = 0
		client.UnresponsiveUntil = time.Time{}
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

	registeredPayload, marshalErr := json.Marshal(WebSocketRegisteredResponse{
		Status: "ok",
		NodeID: client.NodeID,
	})
	if marshalErr != nil {
		registeredPayload = json.RawMessage(`{"status": "ok"}`)
	}
	response := WebSocketMessage{
		Type:    "registered",
		Payload: registeredPayload,
	}
	if err := writeWSMessage(wsClientInfo.Conn, response); err != nil {
		log.Printf("Failed to send registered ack to client %s: %v", key, err)
	}
	log.Printf("Client registered via WebSocket: %s", key)
	return wsClientInfo
}

func handleClientHeartbeat(clientInfo *WebSocketClientInfo, _ WebSocketMessage) {
	if clientInfo == nil {
		return
	}
	now := time.Now()
	clientInfo.Mutex.Lock()
	clientInfo.LastSeen = now
	clientInfo.ConsecutiveFailures = 0
	clientInfo.UnresponsiveUntil = time.Time{}
	clientInfo.Mutex.Unlock()
	clientInfo.LastHeartbeat = now
	response := WebSocketMessage{
		Type: "heartbeat_ack",
	}
	if err := writeWSMessage(clientInfo.Conn, response); err != nil {
		log.Printf("Failed to send heartbeat ack to client %s: %v", clientKey(clientInfo.IP, clientInfo.NodeID), err)
	}
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
	errResp := WebSocketErrorResponse{Message: message}
	payload, _ := json.Marshal(errResp)
	response := WebSocketMessage{
		Type:    "error",
		Payload: payload,
	}
	if err := writeWSMessage(conn, response); err != nil {
		log.Printf("Failed to send websocket error response: %v", err)
	}
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

	// Marshal payload to JSON
	payloadBytes, err := json.Marshal(task.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %v", err)
	}

	taskMsg := WebSocketMessage{
		Type:    "task",
		TaskID:  task.ID,
		Payload: json.RawMessage(payloadBytes),
	}
	err = writeWSMessage(wsClientInfo.Conn, taskMsg)
	if err != nil {
		wsClientInfo.Connected = false
		log.Printf("Failed to send task to client %s: %v", key, err)
		return fmt.Errorf("failed to send task to client: %v", err)
	}
	log.Printf("Task %s sent to client %s via WebSocket", task.ID, key)
	return nil
}
