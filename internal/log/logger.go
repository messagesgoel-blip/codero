package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

var (
	// mu protects the global logger and out writer.
	mu     sync.RWMutex
	logger *slog.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	out    io.Writer    = os.Stdout
)

// Init initializes the global logger. It is safe for concurrent use.
func Init(level, path string) error {
	mu.Lock()
	defer mu.Unlock()

	var newOut io.Writer = os.Stdout
	if path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		newOut = f
	}

	// Close the old file if it was a file and not os.Stdout.
	if oldFile, ok := out.(*os.File); ok && oldFile != os.Stdout && oldFile != os.Stderr {
		_ = oldFile.Close()
	}

	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	out = newOut
	logger = slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{
		Level: l,
	}))
	return nil
}

// Named fields to ensure consistency.
const (
	FieldEventType = "event_type"
	FieldComponent = "component"
	FieldBranch    = "branch"
	FieldRepo      = "repo"
	FieldSession   = "session"
	FieldFromState = "from_state"
	FieldToState   = "to_state"
)

// Event types.
const (
	EventStartup    = "startup"
	EventShutdown   = "shutdown"
	EventTransition = "transition"
	EventRejection  = "rejection"
	EventSystem     = "system"
)

// Log helpers.
func Debug(msg string, args ...any) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Debug(msg, args...)
}

func Info(msg string, args ...any) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Info(msg, args...)
}

func Warn(msg string, args ...any) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Warn(msg, args...)
}

func Error(msg string, args ...any) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Error(msg, args...)
}

func With(args ...any) *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return logger.With(args...)
}
