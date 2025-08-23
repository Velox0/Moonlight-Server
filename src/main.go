package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
)

var (
	config *Config
)

func main() {
	// Load config - try local file first, then system location
	configPaths := []string{"mls.json", "/etc/moonlight/mls.json"}
	var cfg *Config
	var err error

	for _, path := range configPaths {
		cfg, err = LoadConfig(path)
		if err == nil {
			fmt.Printf("Loaded config from: %s\n", path)
			break
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config from any location: %v\n", err)
		os.Exit(1)
	}
	config = cfg

	// Handlers
	http.HandleFunc("/client/heartbeat", clientHeartbeatHandler)
	http.HandleFunc("/task/request", taskRequestHandler)

	// WebSocket handler (only if enabled)
	if cfg.WS.Enabled {
		http.HandleFunc(cfg.WS.Path, wsHeartbeatHandler)
		startConnectionHealthChecker()
		fmt.Printf("WebSocket enabled on path: %s\n", cfg.WS.Path)
	}

	// HTML dashboard (only if enabled)
	if cfg.HTML.Enabled {
		// Serve static files
		fs := http.FileServer(http.Dir("static"))
		http.Handle(cfg.HTML.StaticPath+"/", http.StripPrefix(cfg.HTML.StaticPath, fs))

		// Serve dashboard
		http.HandleFunc(cfg.HTML.DashboardPath, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "static/index.html")
		})

		// Serve index page
		http.HandleFunc(cfg.HTML.IndexPath, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.ServeFile(w, r, "static/index.html")
			} else {
				http.NotFound(w, r)
			}
		})

		fmt.Printf("HTML dashboard enabled on path: %s\n", cfg.HTML.DashboardPath)
	}

	fmt.Printf("Server started on port:%d\n", cfg.Port)
	fmt.Printf("Accessible at: http://localhost:%d (loopback)\n", cfg.Port)
	for _, ip := range getLocalIPv4Addrs() {
		if ip == "127.0.0.1" {
			continue
		}
		fmt.Printf("Accessible at: http://%s:%d\n", ip, cfg.Port)
	}
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil))
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
