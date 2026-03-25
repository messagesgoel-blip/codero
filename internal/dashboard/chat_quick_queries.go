package dashboard

import "strings"

// quickQueryMap maps "/" prefix shortcuts to their natural-language expansions
// per LiteLLM Chat v1 spec §4.4.
var quickQueryMap = map[string]string{
	"/status":  "What is the current status of all active sessions?",
	"/queue":   "What tasks are in the queue and what are their priorities?",
	"/prs":     "What are the open PRs and their review status?",
	"/recent":  "What are the 5 most recent completed tasks?",
	"/blocked": "Are any sessions currently blocked? Why?",
	"/health":  "Is the system healthy? Any issues?",
}

// quickQueryPrefixes maps prefix-based shortcuts that take an argument.
var quickQueryPrefixes = map[string]string{
	"/agent ":   "What is agent %s doing right now?",
	"/session ": "Give me details on session %s",
}

// ExpandQuickQuery expands a / prefix shortcut to its natural language form.
// If the input is not a quick query, it is returned unchanged.
// The second return value indicates whether expansion occurred.
func ExpandQuickQuery(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return input, false
	}

	lower := strings.ToLower(trimmed)
	if expanded, ok := quickQueryMap[lower]; ok {
		return expanded, true
	}

	for prefix, tmpl := range quickQueryPrefixes {
		if strings.HasPrefix(lower, prefix) {
			arg := strings.TrimSpace(trimmed[len(prefix):])
			if arg != "" {
				return strings.Replace(tmpl, "%s", arg, 1), true
			}
		}
	}

	return input, false
}
