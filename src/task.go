package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	maxTaskRequestBytes = 8 * 1024 * 1024
	defaultTaskDeadline = 30 * time.Second
	maxRetriesLimit     = 10
	minBackoffMs        = 10
	maxBackoffMs        = 30000
)

var sendTaskToPeer = SendTaskToPeer

type taskAssignment struct {
	clientKey string
	expiresAt time.Time
}

var (
	taskAssignments     = make(map[string]taskAssignment)
	taskAssignmentsLock sync.Mutex
)

func trackTaskAssignment(taskID string, client *ClientInfo, ttl time.Duration) bool {
	if taskID == "" || client == nil {
		return false
	}
	if ttl <= 0 {
		ttl = defaultTaskDeadline
	}

	now := time.Now()
	key := clientKey(client.IP, client.NodeID)

	taskAssignmentsLock.Lock()
	if _, exists := taskAssignments[taskID]; exists {
		taskAssignmentsLock.Unlock()
		return false
	}
	taskAssignments[taskID] = taskAssignment{clientKey: key, expiresAt: now.Add(ttl)}
	taskAssignmentsLock.Unlock()

	client.Mutex.Lock()
	client.ActiveJobs++
	client.LastAssigned = now
	client.Mutex.Unlock()

	time.AfterFunc(ttl, func() {
		releaseTaskAssignment(taskID)
	})

	return true
}

func releaseTaskAssignment(taskID string) {
	if taskID == "" {
		return
	}

	taskAssignmentsLock.Lock()
	assignment, exists := taskAssignments[taskID]
	if !exists {
		taskAssignmentsLock.Unlock()
		return
	}
	delete(taskAssignments, taskID)
	taskAssignmentsLock.Unlock()

	clientsMutex.RLock()
	client := clients[assignment.clientKey]
	clientsMutex.RUnlock()
	if client == nil {
		return
	}
	client.Mutex.Lock()
	if client.ActiveJobs > 0 {
		client.ActiveJobs--
	}
	client.Mutex.Unlock()
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxTaskRequestBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("unexpected trailing data")
	}
	return nil
}

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

func taskRequestHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req TaskRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid task", http.StatusBadRequest)
		return
	}
	if req.Region == "" {
		http.Error(w, "region is required", http.StatusBadRequest)
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
	trackTaskAssignment(task.ID, client, 30*time.Second)

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

func quicTaskRequestHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req TaskRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid task", http.StatusBadRequest)
		return
	}
	if req.Region == "" {
		http.Error(w, "region is required", http.StatusBadRequest)
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

	// Calculate deadline: 30 seconds from now
	deadline := time.Now().Add(defaultTaskDeadline)
	deadlineNanos := deadline.UnixNano()

	peerURL := fmt.Sprintf("quic://%s:%d", client.IP, client.Port)
	resp := P2PQUICTaskResponse{
		Mode:     "p2p-quic",
		TaskID:   task.ID,
		Region:   client.Region,
		Deadline: deadlineNanos,
		Peer: P2PPeerEndpoint{
			NodeID:   client.NodeID,
			IP:       client.IP,
			Port:     client.Port,
			Protocol: "quic",
			URL:      peerURL,
		},
		TTLSeconds: 30,
	}
	trackTaskAssignment(task.ID, client, time.Duration(resp.TTLSeconds)*time.Second)

	fmt.Printf("P2P QUIC MATCH id=%s peer=%s region=%s endpoint=%s\n", task.ID, clientKey(client.IP, client.NodeID), client.Region, peerURL)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func quicTaskExecuteHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TaskID        string          `json:"task_id"`
		PeerIP        string          `json:"peer_ip"`
		PeerPort      int             `json:"peer_port"`
		DeadlineNanos int64           `json:"deadline_unix_nanos"`
		Payload       json.RawMessage `json:"payload"`
		MaxRetries    int             `json:"max_retries,omitempty"`
		BackoffMs     int             `json:"backoff_ms,omitempty"`
	}

	if err := decodeJSONBody(w, r, &req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.PeerIP == "" || req.PeerPort <= 0 {
		http.Error(w, "invalid peer address", http.StatusBadRequest)
		return
	}
	if net.ParseIP(req.PeerIP) == nil {
		http.Error(w, "invalid peer ip", http.StatusBadRequest)
		return
	}
	if req.TaskID == "" {
		req.TaskID = generateTaskID()
	}

	if req.MaxRetries == 0 {
		req.MaxRetries = 3
	}
	if req.MaxRetries < 0 {
		req.MaxRetries = 0
	}
	if req.MaxRetries > maxRetriesLimit {
		req.MaxRetries = maxRetriesLimit
	}
	if req.BackoffMs == 0 {
		req.BackoffMs = 100
	}
	if req.BackoffMs < minBackoffMs {
		req.BackoffMs = minBackoffMs
	}
	if req.BackoffMs > maxBackoffMs {
		req.BackoffMs = maxBackoffMs
	}

	// Create context with deadline
	ctx := r.Context()
	if req.DeadlineNanos <= 0 {
		req.DeadlineNanos = time.Now().Add(defaultTaskDeadline).UnixNano()
	}
	deadline := time.Unix(0, req.DeadlineNanos)
	if time.Now().After(deadline) {
		http.Error(w, "deadline already passed", http.StatusRequestTimeout)
		return
	}
	newCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ctx = newCtx

	assignedClient := findClientByEndpoint(req.PeerIP, req.PeerPort)
	shouldRelease := false
	if assignedClient != nil {
		ttl := time.Until(deadline)
		if trackTaskAssignment(req.TaskID, assignedClient, ttl) {
			shouldRelease = true
		}
	}
	if shouldRelease {
		defer releaseTaskAssignment(req.TaskID)
	}

	// Create task frame
	taskFrame := TaskFrame{
		TaskID:     req.TaskID,
		Deadline:   req.DeadlineNanos,
		Payload:    req.Payload,
		MaxRetries: req.MaxRetries,
		BackoffMs:  req.BackoffMs,
	}

	// Execute with retry logic
	var respFrame *ResponseFrame
	var lastErr error
	var err error

	for attempt := 0; attempt <= req.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := ExponentialBackoff(attempt-1, req.MaxRetries, req.BackoffMs)
			log.Printf("Task %s: retry %d/%d after %v", req.TaskID, attempt, req.MaxRetries, backoff)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				http.Error(w, "context deadline exceeded", http.StatusGatewayTimeout)
				return
			}
		}

		perAttemptTimeout := 15 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				http.Error(w, "task deadline exceeded", http.StatusGatewayTimeout)
				return
			}
			if remaining < perAttemptTimeout {
				perAttemptTimeout = remaining
			}
		}
		execCtx, cancel := context.WithTimeout(ctx, perAttemptTimeout)
		respFrame, err = sendTaskToPeer(execCtx, req.PeerIP, req.PeerPort, &taskFrame)
		cancel()

		if err == nil {
			markClientSuccess(assignedClient)
			// Success
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(respFrame.Status)
			w.Write(respFrame.Payload)
			return
		}

		lastErr = err
		markClientFailure(assignedClient)
		log.Printf("Task %s: peer call failed (attempt %d/%d): %v", req.TaskID, attempt+1, req.MaxRetries+1, err)

		if ctx.Err() != nil {
			http.Error(w, "task deadline exceeded", http.StatusGatewayTimeout)
			return
		}
	}

	log.Printf("Task %s: all retries exhausted", req.TaskID)
	http.Error(w, fmt.Sprintf("failed after %d retries: %v", req.MaxRetries+1, lastErr), http.StatusServiceUnavailable)
}
