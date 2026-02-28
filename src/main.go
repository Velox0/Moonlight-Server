package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var Version = "dev"

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

	// Client table endpoint
	http.HandleFunc("/api/clients", clientsTableHandler)

	// Region endpoint
	http.HandleFunc("/api/region", regionListHandler)

	// New monitor endpoint
	http.HandleFunc("/api/monitor", monitorHandler)

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
	fmt.Printf("Config reload endpoint: http://localhost:%d/api/admin/reload (POST)\n", cfg.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil))
}
