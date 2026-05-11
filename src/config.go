package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

var (
	config           *Config
	configMutex      sync.RWMutex
	configSearchPath []string
	loadedConfigPath string
)

// WebSocketConfig structure for WebSocket settings
type WebSocketConfig struct {
	Enabled                 bool   `json:"enabled"`
	Path                    string `json:"path"`
	MaxConnections          int    `json:"max_connections"`
	ReadTimeout             int    `json:"read_timeout"`
	WriteTimeout            int    `json:"write_timeout"`
	HeartbeatInterval       int    `json:"heartbeat_interval"`
	ConnectionCheckInterval int    `json:"connection_check_interval"`
}

// HTMLConfig structure for HTML/static file settings
type HTMLConfig struct {
	Enabled       bool   `json:"enabled"`
	StaticPath    string `json:"static_path"`
	IndexPath     string `json:"index_path"`
	DashboardPath string `json:"dashboard_path"`
}

// SwaggerConfig structure for Swagger/OpenAPI metadata
type SwaggerConfig struct {
	Host     string `json:"host"`
	BasePath string `json:"base_path"`
	Scheme   string `json:"scheme"`
}

// Config structure for server
type Config struct {
	Tokens          []string          `json:"tokens"`
	RetryCount      int               `json:"retry_count"`
	Port            int               `json:"port"`
	WS              WebSocketConfig   `json:"ws"`
	HTML            HTMLConfig        `json:"html"`
	Swagger         SwaggerConfig     `json:"swagger"`
	RegionHierarchy map[string]string `json:"region_hierarchy"`
}

func printConfigMinimal(cfg *Config) {
	// Build a stable, readable hierarchy string
	keys := make([]string, 0, len(cfg.RegionHierarchy))
	for k := range cfg.RegionHierarchy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s->%s", k, cfg.RegionHierarchy[k]))
	}
	hierarchy := "-"
	if len(pairs) > 0 {
		hierarchy = strings.Join(pairs, ", ")
	}

	// WebSocket status
	wsStatus := "disabled"
	if cfg.WS.Enabled {
		wsStatus = fmt.Sprintf("enabled (path: %s)", cfg.WS.Path)
	}

	// HTML status
	htmlStatus := "disabled"
	if cfg.HTML.Enabled {
		htmlStatus = fmt.Sprintf("enabled (static: %s)", cfg.HTML.StaticPath)
	}

	fmt.Printf(
		"================ CONFIG ================\n"+
			"Tokens           : [%s]\n"+
			"RetryCount       : %d\n"+
			"Region Hierarchy : %s\n"+
			"Port             : %d\n"+
			"WebSocket        : %s\n"+
			"HTML             : %s\n"+
			"=======================================\n",
		strings.Join(cfg.Tokens, ", "),
		cfg.RetryCount,
		hierarchy,
		cfg.Port,
		wsStatus,
		htmlStatus,
	)
}

// LoadConfig loads server configuration
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}

	// Set default WebSocket configuration
	if cfg.WS.Path == "" {
		cfg.WS.Path = "/ws"
	}
	if cfg.WS.MaxConnections == 0 {
		cfg.WS.MaxConnections = 1000
	}
	if cfg.WS.ReadTimeout == 0 {
		cfg.WS.ReadTimeout = 300
	}
	if cfg.WS.WriteTimeout == 0 {
		cfg.WS.WriteTimeout = 30
	}
	if cfg.WS.HeartbeatInterval == 0 {
		cfg.WS.HeartbeatInterval = 30
	}
	if cfg.WS.ConnectionCheckInterval == 0 {
		cfg.WS.ConnectionCheckInterval = 60
	}

	// Set default HTML configuration
	if cfg.HTML.StaticPath == "" {
		cfg.HTML.StaticPath = "/static"
	}
	if cfg.HTML.IndexPath == "" {
		cfg.HTML.IndexPath = "/"
	}
	if cfg.HTML.DashboardPath == "" {
		cfg.HTML.DashboardPath = "/dashboard"
	}

	// Set default Swagger configuration
	if cfg.Swagger.BasePath == "" {
		cfg.Swagger.BasePath = "/"
	}
	if cfg.Swagger.Scheme == "" {
		cfg.Swagger.Scheme = "http"
	}
	if cfg.Swagger.Host == "" {
		cfg.Swagger.Host = fmt.Sprintf("localhost:%d", cfg.Port)
	}

	printConfigMinimal(&cfg)

	return &cfg, nil
}

func setConfigSearchPaths(paths []string) {
	configMutex.Lock()
	configSearchPath = append([]string(nil), paths...)
	configMutex.Unlock()
}

func loadConfigFromSearchPaths() (*Config, string, error) {
	configMutex.RLock()
	paths := append([]string(nil), configSearchPath...)
	configMutex.RUnlock()

	if len(paths) == 0 {
		return nil, "", fmt.Errorf("no config search paths configured")
	}

	var lastErr error
	for _, path := range paths {
		cfg, err := LoadConfig(path)
		if err == nil {
			return cfg, path, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unable to load config")
	}
	return nil, "", lastErr
}

func setActiveConfig(cfg *Config, path string) {
	configMutex.Lock()
	config = cfg
	loadedConfigPath = path
	configMutex.Unlock()
}

func getConfig() *Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return config
}

func reloadConfig() (string, error) {
	cfg, path, err := loadConfigFromSearchPaths()
	if err != nil {
		return "", err
	}
	setActiveConfig(cfg, path)
	return path, nil
}
