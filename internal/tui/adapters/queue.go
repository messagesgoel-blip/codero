package adapters

import (
	"fmt"
	"time"

	"github.com/codero/codero/internal/state"
)

// QueueItem is the display-ready model for a branch in the operator queue.
type QueueItem struct {
	ID          string
	Repo        string
	Branch      string
	State       string
	Priority    int
	RetryCount  int
	MaxRetries  int
	WaitingSec  int
	HeadHash    string
	Approved    bool
	CIGreen     bool
	DisplayLine string
}

// FromBranchRecord converts a state.BranchRecord to a QueueItem.
func FromBranchRecord(r state.BranchRecord) QueueItem {
	var waitSec int
	if r.SubmissionTime != nil {
		waitSec = int(time.Since(*r.SubmissionTime).Seconds())
	}

	shortHash := r.HeadHash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}

	display := fmt.Sprintf("%-24s  %-14s  %s", truncBranch(r.Branch, 24), string(r.State), shortHash)

	return QueueItem{
		ID:          r.ID,
		Repo:        r.Repo,
		Branch:      r.Branch,
		State:       string(r.State),
		Priority:    r.QueuePriority,
		RetryCount:  r.RetryCount,
		MaxRetries:  r.MaxRetries,
		WaitingSec:  waitSec,
		HeadHash:    shortHash,
		Approved:    r.Approved,
		CIGreen:     r.CIGreen,
		DisplayLine: display,
	}
}

// FromBranchRecords converts a slice of BranchRecords to QueueItems.
func FromBranchRecords(rs []state.BranchRecord) []QueueItem {
	items := make([]QueueItem, len(rs))
	for i, r := range rs {
		items[i] = FromBranchRecord(r)
	}
	return items
}

func truncBranch(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
