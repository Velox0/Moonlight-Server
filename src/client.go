package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	clients       = make(map[string]*ClientInfo)
	clientsMutex  sync.RWMutex
	nodeIDCounter int = 1
	nodeIDMutex   sync.Mutex
)

const (
	clientStaleAfter   = 90 * time.Second
	failureBackoffBase = 5 * time.Second
	failureBackoffMax  = 2 * time.Minute
)

// Generate a client key
func clientKey(ip, nodeID string) string {
	return ip + ":" + nodeID
}

func assignNodeID() string {
	nodeIDMutex.Lock()
	id := nodeIDCounter
	nodeIDCounter++
	nodeIDMutex.Unlock()

	timestampHex := fmt.Sprintf("%x", time.Now().UnixNano())
	counterHex := fmt.Sprintf("%x", id)

	var randBytes [4]byte
	if _, err := rand.Read(randBytes[:]); err == nil {
		return timestampHex + counterHex + hex.EncodeToString(randBytes[:])
	}

	return timestampHex + counterHex
}

// @Summary      Register or update client presence
// @Description  Registers and updates client presence. Authentication is via token in JSON body. Payload except token and region is optional.
// @Tags         Client
// @Accept       json
// @Produce      plain
// @Param        body  body      HeartbeatRequest  true  "Heartbeat payload"
// @Success      200   {string}  string  "registered"
// @Failure      400   {object}  ErrorResponse
// @Failure      401   {object}  ErrorResponse
// @Router       /api/heartbeat [post]
func clientHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		IP     string `json:"ip"`
		NodeID string `json:"node_id"`
		Token  string `json:"token"`
		Region string `json:"region"`
		Port   int    `json:"port"`
	}

	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}

	if req.IP == "" {
		remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
		req.IP = remoteIP
	}

	if req.Region == "" || req.Token == "" {
		http.Error(w, "invalid request: token and region are required", http.StatusBadRequest)
		return
	}

	if !validToken(req.Token) {
		http.Error(w, "unauthorized token", http.StatusUnauthorized)
		return
	}

	if req.Port == 0 {
		req.Port = 3000
	}
	if req.NodeID == "" {
		req.NodeID = assignNodeID()
	}

	key := clientKey(req.IP, req.NodeID)
	clientsMutex.Lock()
	client, exists := clients[key]
	if !exists {
		client = &ClientInfo{
			IP: req.IP, NodeID: req.NodeID, Token: req.Token,
			Region: req.Region, Port: req.Port, Valid: true, Protocol: ProtocolHTTP,
		}
		clients[key] = client
	} else {
		client.Mutex.Lock()
		client.Region, client.Token, client.Port, client.Valid = req.Region, req.Token, req.Port, true
		client.Protocol = ProtocolHTTP
		client.Mutex.Unlock()
	}
	client.Mutex.Lock()
	client.LastSeen = time.Now()
	client.ConsecutiveFailures = 0
	client.UnresponsiveUntil = time.Time{}
	client.Mutex.Unlock()
	clientsMutex.Unlock()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("registered"))
}

func validToken(token string) bool {
	cfg := getConfig()
	if cfg == nil || len(cfg.Tokens) == 0 || token == "" {
		return false
	}
	for _, t := range cfg.Tokens {
		if t == token {
			return true
		}
	}
	return false
}

func isClientHealthyAt(valid bool, lastSeen time.Time, unresponsiveUntil time.Time, now time.Time) bool {
	if !valid {
		return false
	}
	if !unresponsiveUntil.IsZero() && now.Before(unresponsiveUntil) {
		return false
	}
	if !lastSeen.IsZero() && now.Sub(lastSeen) > clientStaleAfter {
		return false
	}
	return true
}

func markClientFailure(client *ClientInfo) {
	if client == nil {
		return
	}
	now := time.Now()
	client.Mutex.Lock()
	client.ConsecutiveFailures++
	client.LastFailure = now
	step := client.ConsecutiveFailures - 1
	if step < 0 {
		step = 0
	}
	if step > 6 {
		step = 6
	}
	backoff := failureBackoffBase * time.Duration(1<<uint(step))
	if backoff > failureBackoffMax {
		backoff = failureBackoffMax
	}
	client.UnresponsiveUntil = now.Add(backoff)
	client.Mutex.Unlock()
}

func markClientSuccess(client *ClientInfo) {
	if client == nil {
		return
	}
	client.Mutex.Lock()
	client.ConsecutiveFailures = 0
	client.LastFailure = time.Time{}
	client.UnresponsiveUntil = time.Time{}
	client.Mutex.Unlock()
}

func findClientByEndpoint(ip string, port int) *ClientInfo {
	clientsMutex.RLock()
	defer clientsMutex.RUnlock()
	for _, client := range clients {
		client.Mutex.Lock()
		match := client.IP == ip && client.Port == port && client.Valid
		client.Mutex.Unlock()
		if match {
			return client
		}
	}
	return nil
}

// Client selection (load balancing)
func selectBestClient(targetRegion string, candidates []*ClientInfo) *ClientInfo {
	if len(candidates) == 0 {
		return nil
	}
	now := time.Now()
	var best *ClientInfo
	var bestJobs int
	var bestDist int
	var bestSeen time.Duration
	var bestLatency time.Duration
	for _, c := range candidates {
		c.Mutex.Lock()
		valid := c.Valid
		lastSeen := c.LastSeen
		latency := c.AvgLatency
		jobs := c.ActiveJobs
		region := c.Region
		unresponsiveUntil := c.UnresponsiveUntil
		c.Mutex.Unlock()

		if !isClientHealthyAt(valid, lastSeen, unresponsiveUntil, now) {
			continue
		}

		dist := regionDistance(targetRegion, region)
		seenAge := now.Sub(lastSeen)
		if lastSeen.IsZero() {
			seenAge = time.Duration(1<<63 - 1)
		}

		if best == nil ||
			jobs < bestJobs ||
			(jobs == bestJobs && dist < bestDist) ||
			(jobs == bestJobs && dist == bestDist && seenAge < bestSeen) ||
			(jobs == bestJobs && dist == bestDist && seenAge == bestSeen && latency < bestLatency) {
			best = c
			bestJobs = jobs
			bestDist = dist
			bestSeen = seenAge
			bestLatency = latency
		}
	}
	return best
}
