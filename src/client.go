package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
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

// Client selection (load balancing)
func selectBestClient(candidates []*ClientInfo) *ClientInfo {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	bestScore := math.MaxFloat64
	for _, c := range candidates {
		c.Mutex.Lock()
		t := time.Since(c.LastSeen).Seconds()
		lat := c.AvgLatency.Seconds()
		score := t + lat
		if score < bestScore {
			best = c
			bestScore = score
		}
		c.Mutex.Unlock()
	}
	return best
}
