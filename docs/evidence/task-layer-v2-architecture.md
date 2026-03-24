# Task Layer v2 — Architecture Evidence

## I-44: `internal/github` is the Sole GitHub Import

The `internal/github` package is the **only** package in the codebase that communicates with the GitHub REST API. It uses raw `net/http` with Bearer token authentication — no external GitHub SDK (e.g., `go-github`) is imported.

**Evidence:**
- `internal/github/client.go` imports only stdlib packages and `internal/webhook`
- `go.sum` contains no `github.com/google/go-github` entry
- All other packages that need GitHub data consume it through the `webhook.GitHubClient` interface, which `internal/github.Client` implements

**Verification command:**
```bash
grep -r 'go-github' go.sum go.mod  # must return no results
grep -r 'github.com/google/go-github' internal/ cmd/  # must return no results
```

## I-49: `GetLinkByTaskID` is the Sole Task→PR/Issue Resolver

`GetLinkByTaskID` in `internal/state/github_links.go` is the canonical and sole mechanism for resolving a Codero task ID to its associated GitHub PR/issue link.

**Evidence:**
- `GetLinkByTaskID(ctx, db, taskID)` queries `codero_github_links` by `task_id` (unique column)
- The alternative query functions `GetLinkByRepoPR` and `GetLinkByBranch` exist for webhook/reconciler use cases (incoming GitHub events need to resolve by PR number or branch name), but they are **not** used for task→PR resolution
- The daemon and CLI always resolve task links via `GetLinkByTaskID`

**Verification:**
```bash
grep -rn 'GetLinkByRepoPR\|GetLinkByBranch' internal/ cmd/ --include='*.go' | grep -v '_test.go'
# These should appear only in webhook/reconciler paths, not in task resolution paths
```

## §12.4: Handoff is Codero-Managed

Task handoff is managed exclusively by the Codero daemon, not by agents.

**Evidence:**
- `config.Sweeper.HandoffTTL` controls the timeout for handoff-waiting assignments
- `ReconcileAgentAssignmentWaitingState` in `internal/state/agent_sessions.go` manages the transition — agents cannot invoke handoff directly
- `successor_session_id` on `agent_assignments` is the nomination mechanism (I-41), enforced during `AcceptTask`
- Agents may only emit substatus transitions; they cannot directly manipulate handoff state
