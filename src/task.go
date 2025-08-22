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

	pb "github.com/velox0/moonlight-server/moonlight_server_proto"
)

type Task struct {
	ID      string          `json:"id"`
	Region  string          `json:"region"`
	Payload json.RawMessage `json:"payload"`
}

// Global gRPC server instance
var grpcServer *GRPCServer

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

	// Assign a server-side ID
	task.ID = generateTaskID()

	// Check if client is connected via gRPC
	if grpcServer != nil && grpcServer.IsGRPCClient(client) {
		// Handle via gRPC
		result, err := handleGRPCTask(client, &task)
		if err != nil {
			http.Error(w, fmt.Sprintf("gRPC task failed: %v", err), http.StatusBadGateway)
			return
		}

		// Return gRPC result
		w.Header().Set("Content-Type", "application/json")
		if result.Err != "" {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error": %q}`, result.Err)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write(result.Data)
		}

		fmt.Printf("TASK GRPC id=%s to client %s region %s success=%t\n",
			task.ID, clientKey(client.IP, client.NodeID), client.Region, result.Err == "")

	} else {
		// Handle via HTTP (existing logic)
		err := handleHTTPTask(client, &task, w)
		if err != nil {
			fmt.Printf("TASK HTTP id=%s to client %s region %s error=%v\n",
				task.ID, clientKey(client.IP, client.NodeID), client.Region, err)
		}
	}
}

// handleGRPCTask sends a task to a gRPC client
func handleGRPCTask(client *ClientInfo, task *Task) (*pb.TaskResult, error) {
	// Convert JSON task to protobuf task
	pbTask := &pb.Task{
		Id:      task.ID,
		Region:  task.Region,
		Payload: []byte(task.Payload),
	}

	// Send task and wait for result
	return grpcServer.SendTaskToGRPCClient(client, pbTask)
}

// handleHTTPTask sends a task to an HTTP client (existing logic)
func handleHTTPTask(client *ClientInfo, task *Task, w http.ResponseWriter) error {
	// Forward only the payload to the client's /work endpoint
	forwardBody, _ := json.Marshal(task.Payload)
	url := fmt.Sprintf("http://%s:%d/work", client.IP, client.Port)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(forwardBody))
	if err != nil {
		http.Error(w, "failed to build request", http.StatusInternalServerError)
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	clientHTTP := &http.Client{Timeout: 15 * time.Second}

	resp, err := clientHTTP.Do(req)
	if err != nil {
		http.Error(w, "client unreachable", http.StatusBadGateway)
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	fmt.Printf("TASK HTTP id=%s to client %s region %s status=%d\n",
		task.ID, clientKey(client.IP, client.NodeID), client.Region, resp.StatusCode)

	// Forward response
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	return nil
}
