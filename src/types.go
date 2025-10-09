package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MonitorData defines the JSON output of /monitor
type MonitorData struct {
	HumanMemoryAlloc  string `json:"human_memory_alloc_bytes"`
	HumanTotalAlloc   string `json:"human_total_alloc_bytes"`
	HumanSysMem       string `json:"human_sys_mem_bytes"`
	MemoryAlloc       uint64 `json:"memory_alloc_bytes"`
	TotalAlloc        uint64 `json:"total_alloc_bytes"`
	SysMem            uint64 `json:"sys_mem_bytes"`
	NumGC             uint32 `json:"num_gc"`
	NumGoroutine      int    `json:"num_goroutine"`
	ClientCount       int    `json:"client_count"`
	RequestPending    int    `json:"request_pending"`
	RequestFailed     int    `json:"request_failed"`
	RequestSuccessful int    `json:"request_successful"`
	ClientsMapSize    int    `json:"clients_map_size"`
	RequestsMapSize   int    `json:"requests_map_size"`
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
