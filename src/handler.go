package main

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"time"
)

// monitorHandler serves metrics at /monitor in JSON
// @Summary      Get server metrics
// @Description  Returns server runtime metrics including memory, goroutines, and request stats
// @Tags         Monitoring
// @Produce      json
// @Success      200  {object}  MonitorData
// @Failure      500  {object}  ErrorResponse
// @Router       /api/monitor [get]
func monitorHandler(w http.ResponseWriter, r *http.Request) {
	monitorData := collectMonitorData()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(monitorData); err != nil {
		http.Error(w, "Failed to encode monitor data", http.StatusInternalServerError)
		return
	}
}

// clientsTableHandler serves JSON data for connected clients
// @Summary      List connected clients
// @Description  Returns a list of all currently connected clients with their status
// @Tags         Client
// @Produce      json
// @Success      200  {array}   ClientTableRow
// @Failure      500  {object}  ErrorResponse
// @Router       /api/clients [get]
func clientsTableHandler(w http.ResponseWriter, r *http.Request) {
	type ClientTableRow struct {
		NodeID    string   `json:"node_id"`
		Protocol  Protocol `json:"protocol"`
		LastSeen  string   `json:"last_seen"`
		Connected string   `json:"connected"`
	}

	// snapshot clients under lock
	clientsMutex.RLock()
	snapshot := make([]*ClientInfo, 0, len(clients))
	for _, c := range clients {
		snapshot = append(snapshot, c)
	}
	clientsMutex.RUnlock()

	var rows []ClientTableRow
	for _, client := range snapshot {
		client.Mutex.Lock()
		status := "connected"
		if !client.Valid {
			status = "disconnected"
		}
		rows = append(rows, ClientTableRow{
			NodeID:    client.NodeID,
			Protocol:  client.Protocol,
			LastSeen:  client.LastSeen.Format(time.RFC3339),
			Connected: status,
		})
		client.Mutex.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		http.Error(w, "Failed to encode client table", http.StatusInternalServerError)
		return
	}
}

// regionListHandler returns the list of regions configured
// @Summary      List available regions
// @Description  Returns the list of configured regions in the server hierarchy
// @Tags         Configuration
// @Produce      json
// @Success      200  {object}  RegionListResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /api/region [get]
func regionListHandler(w http.ResponseWriter, r *http.Request) {
	var regions []string
	regions = append(regions, "global")

	cfg := getConfig()
	if cfg != nil && cfg.RegionHierarchy != nil {
		for region := range cfg.RegionHierarchy {
			regions = append(regions, region)
		}
	}

	sort.Strings(regions)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"regions": regions,
	}); err != nil {
		http.Error(w, "Failed to encode region list", http.StatusInternalServerError)
		return
	}
}

// collectMonitorData gathers runtime and app statistics for /monitor
func collectMonitorData() MonitorData {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	clientsMutex.RLock()
	clientCount := len(clients)
	clientsMutex.RUnlock()

	requestsMutex.RLock()
	requestCount := len(requests)
	requestsMutex.RUnlock()

	requestCounters.RLock()
	pending := requestCounters.pending
	failed := requestCounters.failed
	successful := requestCounters.successful
	requestCounters.RUnlock()

	return MonitorData{
		HumanMemoryAlloc:  humanizeBytes(memStats.Alloc),
		HumanTotalAlloc:   humanizeBytes(memStats.TotalAlloc),
		HumanSysMem:       humanizeBytes(memStats.Sys),
		MemoryAlloc:       memStats.Alloc,
		TotalAlloc:        memStats.TotalAlloc,
		SysMem:            memStats.Sys,
		NumGC:             uint32(memStats.NumGC),
		NumGoroutine:      runtime.NumGoroutine(),
		ClientCount:       clientCount,
		RequestPending:    pending,
		RequestFailed:     failed,
		RequestSuccessful: successful,
		ClientsMapSize:    clientCount,
		RequestsMapSize:   requestCount,
	}
}

// apiDocsHandler serves API documentation in JSON format
// @Summary      Get API documentation
// @Description  Returns comprehensive API documentation in JSON format
// @Tags         Documentation
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /api/docs [get]
func apiDocsHandler(w http.ResponseWriter, r *http.Request) {
	docs := map[string]interface{}{
		"title":   "Moonlight Server API Documentation",
		"version": Version,
		"baseURL": "http://localhost:8000",
		"endpoints": map[string]interface{}{
			"GET /": map[string]string{
				"description": "Serves the HTML dashboard homepage",
				"auth":        "none",
				"returns":     "HTML page with interactive dashboard",
			},
			"GET /static/*": map[string]string{
				"description": "Static file server for CSS, JavaScript, and assets",
				"auth":        "none",
				"examples":    "/static/style.css, /static/script.js",
			},
			"GET /favicon.ico": map[string]string{
				"description": "Server favicon",
				"auth":        "none",
				"returns":     "ICO image file",
			},
			"POST /api/heartbeat": map[string]string{
				"description": "Client heartbeat - registers or updates client presence",
				"auth":        "token (in Authorization header or X-Token)",
				"method":      "POST",
				"body":        `{"node_id": "string", "protocol": "websocket|http"}`,
				"returns":     `{"status": "ok", "server_time": "ISO8601"}`,
			},
			"POST /api/request": map[string]string{
				"description": "Task request from client",
				"auth":        "token",
				"method":      "POST",
				"returns":     "Task data if available, otherwise HTTP 202 Accepted",
			},
			"POST /api/admin/login": map[string]string{
				"description": "Authenticate and create an admin session",
				"auth":        "token (in Authorization header or X-Token)",
				"method":      "POST",
				"returns":     `{"session_id": "string", "expires_at": "ISO8601"}`,
			},
			"POST /api/admin/logout": map[string]string{
				"description": "Logout and invalidate session",
				"auth":        "session_id (in Cookie or X-Session-ID header)",
				"method":      "POST",
				"returns":     `{"status": "ok"}`,
			},
			"GET /api/admin/session": map[string]string{
				"description": "Check current session status",
				"auth":        "session_id",
				"method":      "GET",
				"returns":     `{"authenticated": true, "expires_at": "ISO8601"}`,
			},
			"POST /api/admin/reload": map[string]string{
				"description": "Reload server configuration (SIGHUP or HTTP)",
				"auth":        "session_id",
				"method":      "POST",
				"returns":     `{"status": "ok", "config_path": "string"}`,
			},
			"GET /api/monitor": map[string]string{
				"description": "Server metrics and statistics",
				"auth":        "none",
				"method":      "GET",
				"returns":     "MonitorData with memory, goroutines, client count, request stats",
			},
			"GET /api/clients": map[string]string{
				"description": "List of connected clients with status",
				"auth":        "none",
				"method":      "GET",
				"returns":     `[{"node_id": "string", "protocol": "string", "last_seen": "ISO8601", "connected": "string"}]`,
			},
			"GET /api/region": map[string]string{
				"description": "List of configured regions",
				"auth":        "none",
				"method":      "GET",
				"returns":     `{"regions": ["string"]}`,
			},
			"WS /ws": map[string]string{
				"description": "WebSocket endpoint for real-time client communication",
				"auth":        "token (as query parameter: /ws?token=...)",
				"protocol":    "WebSocket",
				"returns":     "WebSocket connection for bidirectional messaging",
			},
		},
		"authentication": map[string]interface{}{
			"token": map[string]string{
				"type":        "Bearer token",
				"location":    "Authorization header (Bearer TOKEN) or X-Token header",
				"description": "API token configured in server config for client authentication",
			},
			"session": map[string]string{
				"type":        "Session cookie or header",
				"location":    "Cookie (session_id) or X-Session-ID header",
				"description": "Admin session obtained after successful login",
			},
		},
		"statusCodes": map[string]string{
			"200": "OK - Request successful",
			"201": "Created - Resource created",
			"202": "Accepted - Request accepted for processing (async)",
			"400": "Bad Request - Invalid parameters",
			"401": "Unauthorized - Missing or invalid authentication",
			"403": "Forbidden - Authenticated but not authorized",
			"404": "Not Found - Endpoint does not exist",
			"405": "Method Not Allowed - Wrong HTTP method",
			"500": "Internal Server Error",
		},
		"examples": map[string]interface{}{
			"curl_heartbeat": `curl -X POST http://localhost:8000/api/heartbeat -H "X-Token: supersecrettokenox0" -H "Content-Type: application/json" -d '{"node_id": "client-001", "protocol": "http"}'`,
			"curl_login":     `curl -X POST http://localhost:8000/api/admin/login -H "X-Token: supersecrettokenox0"`,
			"curl_monitor":   `curl http://localhost:8000/api/monitor`,
			"curl_clients":   `curl http://localhost:8000/api/clients`,
			"curl_regions":   `curl http://localhost:8000/api/region`,
		},
		"notes": []string{
			"All timestamps are in ISO8601 format (UTC)",
			"HTML dashboard is available at / (requires 'enabled: true' in config html section)",
			"WebSocket is optional and must be enabled in config",
			"Tokens are configured in mls.json and required for client operations",
			"Sessions expire after configured timeout",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if err := json.NewEncoder(w).Encode(docs); err != nil {
		http.Error(w, "Failed to encode documentation", http.StatusInternalServerError)
	}
}
