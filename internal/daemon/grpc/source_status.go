package grpc

import (
	"encoding/json"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
)

const sourceStatusCompliance = "compliance"

func parseSourceStatuses(raw string) map[string]string {
	if raw == "" {
		return map[string]string{}
	}

	var statuses map[string]string
	if err := json.Unmarshal([]byte(raw), &statuses); err == nil && statuses != nil {
		return statuses
	}

	return map[string]string{sourceStatusCompliance: raw}
}

func marshalSourceStatuses(statuses map[string]string) (string, error) {
	if len(statuses) == 0 {
		return "{}", nil
	}

	encoded, err := json.Marshal(statuses)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func feedbackStatusFromSourceStatus(raw string) (daemonv1.FeedbackStatus, string) {
	status := parseSourceStatuses(raw)[sourceStatusCompliance]
	switch status {
	case "actionable", "fail", "warn":
		return daemonv1.FeedbackStatus_FEEDBACK_STATUS_ACTIONABLE, "needs_revision"
	case "pass", "resolved":
		return daemonv1.FeedbackStatus_FEEDBACK_STATUS_RESOLVED, "ready"
	default:
		return daemonv1.FeedbackStatus_FEEDBACK_STATUS_INFORMATIONAL, "informational"
	}
}
