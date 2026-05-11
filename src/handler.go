package main

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"
)

func requestScheme(r *http.Request) string {
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		return strings.TrimSpace(strings.Split(forwardedProto, ",")[0])
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func requestHost(r *http.Request) string {
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		return strings.TrimSpace(strings.Split(forwardedHost, ",")[0])
	}
	return r.Host
}

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
		NodeID     string   `json:"node_id"`
		Protocol   Protocol `json:"protocol"`
		LastSeen   string   `json:"last_seen"`
		Connected  string   `json:"connected"`
		ActiveJobs int      `json:"active_jobs"`
		Health     string   `json:"health"`
	}

	// snapshot clients under lock
	clientsMutex.RLock()
	snapshot := make([]*ClientInfo, 0, len(clients))
	for _, c := range clients {
		snapshot = append(snapshot, c)
	}
	clientsMutex.RUnlock()

	now := time.Now()
	var rows []ClientTableRow
	for _, client := range snapshot {
		client.Mutex.Lock()
		status := "connected"
		if !client.Valid {
			status = "disconnected"
		}
		health := "healthy"
		if !client.Valid {
			health = "disconnected"
		} else if !isClientHealthyAt(client.Valid, client.LastSeen, client.UnresponsiveUntil, now) {
			health = "unresponsive"
		}
		rows = append(rows, ClientTableRow{
			NodeID:     client.NodeID,
			Protocol:   client.Protocol,
			LastSeen:   client.LastSeen.Format(time.RFC3339),
			Connected:  status,
			ActiveJobs: client.ActiveJobs,
			Health:     health,
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
