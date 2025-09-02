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
	ID      string          `json:"id"`
	Region  string          `json:"region"`
	Payload json.RawMessage `json:"payload"`
}

// TaskResponse represents a response from a client for a specific task
type TaskResponse struct {
	TaskID   string            `json:"task_id"`
	Response json.RawMessage   `json:"response"`
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

func taskRequestHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var task Task
	body, _ := io.ReadAll(r.Body)
	err := json.Unmarshal(body, &task)
	if err != nil {
		http.Error(w, "invalid task", http.StatusBadRequest)
		return
	}

	client := selectClientForRegion(task.Region)
	if client == nil {
		http.Error(w, "no suitable client available", http.StatusServiceUnavailable)
		return
	}

	// Assign a server-side ID
	task.ID = generateTaskID()

	// Create response channel for this task
	responseChan := make(chan *TaskResponse, 1)
	taskMutex.Lock()
	pendingTasks[task.ID] = responseChan
	taskMutex.Unlock()

	// Clean up response channel after timeout
	go func() {
		time.Sleep(30 * time.Second) // 30 second timeout
		taskMutex.Lock()
		if _, exists := pendingTasks[task.ID]; exists {
			delete(pendingTasks, task.ID)
			// Use select to avoid closing already closed channel
			select {
			case <-responseChan:
				// Channel already closed, do nothing
			default:
				close(responseChan)
			}
		}
		taskMutex.Unlock()
	}()

	// Try to send task via WebSocket first
	err = sendTaskToClient(client, task)
	if err != nil {
		// Fallback to HTTP if WebSocket fails
		err = sendTaskViaHTTP(client, task)
		if err != nil {
			// Clean up and return error
			taskMutex.Lock()
			if _, exists := pendingTasks[task.ID]; exists {
				delete(pendingTasks, task.ID)
				// Use select to avoid closing already closed channel
				select {
				case <-responseChan:
					// Channel already closed, do nothing
				default:
					close(responseChan)
				}
			}
			taskMutex.Unlock()

			http.Error(w, "client unreachable", http.StatusBadGateway)
			return
		}
	}

	fmt.Printf("TASK PROXIED id=%s to client %s region %s\n", task.ID, clientKey(client.IP, client.NodeID), client.Region)

	// Wait for response
	select {
	case response := <-responseChan:
		// Clean up the task from pending tasks
		taskMutex.Lock()
		delete(pendingTasks, task.ID)
		taskMutex.Unlock()

		// Set response headers
		for key, value := range response.Headers {
			w.Header().Set(key, value)
		}

		// Set default content type if not provided
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}

		w.WriteHeader(response.Status)
		w.Write(response.Response)

	case <-time.After(30 * time.Second):
		// Timeout
		taskMutex.Lock()
		delete(pendingTasks, task.ID)
		taskMutex.Unlock()

		http.Error(w, "task timeout", http.StatusGatewayTimeout)
	}
}

// sendTaskViaHTTP sends task via HTTP (fallback method)
func sendTaskViaHTTP(client *ClientInfo, task Task) error {
	url := fmt.Sprintf("http://%s:%d/work", client.IP, client.Port)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(task.Payload))
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
