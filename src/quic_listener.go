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

var (
	quicListener *quic.Listener
	quicMutex    sync.RWMutex
)

// StartQUICListener starts the QUIC server on the configured port
func StartQUICListener(port int) error {
	quicMutex.Lock()
	defer quicMutex.Unlock()

	// Self-signed cert for P2P (in production, use proper certs or DTLS handshake)
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // P2P environment, peers trust each other
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{quicALPN},
	}

	listener, err := quic.ListenAddr(fmt.Sprintf(":%d", port), tlsConfig, &quic.Config{
		MaxIdleTimeout:                 30 * time.Second,
		InitialStreamReceiveWindow:     1 * 1024 * 1024,   // 1MB initial
		MaxStreamReceiveWindow:         10 * 1024 * 1024,  // 10MB max per stream
		InitialConnectionReceiveWindow: 10 * 1024 * 1024,  // 10MB initial
		MaxConnectionReceiveWindow:     100 * 1024 * 1024, // 100MB max per connection
		HandshakeIdleTimeout:           10 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to create QUIC listener: %w", err)
	}

	quicListener = listener
	log.Printf("QUIC listener started on :%d", port)

	// Accept connections in background
	go acceptQUICConnections()

	return nil
}

// acceptQUICConnections accepts incoming QUIC connections and routes streams
func acceptQUICConnections() {
	for {
		quicMutex.RLock()
		listener := quicListener
		quicMutex.RUnlock()
		if listener == nil {
			return
		}

		conn, err := listener.Accept(context.Background())
		if err != nil {
			quicMutex.RLock()
			stopped := quicListener == nil
			quicMutex.RUnlock()
			if stopped {
				return
			}
			log.Printf("QUIC accept error: %v", err)
			continue
		}

		remoteIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		log.Printf("QUIC connection accepted from %s", remoteIP)

		// Handle streams from this connection in background
		go handleQUICConnection(conn)
	}
}

// handleQUICConnection handles all streams from a QUIC connection
func handleQUICConnection(conn *quic.Conn) {
	defer conn.CloseWithError(0, "peer closed")

	remoteAddr := conn.RemoteAddr().String()

	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			log.Printf("QUIC stream accept error: %v", err)
			return
		}

		// Handle stream in goroutine with timeout
		go handleQUICStream(stream, remoteAddr)
	}
}

// handleQUICStream handles a single QUIC stream (one task request-response)
func handleQUICStream(stream *quic.Stream, remoteAddr string) {
	defer stream.Close()

	remoteIP := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteIP = host
	}

	// Total stream timeout: 60 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Read task frame
	frameType, frameData, err := ReadFrame(ctx, stream)
	if err != nil {
		log.Printf("QUIC stream %d: read frame error from %s: %v", stream.StreamID(), remoteIP, err)
		WriteFrame(context.Background(), stream, FrameTypeError, []byte("failed to read frame"))
		return
	}

	if frameType == FrameTypePing {
		if err := WriteFrame(ctx, stream, FrameTypePong, []byte{}); err != nil {
			log.Printf("QUIC stream %d: failed to write pong to %s: %v", stream.StreamID(), remoteIP, err)
		}
		return
	}

	if frameType != FrameTypeTask {
		log.Printf("QUIC stream %d: unexpected frame type %d from %s", stream.StreamID(), frameType, remoteIP)
		WriteFrame(context.Background(), stream, FrameTypeError, []byte("expected task frame"))
		return
	}

	// Parse task
	var taskFrame TaskFrame
	if err := taskFrame.Unmarshal(frameData); err != nil {
		log.Printf("QUIC stream %d: unmarshal error from %s: %v", stream.StreamID(), remoteIP, err)
		WriteFrame(context.Background(), stream, FrameTypeError, []byte("invalid task frame"))
		return
	}

	// Enforce deadline from task frame
	if taskFrame.Deadline < 0 {
		log.Printf("QUIC stream %d: invalid deadline from %s", stream.StreamID(), remoteIP)
		WriteFrame(context.Background(), stream, FrameTypeError, []byte("invalid deadline"))
		return
	}
	if taskFrame.Deadline > 0 {
		deadline := time.Unix(0, taskFrame.Deadline)
		if time.Now().After(deadline) {
			log.Printf("QUIC stream %d: task %s already expired", stream.StreamID(), taskFrame.TaskID)
			respFrame := ResponseFrame{
				TaskID: taskFrame.TaskID,
				Status: 504, // Gateway Timeout
				Error:  "task deadline exceeded",
			}
			respData, _ := respFrame.Marshal()
			WriteFrame(ctx, stream, FrameTypeResponse, respData)
			return
		}

		// Create child context with task deadline
		timeUntilDeadline := time.Until(deadline)
		if timeUntilDeadline > 0 {
			newCtx, newCancel := context.WithTimeout(ctx, timeUntilDeadline)
			defer newCancel()
			ctx = newCtx
		}
	}

	if taskFrame.TaskID == "" || taskFrame.Region == "" {
		log.Printf("QUIC stream %d: invalid task fields from %s", stream.StreamID(), remoteIP)
		WriteFrame(context.Background(), stream, FrameTypeError, []byte("invalid task fields"))
		return
	}

	log.Printf("QUIC stream %d: received task %s from %s region %s", stream.StreamID(), taskFrame.TaskID, remoteIP, taskFrame.Region)

	// Find client for region
	client := selectClientForRegion(taskFrame.Region)
	if client == nil {
		log.Printf("QUIC stream %d: no client available for region %s", stream.StreamID(), taskFrame.Region)
		respFrame := ResponseFrame{
			TaskID: taskFrame.TaskID,
			Status: 503, // Service Unavailable
			Error:  "no suitable client available",
		}
		respData, _ := respFrame.Marshal()
		WriteFrame(ctx, stream, FrameTypeResponse, respData)
		return
	}

	if client.Port <= 0 {
		log.Printf("QUIC stream %d: selected client %s not advertising port", stream.StreamID(), client.NodeID)
		respFrame := ResponseFrame{
			TaskID: taskFrame.TaskID,
			Status: 503,
			Error:  "selected client has no TCP port",
		}
		respData, _ := respFrame.Marshal()
		WriteFrame(ctx, stream, FrameTypeResponse, respData)
		return
	}

	// Return P2P direct connection info (no relay)
	// Client should connect directly to selected peer
	respFrame := ResponseFrame{
		TaskID: taskFrame.TaskID,
		Status: 200,
		Payload: []byte(fmt.Sprintf(`{"mode":"p2p-quic","peer":{"ip":"%s","port":%d,"node_id":"%s"}}`,
			client.IP, client.Port, client.NodeID)),
	}

	respData, err := respFrame.Marshal()
	if err != nil {
		log.Printf("QUIC stream %d: response marshal error: %v", stream.StreamID(), err)
		return
	}

	if err := WriteFrame(ctx, stream, FrameTypeResponse, respData); err != nil {
		log.Printf("QUIC stream %d: write response error: %v", stream.StreamID(), err)
		return
	}

	log.Printf("QUIC stream %d: sent P2P match to %s", stream.StreamID(), taskFrame.TaskID)
}

// StopQUICListener gracefully stops the QUIC listener
func StopQUICListener() {
	quicMutex.Lock()
	defer quicMutex.Unlock()

	if quicListener != nil {
		quicListener.Close()
		quicListener = nil
		log.Printf("QUIC listener stopped")
	}
}
