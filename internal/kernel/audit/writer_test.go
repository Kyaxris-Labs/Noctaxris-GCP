package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
)

func TestWriterNilSafe(t *testing.T) {
	var w *audit.Writer
	w.SetSink(nil)
	if err := w.Write(context.Background(), audit.Event{}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriterAppendAndSink(t *testing.T) {
	dir := t.TempDir()
	w, err := audit.NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	granted := true
	ev := audit.Event{
		InsertID:       "id-1",
		Timestamp:      time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
		Severity:       "INFO",
		PrincipalEmail: "root@example.com",
		MethodName:     "test.Method",
		ResourceName:   "projects/p/resources/r",
		Permission:     "res.get",
		Granted:        &granted,
		StatusCode:     200,
		RequestID:      "req-1",
		ServiceName:    "lab.googleapis.com",
		Message:        "ok",
	}
	if err := w.Write(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "audit.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(data))
	var got audit.Event
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatal(err)
	}
	if got.InsertID != ev.InsertID || got.MethodName != ev.MethodName || got.PrincipalEmail != ev.PrincipalEmail {
		t.Fatalf("got=%+v", got)
	}
	called := false
	w.SetSink(func(ctx context.Context, e audit.Event) error {
		called = true
		if e.InsertID != "id-2" {
			t.Fatalf("sink event=%+v", e)
		}
		return nil
	})
	if err := w.Write(context.Background(), audit.Event{InsertID: "id-2"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("sink not called")
	}

	w.SetSink(func(context.Context, audit.Event) error {
		return errors.New("sink boom")
	})
	if err := w.Write(context.Background(), audit.Event{InsertID: "id-3"}); err == nil || !strings.Contains(err.Error(), "sink") {
		t.Fatalf("expected sink error, got %v", err)
	}

	w.SetSink(nil)
	if err := w.Write(context.Background(), audit.Event{InsertID: "id-4"}); err != nil {
		t.Fatal(err)
	}
}

func TestWriterCanceledContext(t *testing.T) {
	w, err := audit.NewWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Write(ctx, audit.Event{InsertID: "x"}); err == nil {
		t.Fatal("expected canceled error")
	}
}

func TestWriterCloseIdempotent(t *testing.T) {
	w, err := audit.NewWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
