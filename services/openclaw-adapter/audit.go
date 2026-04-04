package main

import (
	"bufio"
	"encoding/json"
	"os"
)

// readAuditEntries reads the JSONL audit log and returns up to limit entries, newest first.
// Returns empty slice (not error) if file doesn't exist.
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
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var e auditEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue // skip malformed lines
		}
		all = append(all, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
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
