package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// FrameType identifies the type of QUIC frame
type FrameType byte

const (
	FrameTypeTask     FrameType = 0x01
	FrameTypeResponse FrameType = 0x02
	FrameTypeError    FrameType = 0x03
	FrameTypePing     FrameType = 0x04
	FrameTypePong     FrameType = 0x05
)

const (
	maxFrameDataSize = 16777215
	deadlineSkew     = time.Second
)

// TaskFrame is the binary protocol structure for task requests
type TaskFrame struct {
	TaskID     string
	Region     string
	Deadline   int64 // Unix nanos
	Payload    []byte
	RetryCount int
	MaxRetries int
	BackoffMs  int
}

// ResponseFrame is the binary protocol structure for task responses
type ResponseFrame struct {
	TaskID   string
	Status   int
	Payload  []byte
	Error    string
	Duration int64 // millis
}

// ErrorFrame is sent when an error occurs
type ErrorFrame struct {
	Code    int
	Message string
}

// Marshal encodes TaskFrame to binary with frame header
func (tf *TaskFrame) Marshal() ([]byte, error) {
	// Task payload: taskID | region | deadline | payload | retryCount | maxRetries | backoffMs
	// Variable-length strings prefixed with 2-byte length
	buf := make([]byte, 0, 512)

	// Task ID (2-byte len + string)
	if len(tf.TaskID) > 65535 {
		return nil, fmt.Errorf("task ID too long: %d bytes", len(tf.TaskID))
	}
	buf = append(buf, byte(len(tf.TaskID)>>8), byte(len(tf.TaskID)))
	buf = append(buf, []byte(tf.TaskID)...)

	// Region (2-byte len + string)
	if len(tf.Region) > 65535 {
		return nil, fmt.Errorf("region too long: %d bytes", len(tf.Region))
	}
	buf = append(buf, byte(len(tf.Region)>>8), byte(len(tf.Region)))
	buf = append(buf, []byte(tf.Region)...)

	// Deadline (8 bytes, int64)
	buf = append(buf, make([]byte, 8)...)
	binary.BigEndian.PutUint64(buf[len(buf)-8:], uint64(tf.Deadline))

	// Payload length (4 bytes) + payload
	if len(tf.Payload) > 16777215 { // 24-bit max
		return nil, fmt.Errorf("payload too large: %d bytes", len(tf.Payload))
	}
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(len(tf.Payload)))
	buf = append(buf, lenBytes...)
	buf = append(buf, tf.Payload...)

	// Retry info (1 byte each)
	buf = append(buf, byte(tf.RetryCount), byte(tf.MaxRetries))
	lenBytes = make([]byte, 2)
	binary.BigEndian.PutUint16(lenBytes, uint16(tf.BackoffMs))
	buf = append(buf, lenBytes...)

	return buf, nil
}

// Unmarshal decodes TaskFrame from binary
func (tf *TaskFrame) Unmarshal(data []byte) error {
	if len(data) < 2 {
		return fmt.Errorf("buffer too small")
	}

	offset := 0

	// Task ID
	if offset+2 > len(data) {
		return fmt.Errorf("incomplete task ID length")
	}
	idLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+idLen > len(data) {
		return fmt.Errorf("incomplete task ID")
	}
	tf.TaskID = string(data[offset : offset+idLen])
	offset += idLen

	// Region
	if offset+2 > len(data) {
		return fmt.Errorf("incomplete region length")
	}
	regLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+regLen > len(data) {
		return fmt.Errorf("incomplete region")
	}
	tf.Region = string(data[offset : offset+regLen])
	offset += regLen

	// Deadline
	if offset+8 > len(data) {
		return fmt.Errorf("incomplete deadline")
	}
	tf.Deadline = int64(binary.BigEndian.Uint64(data[offset : offset+8]))
	offset += 8

	// Payload
	if offset+4 > len(data) {
		return fmt.Errorf("incomplete payload length")
	}
	payLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4
	if offset+payLen > len(data) {
		return fmt.Errorf("incomplete payload")
	}
	tf.Payload = append([]byte{}, data[offset:offset+payLen]...)
	offset += payLen

	// Retry info
	if offset+4 > len(data) {
		return fmt.Errorf("incomplete retry info")
	}
	tf.RetryCount = int(data[offset])
	tf.MaxRetries = int(data[offset+1])
	tf.BackoffMs = int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))

	return nil
}

// Marshal encodes ResponseFrame to binary
func (rf *ResponseFrame) Marshal() ([]byte, error) {
	buf := make([]byte, 0, 512)

	// Task ID (2-byte len + string)
	if len(rf.TaskID) > 65535 {
		return nil, fmt.Errorf("task ID too long")
	}
	buf = append(buf, byte(len(rf.TaskID)>>8), byte(len(rf.TaskID)))
	buf = append(buf, []byte(rf.TaskID)...)

	// Status (4 bytes)
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(rf.Status))
	buf = append(buf, lenBytes...)

	// Payload (4-byte len + data)
	if len(rf.Payload) > 16777215 {
		return nil, fmt.Errorf("payload too large")
	}
	lenBytes = make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(len(rf.Payload)))
	buf = append(buf, lenBytes...)
	buf = append(buf, rf.Payload...)

	// Error (2-byte len + string)
	if len(rf.Error) > 65535 {
		return nil, fmt.Errorf("error message too long")
	}
	buf = append(buf, byte(len(rf.Error)>>8), byte(len(rf.Error)))
	buf = append(buf, []byte(rf.Error)...)

	// Duration (8 bytes)
	lenBytes = make([]byte, 8)
	binary.BigEndian.PutUint64(lenBytes, uint64(rf.Duration))
	buf = append(buf, lenBytes...)

	return buf, nil
}

// Unmarshal decodes ResponseFrame from binary
func (rf *ResponseFrame) Unmarshal(data []byte) error {
	if len(data) < 2 {
		return fmt.Errorf("buffer too small")
	}

	offset := 0

	// Task ID
	if offset+2 > len(data) {
		return fmt.Errorf("incomplete task ID length")
	}
	idLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+idLen > len(data) {
		return fmt.Errorf("incomplete task ID")
	}
	rf.TaskID = string(data[offset : offset+idLen])
	offset += idLen

	// Status
	if offset+4 > len(data) {
		return fmt.Errorf("incomplete status")
	}
	rf.Status = int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	// Payload
	if offset+4 > len(data) {
		return fmt.Errorf("incomplete payload length")
	}
	payLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4
	if offset+payLen > len(data) {
		return fmt.Errorf("incomplete payload")
	}
	rf.Payload = append([]byte{}, data[offset:offset+payLen]...)
	offset += payLen

	// Error
	if offset+2 > len(data) {
		return fmt.Errorf("incomplete error length")
	}
	errLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+errLen > len(data) {
		return fmt.Errorf("incomplete error")
	}
	rf.Error = string(data[offset : offset+errLen])
	offset += errLen

	// Duration
	if offset+8 > len(data) {
		return fmt.Errorf("incomplete duration")
	}
	rf.Duration = int64(binary.BigEndian.Uint64(data[offset : offset+8]))

	return nil
}

// WriteFrame writes a frame (type + length + data) to writer with deadline
func WriteFrame(ctx context.Context, w io.Writer, frameType FrameType, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Extract deadline if available
	deadline, ok := ctx.Deadline()
	if ok {
		// Set write deadline with 1 second buffer before context deadline
		timeUntilDeadline := time.Until(deadline)
		if timeUntilDeadline <= 0 {
			return context.DeadlineExceeded
		}
		if tc, ok := w.(interface{ SetWriteDeadline(time.Time) error }); ok {
			writeDeadline := deadline
			if timeUntilDeadline > deadlineSkew {
				writeDeadline = deadline.Add(-deadlineSkew)
			}
			if err := tc.SetWriteDeadline(writeDeadline); err != nil {
				return fmt.Errorf("failed to set write deadline: %w", err)
			}
		}
	}

	header := make([]byte, 5)
	header[0] = byte(frameType)
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("failed to write frame header: %w", err)
	}

	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("failed to write frame data: %w", err)
		}
	}

	return nil
}

// ReadFrame reads a frame (type + length + data) from reader with deadline
func ReadFrame(ctx context.Context, r io.Reader) (FrameType, []byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}

	// Extract deadline
	deadline, ok := ctx.Deadline()
	if ok {
		if tc, ok := r.(interface{ SetReadDeadline(time.Time) error }); ok {
			readDeadline := deadline
			if time.Until(deadline) > deadlineSkew {
				readDeadline = deadline.Add(-deadlineSkew)
			}
			if err := tc.SetReadDeadline(readDeadline); err != nil {
				return 0, nil, fmt.Errorf("failed to set read deadline: %w", err)
			}
		}
	}

	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		if err == io.EOF {
			return 0, nil, io.EOF
		}
		return 0, nil, fmt.Errorf("failed to read frame header: %w", err)
	}

	frameType := FrameType(header[0])
	dataLen := binary.BigEndian.Uint32(header[1:])

	if dataLen > maxFrameDataSize { // 24-bit max safety check
		return 0, nil, fmt.Errorf("frame data too large: %d bytes", dataLen)
	}

	data := make([]byte, dataLen)
	if dataLen > 0 {
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		if _, err := io.ReadFull(r, data); err != nil {
			return 0, nil, fmt.Errorf("failed to read frame data: %w", err)
		}
	}

	return frameType, data, nil
}

// ExponentialBackoff calculates backoff with jitter
func ExponentialBackoff(attempt int, maxRetries int, baseMs int) time.Duration {
	if attempt >= maxRetries {
		return time.Duration(baseMs*1000) * time.Millisecond // Fall back to base
	}

	// 2^attempt * baseMs, capped at ~30 seconds
	backoffMs := baseMs * (1 << uint(attempt))
	if backoffMs > 30000 {
		backoffMs = 30000
	}

	// Add jitter: ±20% of backoff
	jitterMs := (backoffMs * 20) / 100
	if jitterMs > 0 {
		// Simple pseudo-random without importing rand (use time-based seed)
		jitter := int(time.Now().UnixNano()%int64(jitterMs*2)) - jitterMs
		backoffMs += jitter
	}

	if backoffMs < 0 {
		backoffMs = baseMs
	}

	return time.Duration(backoffMs) * time.Millisecond
}

// TaskPayload is JSON wrapper for task payload
type TaskPayload struct {
	Data     json.RawMessage        `json:"data"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
