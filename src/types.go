package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MonitorData defines the JSON output of /monitor
// @Description Server metrics and statistics
type MonitorData struct {
	HumanMemoryAlloc  string `json:"human_memory_alloc_bytes" example:"2.5 MB"`
	HumanTotalAlloc   string `json:"human_total_alloc_bytes" example:"10.3 MB"`
	HumanSysMem       string `json:"human_sys_mem_bytes" example:"5.1 MB"`
	MemoryAlloc       uint64 `json:"memory_alloc_bytes" example:"2621440"`
	TotalAlloc        uint64 `json:"total_alloc_bytes" example:"10795008"`
	SysMem            uint64 `json:"sys_mem_bytes" example:"5349376"`
	NumGC             uint32 `json:"num_gc" example:"42"`
	NumGoroutine      int    `json:"num_goroutine" example:"15"`
	ClientCount       int    `json:"client_count" example:"3"`
	RequestPending    int    `json:"request_pending" example:"0"`
	RequestFailed     int    `json:"request_failed" example:"2"`
	RequestSuccessful int    `json:"request_successful" example:"125"`
	ClientsMapSize    int    `json:"clients_map_size" example:"3"`
	RequestsMapSize   int    `json:"requests_map_size" example:"0"`
}

// HeartbeatRequest client registration payload
type HeartbeatRequest struct {
	IP     string `json:"ip" example:"192.168.1.100"`
	NodeID string `json:"node_id" example:"client-001"`
	Token  string `json:"token" example:"supersecrettokenox0"`
	Region string `json:"region" example:"us-east-1"`
	Port   int    `json:"port" example:"8000"`
}

// HeartbeatResponse server response to heartbeat
type HeartbeatResponse struct {
	Status     string `json:"status" example:"ok"`
	ServerTime string `json:"server_time" example:"2025-04-19T10:30:45Z"`
}

// SessionResponse login response with session details
type SessionResponse struct {
	SessionID string `json:"session_id" example:"sess_abc123xyz"`
	ExpiresAt string `json:"expires_at" example:"2025-04-19T12:00:00Z"`
}

// SessionCheckResponse session status information
type SessionCheckResponse struct {
	Authenticated bool   `json:"authenticated" example:"true"`
	ExpiresAt     string `json:"expires_at" example:"2025-04-19T12:00:00Z"`
}

// StatusResponse simple status response
type StatusResponse struct {
	Status string `json:"status" example:"ok"`
}

// ReloadResponse config reload response
type ReloadResponse struct {
	Status     string `json:"status" example:"ok"`
	ConfigPath string `json:"config_path" example:"/etc/moonlight/mls.json"`
	Port       int    `json:"port" example:"8000"`
	WsEnabled  bool   `json:"ws_enabled" example:"true"`
	WsPath     string `json:"ws_path" example:"/ws"`
}

// ErrorResponse error payload
type ErrorResponse struct {
	Error string `json:"error" example:"not authenticated"`
}

// ClientTableRow client status row
type ClientTableRow struct {
	NodeID    string `json:"node_id" example:"client-001"`
	Protocol  string `json:"protocol" example:"http"`
	LastSeen  string `json:"last_seen" example:"2025-04-19T10:35:22Z"`
	Connected string `json:"connected" example:"connected"`
}

// RegionListResponse regions list response
type RegionListResponse struct {
	Regions []string `json:"regions" example:"global,us-east-1,us-west-1"`
}

type Client struct {
	NodeID   string
	Protocol Protocol
	LastSeen time.Time
	Valid    bool
	Mutex    sync.Mutex
}

// Request represents a tracked request/task
type Request struct {
	ID     string
	Status string // "pending", "failed", or "successful"
}

var (
	requests      = make(map[string]*Request)
	requestsMutex sync.RWMutex

	requestCounters = struct {
		pending    int
		failed     int
		successful int
		sync.RWMutex
	}{}
)

// Clients

// Protocol represents the communication protocol used by a client
type Protocol int

const (
	ProtocolHTTP Protocol = iota
	ProtocolWS
)

// MarshalJSON implements json.Marshaler interface
func (p Protocol) MarshalJSON() ([]byte, error) {
	switch p {
	case ProtocolHTTP:
		return json.Marshal("http")
	case ProtocolWS:
		return json.Marshal("ws")
	default:
		return json.Marshal("unknown")
	}
}

// UnmarshalJSON implements json.Unmarshaler interface
func (p Protocol) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "http":
		p = ProtocolHTTP
	case "ws":
		p = ProtocolWS
	default:
		return fmt.Errorf("unknown protocol: %s", s)
	}
	return nil
}

type ClientInfo struct {
	IP         string
	NodeID     string
	Token      string
	Region     string
	Port       int
	LastSeen   time.Time
	AvgLatency time.Duration
	Valid      bool
	Protocol   Protocol
	Mutex      sync.Mutex
}
