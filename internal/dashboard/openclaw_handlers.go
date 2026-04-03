package dashboard

import (
	"net/http"
	"time"

	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// ─── OCL-010: OpenClaw structured state endpoint ─────────────────────────

// OpenClawStateResponse is the response for GET /api/v1/openclaw/state.
// It provides a structured snapshot of system state for OpenClaw consumption.
type OpenClawStateResponse struct {
	Sessions    []ActiveSession    `json:"sessions"`
	Pipeline    []PipelineCard     `json:"pipeline"`
	Activity    []ActivityEvent    `json:"activity"`
	GateHealth  OpenClawGateHealth `json:"gate_health"`
	Scorecard   OpenClawScorecard  `json:"scorecard"`
	GeneratedAt time.Time          `json:"generated_at"`
}

// OpenClawGateHealth is the gate health summary for the openclaw state response.
type OpenClawGateHealth struct {
	Providers []GateHealth `json:"providers"`
	Summary   string       `json:"summary"`
}

// OpenClawScorecard is the scorecard summary for the openclaw state response.
type OpenClawScorecard struct {
	GatePassRate    string `json:"gate_pass_rate"`
	AvgCycleTime    string `json:"avg_cycle_time"`
	MergeRate       string `json:"merge_rate"`
	ComplianceScore string `json:"compliance_score"`
	Summary         string `json:"summary"`
}

func (h *Handler) handleOpenClawState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	ctx := r.Context()

	// Sessions — reuse the active sessions query with a generous limit.
	sessions, err := queryActiveSessions(ctx, h.db, 50)
	if err != nil {
		loglib.Error("openclaw: sessions query failed", "error", err)
		sessions = []ActiveSession{}
	}
	if sessions == nil {
		sessions = []ActiveSession{}
	}

	// Pipeline — reuse the pipeline query.
	pipeline, err := queryPipeline(ctx, h.db)
	if err != nil {
		loglib.Error("openclaw: pipeline query failed", "error", err)
		pipeline = []PipelineCard{}
	}
	if pipeline == nil {
		pipeline = []PipelineCard{}
	}

	// Activity — last 20 events.
	activity, err := queryActivity(ctx, h.db, 20)
	if err != nil {
		loglib.Error("openclaw: activity query failed", "error", err)
		activity = []ActivityEvent{}
	}
	if activity == nil {
		activity = []ActivityEvent{}
	}

	// Gate health — per-provider pass rates.
	providers, err := queryGateHealth(ctx, h.db)
	if err != nil {
		loglib.Error("openclaw: gate health query failed", "error", err)
		providers = []GateHealth{}
	}
	if providers == nil {
		providers = []GateHealth{}
	}
	ghSummary := "no data"
	if len(providers) > 0 {
		totalRuns := 0
		totalPassed := 0
		for _, p := range providers {
			totalRuns += p.Total
			totalPassed += p.Passed
		}
		if totalRuns > 0 {
			pct := float64(totalPassed) / float64(totalRuns) * 100
			ghSummary = formatPercent(pct) + " overall pass rate across " + formatInt(totalRuns) + " runs"
		}
	}

	// Scorecard — proving period metrics.
	var scorecard OpenClawScorecard
	card, err := state.ComputeProvingScorecard(ctx, state.NewDB(h.db))
	if err != nil {
		loglib.Error("openclaw: scorecard compute failed", "error", err)
		scorecard = OpenClawScorecard{
			GatePassRate:    "—",
			AvgCycleTime:    "—",
			MergeRate:       "—",
			ComplianceScore: "—",
			Summary:         "scorecard unavailable",
		}
	} else {
		gatePassRate := "—"
		if card.PrecommitReviews7Days > 0 {
			gatePassRate = formatInt(card.PrecommitReviews7Days) + " runs"
		}
		mergeRate := "—"
		if card.BranchesReviewed7Days > 0 {
			mergeRate = formatInt(card.BranchesReviewed7Days) + "/wk"
		}
		complianceScore := "100%"
		if card.ManualDBRepairs > 0 || card.MissedFeedbackDeliveries > 0 {
			complianceScore = "needs attention"
		}
		scorecard = OpenClawScorecard{
			GatePassRate:    gatePassRate,
			AvgCycleTime:    "—",
			MergeRate:       mergeRate,
			ComplianceScore: complianceScore,
			Summary: formatScorecardSummary(
				card.BranchesReviewed7Days,
				card.PrecommitReviews7Days,
				card.StaleDetections30Days,
			),
		}
	}

	writeJSON(w, http.StatusOK, OpenClawStateResponse{
		Sessions: sessions,
		Pipeline: pipeline,
		Activity: activity,
		GateHealth: OpenClawGateHealth{
			Providers: providers,
			Summary:   ghSummary,
		},
		Scorecard:   scorecard,
		GeneratedAt: time.Now().UTC(),
	})
}

func formatPercent(pct float64) string {
	if pct == float64(int(pct)) {
		return formatInt(int(pct)) + "%"
	}
	return formatFloat(pct) + "%"
}

func formatInt(n int) string {
	return intToStr(n)
}

func formatFloat(f float64) string {
	// Avoid importing strconv for a simple format.
	return floatToStr(f)
}

func formatScorecardSummary(branches, precommits, stale int) string {
	return intToStr(branches) + " branches reviewed, " +
		intToStr(precommits) + " precommit runs, " +
		intToStr(stale) + " stale detections (7d/7d/30d)"
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + intToStr(-n)
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	// Reverse.
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

func floatToStr(f float64) string {
	// Simple format: integer part + 1 decimal place.
	intPart := int(f)
	frac := int((f - float64(intPart)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return intToStr(intPart) + "." + intToStr(frac)
}
