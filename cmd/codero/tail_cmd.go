package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// tailCmd implements `codero tail [session_id]`.
// With a session ID it streams the per-session output tail file.
// Without arguments it lists the most-recent tail files.
func tailCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "tail [session_id]",
		Short: "Stream captured output from a running or recent agent session",
		Long: `Stream the captured stdout of an agent session managed by codero agent run.

Output is captured under CODERO_TAIL_DIR (default: codero-tails in os.TempDir()) as <session-id>.log (capped at 4 MB).
Without a session ID, lists the 10 most-recent tail files.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listTailFiles()
			}
			sessionID := args[0]
			if sessionID == "" || strings.ContainsAny(sessionID, `/\.`) {
				return fmt.Errorf("invalid session_id %q: must be a plain session token with no path separators or traversal tokens", sessionID)
			}
			logPath := tailPath(sessionID)
			return streamTailFile(logPath, follow)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new output as it arrives (like tail -f)")
	return cmd
}

// listTailFiles prints the 10 most-recently modified tail files.
func listTailFiles() error {
	dir := tailDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no tail files found (no sessions have run yet)")
			return nil
		}
		return fmt.Errorf("read tail dir: %w", err)
	}

	type entry struct {
		name    string
		modTime time.Time
		size    int64
	}
	var files []entry
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, entry{
			name:    e.Name(),
			modTime: info.ModTime(),
			size:    info.Size(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	if len(files) == 0 {
		fmt.Println("no tail files found")
		return nil
	}

	limit := 10
	if len(files) < limit {
		limit = len(files)
	}
	fmt.Printf("%-40s  %-8s  %s\n", "session_id", "size", "modified")
	fmt.Println(strings.Repeat("-", 72))
	for _, f := range files[:limit] {
		sessionID := strings.TrimSuffix(f.name, ".log")
		fmt.Printf("%-40s  %-8s  %s\n",
			sessionID,
			formatBytes(f.size),
			f.modTime.Local().Format("2006-01-02 15:04:05"),
		)
	}
	return nil
}

// streamTailFile dumps (and optionally follows) a tail file to stdout.
func streamTailFile(logPath string, follow bool) error {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("tail file not found: %s\n(session may not have produced output yet or has been cleaned up)", filepath.Base(logPath))
		}
		return fmt.Errorf("open tail file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(os.Stdout, f); err != nil {
		return fmt.Errorf("read tail file: %w", err)
	}

	if !follow {
		return nil
	}

	// Follow mode: poll for new data every 250ms.
	for {
		time.Sleep(250 * time.Millisecond)
		_, err := io.Copy(os.Stdout, f)
		if err != nil && err != io.EOF {
			return fmt.Errorf("follow tail file: %w", err)
		}
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
