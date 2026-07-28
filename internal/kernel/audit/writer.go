package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is a lab audit record (Cloud Audit Logs shaped, not AWS CloudTrail).
type Event struct {
	InsertID         string    `json:"insertId"`
	Timestamp        time.Time `json:"timestamp"`
	Severity         string    `json:"severity,omitempty"`
	PrincipalEmail   string    `json:"principalEmail,omitempty"`
	MethodName       string    `json:"methodName,omitempty"`
	ResourceName     string    `json:"resourceName,omitempty"`
	Permission       string    `json:"permission,omitempty"`
	Granted          *bool     `json:"granted,omitempty"`
	StatusCode       int       `json:"statusCode,omitempty"`
	RequestID        string    `json:"requestId,omitempty"`
	ServiceName      string    `json:"serviceName,omitempty"`
	Message          string    `json:"message,omitempty"`
}

// Writer appends JSON lines to audit.jsonl under a directory.
type Writer struct {
	path string
	file *os.File
	mu   sync.Mutex
}

// NewWriter opens or creates dir/audit.jsonl.
func NewWriter(dir string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: create dir: %w", err)
	}
	path := filepath.Join(dir, "audit.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open events file: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("audit: chmod events file: %w", err)
	}
	return &Writer{path: path, file: f}, nil
}

// Write marshals ev as one JSON object and appends it as a single line.
func (w *Writer) Write(ctx context.Context, ev Event) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("audit: marshal event: %w", err)
	}
	w.mu.Lock()
	_, err = w.file.Write(append(data, '\n'))
	w.mu.Unlock()
	if err != nil {
		return fmt.Errorf("audit: write event: %w", err)
	}
	return nil
}

// Close releases the underlying file handle.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
