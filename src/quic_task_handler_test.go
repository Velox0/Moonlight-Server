package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQuicTaskExecuteHandlerSuccess(t *testing.T) {
	prev := sendTaskToPeer
	defer func() { sendTaskToPeer = prev }()

	sendTaskToPeer = func(ctx context.Context, ip string, port int, task *TaskFrame) (*ResponseFrame, error) {
		return &ResponseFrame{
			TaskID:  task.TaskID,
			Status:  200,
			Payload: []byte(`{"ok":true}`),
		}, nil
	}

	body := map[string]interface{}{
		"task_id":             "t-1",
		"peer_ip":             "127.0.0.1",
		"peer_port":           9000,
		"deadline_unix_nanos": time.Now().Add(5 * time.Second).UnixNano(),
		"payload":             map[string]interface{}{"x": 1},
	}
	buf, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/request/quic/execute", bytes.NewReader(buf))
	w := httptest.NewRecorder()
	quicTaskExecuteHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != `{"ok":true}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}

func TestQuicTaskExecuteHandlerDeadlinePassed(t *testing.T) {
	prev := sendTaskToPeer
	defer func() { sendTaskToPeer = prev }()

	called := false
	sendTaskToPeer = func(ctx context.Context, ip string, port int, task *TaskFrame) (*ResponseFrame, error) {
		called = true
		return nil, fmt.Errorf("should not be called")
	}

	body := map[string]interface{}{
		"task_id":             "t-2",
		"peer_ip":             "127.0.0.1",
		"peer_port":           9000,
		"deadline_unix_nanos": time.Now().Add(-1 * time.Second).UnixNano(),
		"payload":             map[string]interface{}{"x": 1},
	}
	buf, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/request/quic/execute", bytes.NewReader(buf))
	w := httptest.NewRecorder()
	quicTaskExecuteHandler(w, req)

	if w.Code != http.StatusRequestTimeout {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	if called {
		t.Fatalf("sendTaskToPeer should not have been called")
	}
}

func TestQuicTaskExecuteHandlerClampsRetryAndBackoff(t *testing.T) {
	prev := sendTaskToPeer
	defer func() { sendTaskToPeer = prev }()

	var gotMaxRetries int
	var gotBackoffMs int
	sendTaskToPeer = func(ctx context.Context, ip string, port int, task *TaskFrame) (*ResponseFrame, error) {
		gotMaxRetries = task.MaxRetries
		gotBackoffMs = task.BackoffMs
		return &ResponseFrame{TaskID: task.TaskID, Status: 200, Payload: []byte(`{"ok":true}`)}, nil
	}

	body := map[string]interface{}{
		"task_id":             "t-3",
		"peer_ip":             "127.0.0.1",
		"peer_port":           9000,
		"deadline_unix_nanos": time.Now().Add(5 * time.Second).UnixNano(),
		"payload":             map[string]interface{}{"x": 1},
		"max_retries":         999,
		"backoff_ms":          1,
	}
	buf, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/request/quic/execute", bytes.NewReader(buf))
	w := httptest.NewRecorder()
	quicTaskExecuteHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	if gotMaxRetries != maxRetriesLimit {
		t.Fatalf("max retries not clamped: got %d want %d", gotMaxRetries, maxRetriesLimit)
	}
	if gotBackoffMs != minBackoffMs {
		t.Fatalf("backoff not clamped: got %d want %d", gotBackoffMs, minBackoffMs)
	}
}
