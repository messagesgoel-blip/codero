//go:build e2e

// tests/e2e/ocl012_chat_removal_test.go
//
// OCL-012 E2E: Verify chat module was properly removed and proxy works.
// Tests 1-2 check static code properties (no OpenAI imports, no CODERO_CHAT_ env vars).
// Tests 3-4 verify runtime behavior (require CODERO_E2E_LIVE=1).

package e2e

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOCL012_NoOpenAIImports(t *testing.T) {
	repoRoot := filepath.Join(".", "..", "..")

	// Note: go.mod still has openai-go because services/openclaw-adapter/ uses it.
	// We only verify internal/ and cmd/ are clean.

	// Walk all .go files under internal/ and cmd/
	checkDirs := []string{
		filepath.Join(repoRoot, "internal"),
		filepath.Join(repoRoot, "cmd"),
	}

	for _, baseDir := range checkDirs {
		err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Skip vendor and .git directories
			if d.IsDir() && (d.Name() == "vendor" || d.Name() == ".git") {
				return fs.SkipDir
			}

			// Only check .go files
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read %s: %v", path, err)
				return nil
			}

			if strings.Contains(string(content), `"github.com/openai/openai-go"`) {
				t.Errorf("%s contains OpenAI import", path)
			}

			return nil
		})

		if err != nil {
			t.Errorf("walk %s: %v", baseDir, err)
		}
	}
}

func TestOCL012_NoChatEnvVars(t *testing.T) {
	repoRoot := filepath.Join(".", "..", "..")

	// Walk all .go files under internal/ and cmd/
	checkDirs := []string{
		filepath.Join(repoRoot, "internal"),
		filepath.Join(repoRoot, "cmd"),
	}

	for _, baseDir := range checkDirs {
		err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Skip vendor and .git directories
			if d.IsDir() && (d.Name() == "vendor" || d.Name() == ".git") {
				return fs.SkipDir
			}

			// Only check .go files
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read %s: %v", path, err)
				return nil
			}

			lines := strings.Split(string(content), "\n")
			for lineNum, line := range lines {
				// Skip comments
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") {
					continue
				}

				if strings.Contains(line, "CODERO_CHAT_") {
					t.Errorf("%s:%d contains CODERO_CHAT_ reference", path, lineNum+1)
				}
			}

			return nil
		})

		if err != nil {
			t.Errorf("walk %s: %v", baseDir, err)
		}
	}
}

func TestOCL012_ChatEndpointProxies(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	body := strings.NewReader(`{"prompt":"test"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, testAPIURL()+"/api/v1/dashboard/chat", body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/dashboard/chat: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// Accept 200 (proxy working, adapter responded) or 502 (proxy working, adapter not running)
	if resp.StatusCode != 200 && resp.StatusCode != 502 {
		t.Fatalf("expected 200 or 502, got %d: %s", resp.StatusCode, respBody)
	}

	// If 200, verify response has "response" field
	if resp.StatusCode == 200 {
		var result map[string]interface{}
		if err := json.Unmarshal(respBody, &result); err != nil {
			t.Fatalf("parse response: %v (body: %s)", err, respBody)
		}
		if result["response"] == nil {
			t.Error("expected response field in 200 response")
		}
	}

	t.Logf("Chat endpoint proxy status: %d (expected 200 or 502)", resp.StatusCode)
}

func TestOCL012_ChatHistoryGone(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testAPIURL()+"/api/v1/chat/history", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/chat/history: %v", err)
	}
	defer resp.Body.Close()

	// Expect 404 - route no longer registered
	if resp.StatusCode != 404 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 404, got %d: %s", resp.StatusCode, body)
	}
}
