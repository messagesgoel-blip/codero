package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadAuditEntries_MissingFile(t *testing.T) {
	entries, err := readAuditEntries("/nonexistent/file.jsonl", 10)
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(entries))
	}
}

func TestReadAuditEntries_EmptyFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(f, []byte{}, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	entries, err := readAuditEntries(f, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty slice, got %d", len(entries))
	}
}

func TestReadAuditEntries_SingleEntry(t *testing.T) {
	e := auditEntry{
		Timestamp:      time.Now().UTC(),
		ConversationID: "conv-1",
		Prompt:         "hello",
		Response:       "world",
		StateAvailable: true,
		DurationMs:     42,
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	f := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(f, append(data, '\n'), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	entries, err := readAuditEntries(f, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Prompt != "hello" {
		t.Fatalf("expected prompt 'hello', got %q", entries[0].Prompt)
	}
}

func TestReadAuditEntries_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "audit.jsonl")
	file, err := os.Create(f)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	for i := 0; i < 5; i++ {
		e := auditEntry{
			Timestamp: time.Date(2025, 1, 1, 0, i, 0, 0, time.UTC),
			Prompt:    "q" + string(rune('A'+i)),
		}
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	file.Close()

	entries, err := readAuditEntries(f, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	// Newest (minute=4) should be first
	if entries[0].Prompt != "qE" {
		t.Fatalf("expected newest first (qE), got %q", entries[0].Prompt)
	}
	if entries[4].Prompt != "qA" {
		t.Fatalf("expected oldest last (qA), got %q", entries[4].Prompt)
	}
}

func TestReadAuditEntries_LimitRespected(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "audit.jsonl")
	file, err := os.Create(f)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	for i := 0; i < 10; i++ {
		e := auditEntry{Timestamp: time.Now().UTC(), Prompt: "q"}
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	file.Close()

	entries, err := readAuditEntries(f, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestReadAuditEntries_SkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "audit.jsonl")
	e := auditEntry{Timestamp: time.Now().UTC(), Prompt: "valid"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	content := string(data) + "\n{invalid json\n" + string(data) + "\n"
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	entries, err := readAuditEntries(f, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 valid entries (skipping malformed), got %d", len(entries))
	}
}
