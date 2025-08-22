package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	pb "github.com/velox0/moonlight-server/moonlight_server_proto"
)

var (
	config *Config
)

func main() {
	// Load config
	cfg, err := LoadConfig("/etc/moonlight/mls.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	config = cfg

	// Initialize gRPC server
	grpcServer = NewGRPCServer()

	// Start servers concurrently
	var wg sync.WaitGroup

	// Always start HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		startHTTPServer(cfg.Port)
	}()

	// Start gRPC server if enabled
	if cfg.EnableGRPC {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startGRPCServer(cfg)
		}()
	}

	// Print server info
	fmt.Printf("================ MOONLIGHT SERVER ================\n")
	fmt.Printf("HTTP Server started on port: %d\n", cfg.Port)
	if cfg.EnableGRPC {
		fmt.Printf("gRPC Server started on port: %d\n", cfg.GRPCPort)
	} else {
		fmt.Printf("gRPC Server: DISABLED\n")
	}
	fmt.Printf("================================================\n")

	for _, ip := range getLocalIPv4Addrs() {
		if ip == "127.0.0.1" {
			fmt.Printf("HTTP: http://%s:%d\n", ip, cfg.Port)
			if cfg.EnableGRPC {
				fmt.Printf("gRPC: %s:%d\n", ip, cfg.GRPCPort)
			}
		} else {
			fmt.Printf("HTTP: http://%s:%d (external)\n", ip, cfg.Port)
			if cfg.EnableGRPC {
				fmt.Printf("gRPC: %s:%d (external)\n", ip, cfg.GRPCPort)
			}
		}
	}
	fmt.Printf("================================================\n")

	// Wait for servers
	wg.Wait()
}

// startHTTPServer starts the HTTP server
func startHTTPServer(port int) {
	// HTTP Handlers
	http.HandleFunc("/client/heartbeat", clientHeartbeatHandler)
	http.HandleFunc("/task/request", taskRequestHandler)

	// Add status endpoint
	http.HandleFunc("/status", statusHandler)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("HTTP server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// startGRPCServer starts the gRPC server
func startGRPCServer(cfg *Config) {
	if !cfg.EnableGRPC {
		log.Println("gRPC server disabled in configuration")
		return
	}

	addr := fmt.Sprintf(":%d", cfg.GRPCPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", addr, err)
	}

	// Create gRPC server with options from config
	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(cfg.GRPCConfig.MaxMessageSize),
		grpc.MaxSendMsgSize(cfg.GRPCConfig.MaxMessageSize),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    time.Duration(cfg.GRPCConfig.KeepaliveTime) * time.Second,
			Timeout: time.Duration(cfg.GRPCConfig.KeepaliveTimeout) * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             time.Duration(cfg.GRPCConfig.KeepaliveTime/2) * time.Second,
			PermitWithoutStream: true,
		}),
	}

	s := grpc.NewServer(opts...)

	// Register the moonlight service
	pb.RegisterMoonlightServer(s, grpcServer)

	// Enable reflection if configured
	if cfg.GRPCConfig.EnableReflection {
		reflection.Register(s)
		log.Println("gRPC reflection enabled")
	}

	log.Printf("gRPC server listening on %s", addr)
	log.Fatal(s.Serve(listener))
}

// statusHandler provides server status information
func statusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	httpClients := len(getAllValidClients())
	grpcClients := 0
	if grpcServer != nil {
		grpcClients = grpcServer.GetGRPCClientCount()
	}

	status := map[string]interface{}{
		"http_clients":  httpClients,
		"grpc_clients":  grpcClients,
		"total_clients": httpClients, // Note: gRPC clients are also counted in HTTP clients
		"regions":       getActiveRegions(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// getActiveRegions returns a list of regions with active clients
func getActiveRegions() []string {
	clients := getAllValidClients()
	regions := make(map[string]bool)

	for _, client := range clients {
		client.Mutex.Lock()
		if client.Valid {
			regions[client.Region] = true
		}
		client.Mutex.Unlock()
	}

	result := make([]string, 0, len(regions))
	for region := range regions {
		result = append(result, region)
	}

	return result
}

// getLocalIPv4Addrs returns a list of IPv4 addresses for local interfaces
func getLocalIPv4Addrs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []string{"127.0.0.1"}
	}
	var result []string
	seen := map[string]bool{}

	// Always include loopback first
	result = append(result, "127.0.0.1")
	seen["127.0.0.1"] = true

	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ip = ip.To4()
		if ip == nil {
			continue
		}
		s := ip.String()
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	return result
}
