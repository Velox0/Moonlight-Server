package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type Task struct {
	ID      string      `json:"id"`
	Region  string      `json:"region"`
	Payload interface{} `json:"payload"`
}

// TaskResponse represents a response from a client for a specific task
type TaskResponse struct {
	TaskID   string            `json:"task_id"`
	Response interface{}       `json:"response"`
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers,omitempty"`
}

var (
	pendingTasks = make(map[string]chan *TaskResponse) // taskID -> response channel
	taskMutex    sync.RWMutex
)

// generateTaskID returns a random hex ID
func generateTaskID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// taskRequestHandler handles task requests from clients
// @Summary      Request a task
// @Description  Returns direct P2P peer connection info for a suitable client. Caller connects to the selected client directly over TCP/HTTP.
// @Tags         Task
// @Accept       json
// @Produce      json
// @Param        body  body      TaskRequest   true  "Task request payload"
// @Success      200   {object}  P2PTaskResponse
// @Failure      400   {object}  ErrorResponse
// @Failure      503   {object}  ErrorResponse
// @Router       /api/request [post]
func taskRequestHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req TaskRequest
	body, _ := io.ReadAll(r.Body)
	err := json.Unmarshal(body, &req)
	if err != nil {
		http.Error(w, "invalid task", http.StatusBadRequest)
		return
	}

	task := Task{Region: req.Region, Payload: req.Payload}

	client := selectClientForRegion(task.Region)
	if client == nil {
		http.Error(w, "no suitable client available", http.StatusServiceUnavailable)
		return
	}

	if client.Port <= 0 {
		http.Error(w, "selected client is not advertising a TCP port", http.StatusServiceUnavailable)
		return
	}

	// Assign a server-side ID
	task.ID = generateTaskID()

	peerURL := fmt.Sprintf("http://%s:%d/work", client.IP, client.Port)
	resp := P2PTaskResponse{
		Mode:   "p2p-tcp",
		TaskID: task.ID,
		Region: client.Region,
		Peer: P2PPeerEndpoint{
			NodeID:   client.NodeID,
			IP:       client.IP,
			Port:     client.Port,
			Protocol: "tcp",
			URL:      peerURL,
		},
		TTLSeconds: 30,
	}

	fmt.Printf("P2P MATCH id=%s peer=%s region=%s endpoint=%s\n", task.ID, clientKey(client.IP, client.NodeID), client.Region, peerURL)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// sendTaskViaHTTP sends task via HTTP (fallback method)
func sendTaskViaHTTP(client *ClientInfo, task Task) error {
	url := fmt.Sprintf("http://%s:%d/work", client.IP, client.Port)

	// Marshal payload to JSON
	payloadBytes, err := json.Marshal(task.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Task-ID", task.ID)

	clientHTTP := &http.Client{Timeout: 15 * time.Second}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("client unreachable: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, _ := io.ReadAll(resp.Body)

	// Create task response
	taskResponse := &TaskResponse{
		TaskID:   task.ID,
		Response: respBody,
		Status:   resp.StatusCode,
		Headers:  make(map[string]string),
	}

	// Copy headers
	for key, values := range resp.Header {
		if len(values) > 0 {
			taskResponse.Headers[key] = values[0]
		}
	}

	// Send response to waiting handler
	taskMutex.RLock()
	if responseChan, exists := pendingTasks[task.ID]; exists {
		// Use select to avoid sending on closed channel
		select {
		case responseChan <- taskResponse:
			// Successfully sent
		default:
			// Channel is full or closed, ignore
		}
	}
	taskMutex.RUnlock()

	return nil
}

// StoreTaskResponse stores a response for a pending task
func StoreTaskResponse(taskID string, response *TaskResponse) {
	taskMutex.RLock()
	if responseChan, exists := pendingTasks[taskID]; exists {
		// Use select to avoid sending on closed channel
		select {
		case responseChan <- response:
			log.Printf("Successfully stored response for task %s", taskID)
		default:
			log.Printf("Failed to store response for task %s - channel full or closed", taskID)
		}
	} else {
		log.Printf("No pending task found for taskID %s", taskID)
	}
	taskMutex.RUnlock()
}
