package main

import (
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/velox0/moonlight-server/moonlight_server_proto"
)

// GRPCServer implements the Moonlight gRPC service
type GRPCServer struct {
	pb.UnimplementedMoonlightServer

	// Track active gRPC client connections
	grpcClients      map[string]*GRPCClientConn
	grpcClientsMutex sync.RWMutex

	// Task queues for each client
	taskQueues      map[string]chan *pb.Task
	taskQueuesMutex sync.RWMutex
}

// GRPCClientConn represents an active gRPC client connection
type GRPCClientConn struct {
	Stream     pb.Moonlight_ConnectServer
	ClientInfo *ClientInfo
	TaskQueue  chan *pb.Task
	ResultChan chan *pb.TaskResult
	Done       chan struct{}
}

// NewGRPCServer creates a new gRPC server instance
func NewGRPCServer() *GRPCServer {
	return &GRPCServer{
		grpcClients: make(map[string]*GRPCClientConn),
		taskQueues:  make(map[string]chan *pb.Task),
	}
}

// Connect implements the bidirectional streaming RPC
func (s *GRPCServer) Connect(stream pb.Moonlight_ConnectServer) error {
	ctx := stream.Context()

	// Wait for initial ClientHello message
	msg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "failed to receive hello: %v", err)
	}

	hello := msg.GetHello()
	if hello == nil {
		return status.Errorf(codes.InvalidArgument, "first message must be ClientHello")
	}

	// Validate the client hello
	if hello.Ip == "" || hello.NodeId == "" || hello.Region == "" || hello.Token == "" {
		return status.Errorf(codes.InvalidArgument, "missing required fields in ClientHello")
	}

	if !validToken(hello.Token) {
		return status.Errorf(codes.Unauthenticated, "invalid token")
	}

	// Create client info
	clientKey := clientKey(hello.Ip, hello.NodeId)
	port := int(hello.Port)
	if port == 0 {
		port = 3000
	}

	clientInfo := &ClientInfo{
		IP:       hello.Ip,
		NodeID:   hello.NodeId,
		Token:    hello.Token,
		Region:   hello.Region,
		Port:     port,
		LastSeen: time.Now(),
		Valid:    true,
	}

	// Create gRPC client connection
	taskQueue := make(chan *pb.Task, 100) // Buffer for queued tasks
	resultChan := make(chan *pb.TaskResult, 10)
	done := make(chan struct{})

	grpcConn := &GRPCClientConn{
		Stream:     stream,
		ClientInfo: clientInfo,
		TaskQueue:  taskQueue,
		ResultChan: resultChan,
		Done:       done,
	}

	// Register the client
	s.registerGRPCClient(clientKey, grpcConn, clientInfo)

	log.Printf("gRPC client connected: %s region=%s", clientKey, hello.Region)

	// Handle the connection
	defer func() {
		s.unregisterGRPCClient(clientKey)
		close(done)
		close(taskQueue)
		close(resultChan)
		log.Printf("gRPC client disconnected: %s", clientKey)
	}()

	// Start goroutines for sending tasks and receiving results
	errChan := make(chan error, 2)

	// Goroutine to send tasks to client
	go func() {
		for {
			select {
			case task := <-taskQueue:
				msg := &pb.TaskStreamMessage{
					Msg: &pb.TaskStreamMessage_Task{Task: task},
				}
				if err := stream.Send(msg); err != nil {
					errChan <- fmt.Errorf("failed to send task: %v", err)
					return
				}
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()

	// Goroutine to receive results from client
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					errChan <- nil // Clean shutdown
				} else {
					errChan <- fmt.Errorf("failed to receive message: %v", err)
				}
				return
			}

			switch m := msg.Msg.(type) {
			case *pb.TaskStreamMessage_Result:
				// Forward result to waiting HTTP handler
				select {
				case resultChan <- m.Result:
				case <-time.After(5 * time.Second):
					log.Printf("Result timeout for task %s", m.Result.Id)
				}
			case *pb.TaskStreamMessage_Hello:
				// Ignore additional hello messages
			default:
				log.Printf("Unknown message type from client %s", clientKey)
			}
		}
	}()

	// Update last seen periodically
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case err := <-errChan:
			return err
		case <-ticker.C:
			// Update last seen time
			clientInfo.Mutex.Lock()
			clientInfo.LastSeen = time.Now()
			clientInfo.Mutex.Unlock()
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return nil
		}
	}
}

// registerGRPCClient registers a new gRPC client connection
func (s *GRPCServer) registerGRPCClient(key string, conn *GRPCClientConn, clientInfo *ClientInfo) {
	// Register in gRPC clients map
	s.grpcClientsMutex.Lock()
	s.grpcClients[key] = conn
	s.grpcClientsMutex.Unlock()

	// Register in task queues
	s.taskQueuesMutex.Lock()
	s.taskQueues[key] = conn.TaskQueue
	s.taskQueuesMutex.Unlock()

	// Also register in the existing HTTP clients map for compatibility
	clientsMutex.Lock()
	clients[key] = clientInfo
	clientsMutex.Unlock()
}

// unregisterGRPCClient removes a gRPC client connection
func (s *GRPCServer) unregisterGRPCClient(key string) {
	// Remove from gRPC clients map
	s.grpcClientsMutex.Lock()
	delete(s.grpcClients, key)
	s.grpcClientsMutex.Unlock()

	// Remove from task queues
	s.taskQueuesMutex.Lock()
	delete(s.taskQueues, key)
	s.taskQueuesMutex.Unlock()

	// Mark as invalid in HTTP clients map (don't remove for compatibility)
	clientsMutex.Lock()
	if client, exists := clients[key]; exists {
		client.Mutex.Lock()
		client.Valid = false
		client.Mutex.Unlock()
	}
	clientsMutex.Unlock()
}

// SendTaskToGRPCClient sends a task to a gRPC client and waits for the result
func (s *GRPCServer) SendTaskToGRPCClient(clientInfo *ClientInfo, task *pb.Task) (*pb.TaskResult, error) {
	key := clientKey(clientInfo.IP, clientInfo.NodeID)

	s.grpcClientsMutex.RLock()
	conn, exists := s.grpcClients[key]
	s.grpcClientsMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("gRPC client not connected")
	}

	// Send task to client
	select {
	case conn.TaskQueue <- task:
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("task queue timeout")
	}

	// Wait for result
	select {
	case result := <-conn.ResultChan:
		if result.Id == task.Id {
			return result, nil
		}
		// If IDs don't match, put it back and wait again (simple approach)
		select {
		case conn.ResultChan <- result:
		default:
		}
		return nil, fmt.Errorf("result ID mismatch")
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("task execution timeout")
	}
}

// IsGRPCClient checks if a client is connected via gRPC
func (s *GRPCServer) IsGRPCClient(clientInfo *ClientInfo) bool {
	key := clientKey(clientInfo.IP, clientInfo.NodeID)

	s.grpcClientsMutex.RLock()
	_, exists := s.grpcClients[key]
	s.grpcClientsMutex.RUnlock()

	return exists
}

// GetGRPCClientCount returns the number of active gRPC connections
func (s *GRPCServer) GetGRPCClientCount() int {
	s.grpcClientsMutex.RLock()
	count := len(s.grpcClients)
	s.grpcClientsMutex.RUnlock()
	return count
}
