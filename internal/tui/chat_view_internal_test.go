package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/dashboard"
)

func TestDashboardChatStreamCmd_ParsesDeltaAndDone(t *testing.T) {
	requestSeen := make(chan dashboard.ChatRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req dashboard.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requestSeen <- req

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: delta\n")
		_, _ = io.WriteString(w, "data: {\"delta\":\"Hello\"}\n\n")
		_, _ = io.WriteString(w, "event: done\n")
		_, _ = io.WriteString(w, "data: {\"reply\":\"Hello world\",\"provider\":\"litellm\",\"model\":\"chat-test\",\"conversation_id\":\"conv-1\"}\n\n")
	}))
	defer srv.Close()

	t.Setenv("CODERO_DASHBOARD_CHAT_URL", srv.URL)

	startMsg := dashboardChatStreamCmd(context.Background(), "status", "review", "conv-1")()
	start, ok := startMsg.(terminalChatStreamStartMsg)
	if !ok {
		t.Fatalf("start msg type = %T, want terminalChatStreamStartMsg", startMsg)
	}
	if start.stream == nil {
		t.Fatal("expected stream to be initialized")
	}
	defer start.stream.Close()

	select {
	case req := <-requestSeen:
		if !req.Stream {
			t.Fatal("expected stream=true in request")
		}
		if req.ConversationID != "conv-1" {
			t.Fatalf("conversation_id = %q, want conv-1", req.ConversationID)
		}
		if req.Prompt != "status" {
			t.Fatalf("prompt = %q, want status", req.Prompt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dashboard request")
	}

	msg1 := readTerminalChatStreamCmd(start.stream)()
	delta, ok := msg1.(terminalChatStreamDeltaMsg)
	if !ok {
		t.Fatalf("first stream msg = %T, want terminalChatStreamDeltaMsg", msg1)
	}
	if delta.delta != "Hello" {
		t.Fatalf("delta = %q, want Hello", delta.delta)
	}

	msg2 := readTerminalChatStreamCmd(delta.stream)()
	result, ok := msg2.(terminalChatResultMsg)
	if !ok {
		t.Fatalf("second stream msg = %T, want terminalChatResultMsg", msg2)
	}
	if result.response.Reply != "Hello world" {
		t.Fatalf("reply = %q, want Hello world", result.response.Reply)
	}
	if result.response.ConversationID != "conv-1" {
		t.Fatalf("conversation_id = %q, want conv-1", result.response.ConversationID)
	}
}

func TestRenderChatPane_ShowsThreadAndInput(t *testing.T) {
	m := New(Config{Theme: DefaultTheme})
	m.layout = Compute(100, 24)
	m.chatActive = true
	m.chatConversationID = "conv-7"
	m.cliBusy = true
	m.cliInput.SetValue("How many active sessions?")
	m.cliSuggestions = []dashboard.ChatSuggestion{{Label: "status", Prompt: "status"}}
	m.cliMessages = []terminalMessage{
		{Role: "system", Meta: "codero", Content: "Type help, status, gate, queue, or ask a review question."},
		{Role: "user", Content: "status"},
		{Role: "assistant", Meta: "streaming", Content: "Working on it"},
	}

	view := m.renderChatPane()
	for _, want := range []string{
		"REVIEW CHAT",
		"conversation conv-7",
		"streaming",
		"status",
		"How many active sessions?",
		"Working on it",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("rendered chat pane missing %q\n%s", want, view)
		}
	}
}

func TestReadTerminalChatStreamCmd_EmptyEOFReturnsError(t *testing.T) {
	stream := &terminalChatStream{
		prompt: "status",
		reader: bufio.NewReader(strings.NewReader("")),
	}

	msg := readTerminalChatStreamCmd(stream)()
	if _, ok := msg.(terminalChatErrorMsg); !ok {
		t.Fatalf("msg type = %T, want terminalChatErrorMsg", msg)
	}
}
