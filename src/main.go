// @title           Moonlight Server API
// @version         dev
// @description     A lightweight, distributed task queue and client management system with WebSocket support
// @termsOfService  http://swagger.io/terms/
//
// @contact.name   Moonlight Server
// @contact.url    https://github.com/velox0/moonlight-server
//
// @license.name  MIT
//
// @host      localhost:8080
// @basePath  /
// @schemes   http https
//
// @securityDefinitions.apikey TokenAuth
// @in header
// @name Authorization
//
// @securityDefinitions.apikey SessionAuth
// @in header
// @name X-Session-ID

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	ginSwagger "github.com/swaggo/http-swagger/v2"
	docs "github.com/velox0/moonlight-server/docs"
)

var Version = "dev"

// configReloadHandler handles POST /api/admin/reload
// @Summary      Reload configuration
// @Description  Reload server configuration from disk
// @Tags         Admin
// @Produce      json
// @Success      200   {object}  ReloadResponse
// @Failure      400   {object}  ErrorResponse
// @Failure      401   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     SessionAuth
// @Router       /api/admin/reload [post]
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
	configureSwaggerInfo(getConfig())

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

func configureSwaggerInfo(cfg *Config) {
	host := fmt.Sprintf("localhost:%d", 8080)
	basePath := "/"
	scheme := "http"

	if cfg != nil {
		if cfg.Swagger.Host != "" {
			host = cfg.Swagger.Host
		} else if cfg.Port > 0 {
			host = fmt.Sprintf("localhost:%d", cfg.Port)
		}
		if cfg.Swagger.BasePath != "" {
			basePath = cfg.Swagger.BasePath
		}
		if cfg.Swagger.Scheme != "" {
			scheme = cfg.Swagger.Scheme
		}
	}

	docs.SwaggerInfo.Host = host
	docs.SwaggerInfo.BasePath = basePath
	docs.SwaggerInfo.Schemes = []string{scheme}
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
	configureSwaggerInfo(cfg)
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

	// API documentation endpoint
	http.HandleFunc("/api/docs", apiDocsHandler)

	// Swagger UI (auto-generated from code comments)
	// Serve Swagger UI at /swagger/index.html
	http.Handle("/swagger/", ginSwagger.Handler(
		ginSwagger.URL("/swagger/doc.json"),
		ginSwagger.DocExpansion("list"),
		ginSwagger.DeepLinking(true),
	))
	// Redirect /swagger to /swagger/index.html
	http.HandleFunc("/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/index.html", http.StatusMovedPermanently)
	})

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
