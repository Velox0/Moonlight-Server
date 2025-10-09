package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

var config *Config
var Version = "dev"

func main() {
	fmt.Println("moonlight-server version:", Version)

	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "--version" {
			os.Exit(0)
		}
	}

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
	}

	// Client table endpoint
	http.HandleFunc("/clients/table", clientsTableHandler)

	// Region endpoint
	http.HandleFunc("/region", regionListHandler)

	// New monitor endpoint
	http.HandleFunc("/monitor", monitorHandler)

	// HTML dashboard (only if enabled)
	if cfg.HTML.Enabled {
		// Serve static files
		fs := http.FileServer(http.Dir("static"))
		http.Handle(cfg.HTML.StaticPath+"/", http.StripPrefix(cfg.HTML.StaticPath, fs))

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

	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/favicon.ico")
	})

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
