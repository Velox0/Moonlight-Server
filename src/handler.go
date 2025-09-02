package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"time"
)

// monitorHandler serves metrics at /monitor in JSON
func monitorHandler(w http.ResponseWriter, r *http.Request) {
	monitorData := collectMonitorData()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(monitorData); err != nil {
		http.Error(w, "Failed to encode monitor data", http.StatusInternalServerError)
		return
	}
}

// clientsTableHandler serves JSON or HTML table of connected clients
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

	// If Accept: text/html, render as HTML table
	if r.Header.Get("Accept") == "text/html" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<table border='1'><tr><th>NodeID</th><th>Protocol</th><th>Last Seen</th><th>Status</th></tr>")
		for _, row := range rows {
			protoJSON, err := row.Protocol.MarshalJSON()
			protoStr := ""
			if err == nil {
				protoStr = string(protoJSON)
			} else {
				protoStr = "error"
			}
			if len(protoStr) >= 2 && protoStr[0] == '"' && protoStr[len(protoStr)-1] == '"' {
				protoStr = protoStr[1 : len(protoStr)-1]
			}
			fmt.Fprintf(w, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>", row.NodeID, protoStr, row.LastSeen, row.Connected)
		}
		fmt.Fprintf(w, "</table>")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rows); err != nil {
		http.Error(w, "Failed to encode client table", http.StatusInternalServerError)
		return
	}
}

// regionListHandler returns the list of regions configured
func regionListHandler(w http.ResponseWriter, r *http.Request) {
	var regions []string
	regions = append(regions, "global")

	if config != nil && config.RegionHierarchy != nil {
		for region := range config.RegionHierarchy {
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
