package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var Version = "dev"

// configReloadHandler handles POST /api/admin/reload
func configReloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("Config reload denied: method=%s remote=%s", r.Method, r.RemoteAddr)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Require a valid session
	if !getSessionFromRequest(r) {
		log.Printf("Config reload denied: not authenticated remote=%s", r.RemoteAddr)
		http.Error(w, `{"error":"not authenticated"}`, http.StatusUnauthorized)
		return
	}

	path, err := reloadConfig()
	if err != nil {
		log.Printf("Config reload denied: remote=%s error=%v", r.RemoteAddr, err)
		http.Error(w, fmt.Sprintf("failed to reload config: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Config reloaded from %s (HTTP) remote=%s", path, r.RemoteAddr)

	cfg := getConfig()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"config_path": path,
		"port":        cfg.Port,
		"ws_enabled":  cfg.WS.Enabled,
		"ws_path":     cfg.WS.Path,
	})
}

func startConfigSignalWatcher() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			path, err := reloadConfig()
			if err != nil {
				log.Printf("Config reload failed (SIGHUP): %v", err)
				continue
			}
			log.Printf("Config reloaded from %s (SIGHUP)", path)
		}
	}()
}

func main() {
	fmt.Println("moonlight-server version:", Version)

	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "--version" {
			os.Exit(0)
		}
	}

	// Load config - try local file first, then system locations
	homeDir, err := os.UserHomeDir()
	var configPaths []string

	if err == nil {
		configPaths = []string{"mls.json", homeDir + "/.config/moonlight/mls.json", "/etc/moonlight/mls.json"}
	} else {
		configPaths = []string{"mls.json", "/etc/moonlight/mls.json"}
	}
	setConfigSearchPaths(configPaths)

	cfg, loadedPath, err := loadConfigFromSearchPaths()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config from any location: %v\n", err)
		os.Exit(1)
	}
	setActiveConfig(cfg, loadedPath)
	fmt.Printf("Loaded config from: %s\n", loadedPath)
	startConfigSignalWatcher()

	// Session cleanup
	cleanExpiredSessions()

	// Handlers
	http.HandleFunc("/api/heartbeat", clientHeartbeatHandler)
	http.HandleFunc("/api/request", taskRequestHandler)
	http.HandleFunc("/api/admin/login", loginHandler)
	http.HandleFunc("/api/admin/logout", logoutHandler)
	http.HandleFunc("/api/admin/session", sessionCheckHandler)
	http.HandleFunc("/api/admin/reload", configReloadHandler)

	// WebSocket handler (only if enabled)
	if cfg.WS.Enabled {
		http.HandleFunc(cfg.WS.Path, wsHeartbeatHandler)
		startConnectionHealthChecker()
	}

	// QUIC listener (only if port is configured)
	if cfg.QUICPort > 0 {
		if err := StartQUICListener(cfg.QUICPort); err != nil {
			log.Printf("Failed to start QUIC listener: %v", err)
			// Continue without QUIC if it fails
		} else {
			// Start peer cleanup loop
			StartPeerCleanupLoop(30*time.Second, 5*time.Minute)
		}
	}

	// Client table endpoint
	http.HandleFunc("/api/clients", clientsTableHandler)

	// Region endpoint
	http.HandleFunc("/api/region", regionListHandler)

	// Task endpoints
	http.HandleFunc("/api/request", taskRequestHandler)
	http.HandleFunc("/api/request/quic", quicTaskRequestHandler)
	http.HandleFunc("/api/request/quic/execute", quicTaskExecuteHandler)

	// New monitor endpoint
	http.HandleFunc("/api/monitor", monitorHandler)

	// HTML dashboard (only if enabled)
	if cfg.HTML.Enabled {
		// Serve static files from /static
		staticFS := http.FileServer(http.Dir("static"))
		http.Handle(cfg.HTML.StaticPath+"/", http.StripPrefix(cfg.HTML.StaticPath, staticFS))

		// Root path handler - serves index.html for /
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Only serve index.html for the root path
			if r.URL.Path == "/" {
				http.ServeFile(w, r, "static/index.html")
				return
			}
			// For any other path not caught by other handlers, return 404
			http.NotFound(w, r)
		})

		fmt.Printf("HTML dashboard enabled\n")
		fmt.Printf("  Static files: %s/\n", cfg.HTML.StaticPath)
		fmt.Printf("  Homepage: %s\n", cfg.HTML.IndexPath)
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
	fmt.Printf("Config reload endpoint: http://localhost:%d/api/admin/reload (POST)\n", cfg.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil))
}
