package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
)

// readAuditEntries reads the JSONL audit log and returns up to limit entries, newest first.
// Returns empty slice (not error) if file doesn't exist.
// Oversized or malformed lines are skipped rather than aborting the read.
func readAuditEntries(path string, limit int) ([]auditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []auditEntry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []auditEntry
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var e auditEntry
			if jsonErr := json.Unmarshal(line, &e); jsonErr != nil {
				log.Printf("audit: skip malformed line (%d bytes): %v", len(line), jsonErr)
				continue
			}
			all = append(all, e)
		}
		if err != nil {
			break // EOF or read error
		}
	}

	// Reverse: newest first
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}

	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return all, nil
}
