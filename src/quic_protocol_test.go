package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWriteReadFrameRoundTrip(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	payload := []byte("hello")

	go func() {
		_ = WriteFrame(ctx, client, FrameTypePing, payload)
	}()

	frameType, data, err := ReadFrame(ctx, server)
	if err != nil {
		t.Fatalf("read frame failed: %v", err)
	}
	if frameType != FrameTypePing {
		t.Fatalf("unexpected frame type: %v", frameType)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("payload mismatch: got %q want %q", string(data), string(payload))
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	buf := make([]byte, 5)
	buf[0] = byte(FrameTypeTask)
	over := uint32(maxFrameDataSize + 1)
	buf[1] = byte(over >> 24)
	buf[2] = byte(over >> 16)
	buf[3] = byte(over >> 8)
	buf[4] = byte(over)

	_, _, err := ReadFrame(ctx, bytes.NewReader(buf))
	if err == nil {
		t.Fatalf("expected oversize error")
	}
	if !strings.Contains(err.Error(), "frame data too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadFrameContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := ReadFrame(ctx, bytes.NewReader([]byte{}))
	if err == nil {
		t.Fatalf("expected context error")
	}
}
