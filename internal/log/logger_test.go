package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
)

func TestLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	testLogger := slog.New(handler)

	testLogger.Info("test message",
		FieldEventType, EventStartup,
		FieldComponent, "daemon",
	)

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if m["msg"] != "test message" {
		t.Errorf("expected msg to be %q, got %q", "test message", m["msg"])
	}
	if m[FieldEventType] != EventStartup {
		t.Errorf("expected %s to be %q, got %q", FieldEventType, EventStartup, m[FieldEventType])
	}
}

func TestLogger_Init(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "codero-log-*.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	err = Init("info", tmpFile.Name())
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	Info("init test", FieldEventType, EventStartup, FieldComponent, "daemon")

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal JSON from file: %v", err)
	}

	if m["msg"] != "init test" {
		t.Errorf("expected msg to be %q, got %q", "init test", m["msg"])
	}
	if m[FieldEventType] != EventStartup {
		t.Errorf("expected %s to be %q, got %q", FieldEventType, EventStartup, m[FieldEventType])
	}
}

func TestLogger_FieldConsistency(t *testing.T) {
	if FieldEventType != "event_type" {
		t.Error("FieldEventType mismatch")
	}
	if FieldComponent != "component" {
		t.Error("FieldComponent mismatch")
	}
}
