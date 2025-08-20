package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Task struct {
	ID      string          `json:"id"`
	Region  string          `json:"region"`
	Payload json.RawMessage `json:"payload"`
}

// generateTaskID returns a random hex ID
func generateTaskID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func taskRequestHandler(w http.ResponseWriter, r *http.Request) {
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
	// Assign a server-side ID and forward to the client's /work endpoint
	task.ID = generateTaskID()
	forwardBody, _ := json.Marshal(task.Payload)
	url := fmt.Sprintf("http://%s:%d/work", client.IP, client.Port)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(forwardBody))
	if err != nil {
		http.Error(w, "failed to build request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	clientHTTP := &http.Client{Timeout: 15 * time.Second}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		http.Error(w, "client unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("TASK PROXIED id=%s to client %s region %s status=%d\n", task.ID, clientKey(client.IP, client.NodeID), client.Region, resp.StatusCode)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}
