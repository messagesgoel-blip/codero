package dashboard

import "strings"

// QuickQueryExpansions maps slash-prefix quick queries to their expanded
// natural-language prompts (LiteLLM Chat v1 §4.4).
var QuickQueryExpansions = map[string]string{
	"/prs":     "Show open pull requests with their gate-check status and any blocking findings.",
	"/agent":   "What is the current agent status, including active sessions and recent completions?",
	"/session": "Describe all active agent sessions, their uptime, and assigned repositories.",
	"/recent":  "List the most recent activity: gate runs, agent events, and status changes.",
	"/blocked": "Which items are currently blocked? Show block reasons and required actions.",
	"/health":  "Show system health: agent connectivity, LiteLLM availability, and gate-check status.",
}

// expandQuickQuery rewrites a slash-prefix quick query into a full prompt.
// Returns the expanded prompt and true, or the original prompt and false.
func expandQuickQuery(prompt string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	for prefix, expansion := range QuickQueryExpansions {
		if lower == prefix || strings.HasPrefix(lower, prefix+" ") {
			extra := strings.TrimSpace(prompt[len(prefix):])
			if extra != "" {
				return expansion + " Focus on: " + extra, true
			}
			return expansion, true
		}
	}
	return prompt, false
}

// ExpandQuickQueryForTest is the exported wrapper for certification tests.
func ExpandQuickQueryForTest(prompt string) (string, bool) {
	return expandQuickQuery(prompt)
}
