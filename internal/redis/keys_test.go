package redis

import (
	"errors"
	"testing"
)

func TestBuildKey_ValidInput(t *testing.T) {
	tests := []struct {
		repo, kType, id string
		want            string
	}{
		{"acme/api", "lease", "main", "codero:acme/api:lease:main"},
		{"owner/repo", "queue", "feat-x", "codero:owner/repo:queue:feat-x"},
		{"org/svc", "heartbeat", "session-1", "codero:org/svc:heartbeat:session-1"},
	}
	for _, tt := range tests {
		got, err := BuildKey(tt.repo, tt.kType, tt.id)
		if err != nil {
			t.Errorf("BuildKey(%q,%q,%q): unexpected error: %v", tt.repo, tt.kType, tt.id, err)
			continue
		}
		if got != tt.want {
			t.Errorf("BuildKey(%q,%q,%q) = %q, want %q", tt.repo, tt.kType, tt.id, got, tt.want)
		}
	}
}

func TestBuildKey_InvalidInput(t *testing.T) {
	tests := []struct {
		repo, kType, id string
		desc            string
	}{
		{"", "lease", "main", "empty repo"},
		{"acme/api", "", "main", "empty kType"},
		{"acme/api", "lease", "", "empty id"},
		{"acme:api", "lease", "main", "colon in repo"},
		{"acme/api", "lea:se", "main", "colon in kType"},
		{"acme/api", "lease", "ma:in", "colon in id"},
	}
	for _, tt := range tests {
		_, err := BuildKey(tt.repo, tt.kType, tt.id)
		if err == nil {
			t.Errorf("BuildKey(%q,%q,%q) [%s]: expected error, got nil", tt.repo, tt.kType, tt.id, tt.desc)
			continue
		}
		if !errors.Is(err, ErrInvalidKeyPart) {
			t.Errorf("BuildKey [%s]: want ErrInvalidKeyPart, got %v", tt.desc, err)
		}
	}
}

func TestBuildKey_FormatPrefix(t *testing.T) {
	k, err := BuildKey("r/x", "t", "i")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const prefix = "codero:"
	if len(k) < len(prefix) || k[:len(prefix)] != prefix {
		t.Errorf("key %q does not start with %q", k, prefix)
	}
}

func TestMustBuildKey_PanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustBuildKey with empty part: expected panic, got none")
		}
	}()
	MustBuildKey("", "type", "id")
}
