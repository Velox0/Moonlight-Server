package main

import (
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
