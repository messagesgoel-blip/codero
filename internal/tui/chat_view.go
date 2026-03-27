package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codero/codero/internal/dashboard"
)

const chatContextBanner = "Codero TUI review shell"

type terminalChatStream struct {
	prompt         string
	tab            string
	conversationID string
	resp           *http.Response
	reader         *bufio.Reader
}

func (s *terminalChatStream) Close() {
	if s == nil || s.resp == nil || s.resp.Body == nil {
		return
	}
	_ = s.resp.Body.Close()
}

type terminalChatStreamStartMsg struct {
	stream *terminalChatStream
}

type terminalChatStreamDeltaMsg struct {
	stream *terminalChatStream
	delta  string
}

func dashboardChatStreamCmd(ctx context.Context, prompt, tab, conversationID string) tea.Cmd {
	return func() tea.Msg {
		if ctx == nil {
			ctx = context.Background()
		}
		reqBody := dashboard.ChatRequest{
			Prompt:         prompt,
			Tab:            tab,
			Context:        chatContextBanner,
			Stream:         true,
			ConversationID: conversationID,
			ContextScope:   chatContextScopeForTab(tab),
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("marshal chat stream request: %w", err)}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, dashboardChatEndpoint(), bytes.NewReader(body))
		if err != nil {
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("create chat stream request: %w", err)}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := dashboardChatHTTPClient.Do(req)
		if err != nil {
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("send chat stream request: %w", err)}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
			resp.Body.Close()
			if readErr != nil {
				return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("read chat stream error body: %w", readErr)}
			}
			msg := strings.TrimSpace(string(raw))
			if msg == "" {
				msg = resp.Status
			}
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("chat stream %s: %w", resp.Status, errors.New(msg))}
		}

		return terminalChatStreamStartMsg{
			stream: &terminalChatStream{
				prompt:         prompt,
				tab:            tab,
				conversationID: conversationID,
				resp:           resp,
				reader:         bufio.NewReader(resp.Body),
			},
		}
	}
}

func readTerminalChatStreamCmd(stream *terminalChatStream) tea.Cmd {
	return func() tea.Msg {
		if stream == nil || stream.reader == nil {
			if stream != nil {
				stream.Close()
			}
			return terminalChatErrorMsg{err: errors.New("chat stream unavailable")}
		}

		event, data, err := readChatSSEFrame(stream.reader)
		if err != nil {
			stream.Close()
			if errors.Is(err, io.EOF) {
				return terminalChatErrorMsg{prompt: stream.prompt, err: errors.New("chat stream closed before completion")}
			}
			return terminalChatErrorMsg{prompt: stream.prompt, err: fmt.Errorf("read chat stream frame: %w", err)}
		}

		switch strings.ToLower(strings.TrimSpace(event)) {
		case "", "delta":
			var payload struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				return terminalChatErrorMsg{prompt: stream.prompt, err: fmt.Errorf("decode chat delta: %w", err)}
			}
			return terminalChatStreamDeltaMsg{stream: stream, delta: payload.Delta}
		case "done":
			var resp dashboard.ChatResponse
			if err := json.Unmarshal([]byte(data), &resp); err != nil {
				return terminalChatErrorMsg{prompt: stream.prompt, err: fmt.Errorf("decode chat response: %w", err)}
			}
			if strings.TrimSpace(resp.ConversationID) == "" {
				resp.ConversationID = stream.conversationID
			}
			stream.Close()
			return terminalChatResultMsg{prompt: stream.prompt, response: resp}
		default:
			return terminalChatStreamDeltaMsg{stream: stream}
		}
	}
}

func readChatSSEFrame(r *bufio.Reader) (event string, data string, err error) {
	var dataLines []string
	for {
		line, readErr := r.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", "", readErr
		}
		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			if event != "" || len(dataLines) > 0 {
				return event, strings.Join(dataLines, "\n"), nil
			}
			if errors.Is(readErr, io.EOF) {
				return "", "", io.EOF
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case strings.HasPrefix(line, ":"):
			// Comment/heartbeat line.
		}

		if errors.Is(readErr, io.EOF) {
			if event != "" || len(dataLines) > 0 {
				return event, strings.Join(dataLines, "\n"), nil
			}
			return "", "", io.EOF
		}
	}
}

func chatContextScopeForTab(tab string) string {
	switch strings.ToLower(strings.TrimSpace(tab)) {
	case "queue":
		return "queue"
	case "events":
		return "sessions"
	case "archives":
		return "archives"
	default:
		return "all"
	}
}
