package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// PeerConnection maintains a pooled QUIC connection to a remote peer
type PeerConnection struct {
	RemoteIP      string
	RemotePort    int
	Conn          *quic.Conn
	LastUsed      time.Time
	MaxRetries    int
	BaseBackoffMs int
	Mutex         sync.RWMutex
}

// PeerPool manages connections to multiple peers with reconnection logic
type PeerPool struct {
	Peers map[string]*PeerConnection // key: "ip:port"
	Mutex sync.RWMutex
}

var (
	peerPool = &PeerPool{
		Peers: make(map[string]*PeerConnection),
	}

	quicALPN = "moonlight-quic"

	qcfg = &quic.Config{
		MaxIdleTimeout:                 30 * time.Second,
		InitialStreamReceiveWindow:     1 * 1024 * 1024,   // 1MB initial
		MaxStreamReceiveWindow:         10 * 1024 * 1024,  // 10MB max per stream
		InitialConnectionReceiveWindow: 10 * 1024 * 1024,  // 10MB initial
		MaxConnectionReceiveWindow:     100 * 1024 * 1024, // 100MB max per connection
		HandshakeIdleTimeout:           10 * time.Second,
	}

	tlsCfg = &tls.Config{
		InsecureSkipVerify: true, // P2P environment
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{quicALPN},
	}
)

const (
	defaultPeerTimeout = 25 * time.Second
	defaultPingTimeout = 5 * time.Second
	maxDialTimeout     = 10 * time.Second
)

// GetOrDialPeer gets or creates a QUIC connection to a peer
func GetOrDialPeer(ctx context.Context, remoteIP string, remotePort int) (*quic.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	key := fmt.Sprintf("%s:%d", remoteIP, remotePort)

	peerPool.Mutex.Lock()
	peerConn, exists := peerPool.Peers[key]
	peerPool.Mutex.Unlock()

	if exists {
		peerConn.Mutex.RLock()
		conn := peerConn.Conn
		peerConn.Mutex.RUnlock()

		if conn != nil {
			// Test connection health with ping
			if err := PingPeer(ctx, conn); err == nil {
				peerConn.Mutex.Lock()
				peerConn.LastUsed = time.Now()
				peerConn.Mutex.Unlock()
				return conn, nil
			}
			// Connection dead, reconnect
			log.Printf("Peer %s connection dead, reconnecting", key)
		}
	}

	// Dial new connection
	return dialPeerWithRetry(ctx, remoteIP, remotePort)
}

// dialPeerWithRetry dials a peer with exponential backoff retry
func dialPeerWithRetry(ctx context.Context, remoteIP string, remotePort int) (*quic.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	key := fmt.Sprintf("%s:%d", remoteIP, remotePort)

	peerPool.Mutex.Lock()
	peerConn, exists := peerPool.Peers[key]
	if !exists {
		peerConn = &PeerConnection{
			RemoteIP:      remoteIP,
			RemotePort:    remotePort,
			MaxRetries:    5,
			BaseBackoffMs: 100,
		}
		peerPool.Peers[key] = peerConn
	}
	peerPool.Mutex.Unlock()

	peerConn.Mutex.Lock()
	maxRetries := peerConn.MaxRetries
	peerConn.Mutex.Unlock()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := ExponentialBackoff(attempt-1, maxRetries, peerConn.BaseBackoffMs)
			log.Printf("Peer %s dial retry attempt %d/%d, backing off %v", key, attempt, maxRetries, backoff)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		ip := net.ParseIP(remoteIP)
		if ip == nil {
			return nil, fmt.Errorf("invalid peer IP: %s", remoteIP)
		}
		addr := net.UDPAddr{IP: ip, Port: remotePort}

		dialCtx, cancel := context.WithTimeout(ctx, maxDialTimeout)
		conn, err := quic.DialAddr(dialCtx, addr.String(), tlsCfg, qcfg)
		cancel()

		if err == nil {
			peerConn.Mutex.Lock()
			peerConn.Conn = conn
			peerConn.LastUsed = time.Now()
			peerConn.Mutex.Unlock()

			log.Printf("Peer %s dial succeeded (attempt %d)", key, attempt+1)
			return conn, nil
		}

		lastErr = err
		log.Printf("Peer %s dial failed (attempt %d/%d): %v", key, attempt+1, maxRetries+1, err)
	}

	return nil, fmt.Errorf("failed to dial peer %s after %d attempts: %w", key, maxRetries+1, lastErr)
}

// SendTaskToPeer sends a task frame to a peer and waits for response
func SendTaskToPeer(ctx context.Context, remoteIP string, remotePort int, taskFrame *TaskFrame) (*ResponseFrame, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultPeerTimeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	conn, err := GetOrDialPeer(ctx, remoteIP, remotePort)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer connection: %w", err)
	}

	key := fmt.Sprintf("%s:%d", remoteIP, remotePort)

	// Open new stream for this task
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		log.Printf("Peer %s: failed to open stream: %v", key, err)
		// Invalidate connection on stream open failure
		peerPool.Mutex.Lock()
		if pc, exists := peerPool.Peers[key]; exists {
			pc.Mutex.Lock()
			pc.Conn = nil
			pc.Mutex.Unlock()
		}
		peerPool.Mutex.Unlock()
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Set stream deadlines based on context
	if deadline, ok := ctx.Deadline(); ok {
		timeUntilDeadline := time.Until(deadline)
		if timeUntilDeadline > 0 {
			streamCtx, cancel := context.WithTimeout(ctx, timeUntilDeadline)
			defer cancel()
			ctx = streamCtx
		}
	}

	// Marshal and send task frame
	taskData, err := taskFrame.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task frame: %w", err)
	}

	if err := WriteFrame(ctx, stream, FrameTypeTask, taskData); err != nil {
		log.Printf("Peer %s: failed to write task frame: %v", key, err)
		return nil, fmt.Errorf("failed to send task: %w", err)
	}

	// Read response frame
	frameType, respData, err := ReadFrame(ctx, stream)
	if err != nil {
		log.Printf("Peer %s: failed to read response: %v", key, err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if frameType == FrameTypeError {
		return nil, fmt.Errorf("peer error: %s", string(respData))
	}
	if frameType != FrameTypeResponse {
		return nil, fmt.Errorf("unexpected frame type from peer: %d", frameType)
	}

	// Parse response
	var respFrame ResponseFrame
	if err := respFrame.Unmarshal(respData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	log.Printf("Peer %s: received response for task %s (status=%d)", key, respFrame.TaskID, respFrame.Status)

	return &respFrame, nil
}

// PingPeer sends a ping and waits for pong to test connection health
func PingPeer(ctx context.Context, conn *quic.Conn) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	pingCtx, cancel := context.WithTimeout(ctx, defaultPingTimeout)
	defer cancel()

	if err := WriteFrame(pingCtx, stream, FrameTypePing, []byte{}); err != nil {
		return err
	}

	frameType, respData, err := ReadFrame(pingCtx, stream)
	if err != nil {
		return err
	}

	if frameType == FrameTypeError {
		return fmt.Errorf("peer error: %s", string(respData))
	}
	if frameType != FrameTypePong {
		return fmt.Errorf("expected pong, got frame type %d", frameType)
	}

	return nil
}

// CloseInactivePeers closes connections that haven't been used recently
func CloseInactivePeers(maxIdleTime time.Duration) {
	peerPool.Mutex.Lock()
	defer peerPool.Mutex.Unlock()

	now := time.Now()
	for key, peerConn := range peerPool.Peers {
		peerConn.Mutex.RLock()
		lastUsed := peerConn.LastUsed
		conn := peerConn.Conn
		peerConn.Mutex.RUnlock()

		if now.Sub(lastUsed) > maxIdleTime && conn != nil {
			peerConn.Mutex.Lock()
			conn.CloseWithError(0, "idle timeout")
			peerConn.Conn = nil
			peerConn.Mutex.Unlock()

			log.Printf("Closed idle peer connection: %s", key)
		}
	}
}

// StartPeerCleanupLoop periodically closes idle peer connections
func StartPeerCleanupLoop(interval time.Duration, maxIdleTime time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			CloseInactivePeers(maxIdleTime)
		}
	}()

	log.Printf("Peer cleanup loop started (interval=%v, maxIdleTime=%v)", interval, maxIdleTime)
}
