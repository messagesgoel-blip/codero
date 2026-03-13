# codero

## Implementation Roadmap v4 — Implementation-Ready

**Two Phases · Redis as Coordination Layer · Task IDs for Agent Execution**

Phase 1 builds and proves the system through personal use. Phase 2 begins only after ~6 months of real-world validation. All commercialisation is deferred to Phase 2. Redis is present in both phases as the ephemeral coordination layer — it never replaces the durable store.

---

**Guiding Principle:** Build it for yourself first. A tool that solves your own problems reliably is the only honest foundation for eventually solving other people's problems. Phase 1 is complete when you no longer think about whether codero works — you just use it.

---

## Prior Art: What Already Exists

codero is the evolution of ghwatcher (ghwatcher.goels.in). The following work has been designed and partially implemented in Python. The Go implementation carries forward these validated architectural decisions. Nothing is reinvented — these patterns are ported.

**Validated designs from ghwatcher (carry forward to codero):**

- Event state machine: PENDING → CLAIMED → ACKED → RESOLVED, with ORPHANED and FAILED side paths. Canonical lease module owns all transitions.
- Module split: lease, events, relay, ghwrapper, server, app — single-responsibility separation.
- Pull-on-init delivery: agent polls GET /api/ghwrapper/next on session start. No push.
- Heartbeat-based session lifecycle: POST /api/ghwrapper/heartbeat every 30s, grace period triggers IDLE.
- ao reactions pattern port: ReactionConfig dataclass, dispatch loop, escalation timer with first_seen_at, retry/backoff with attempt_count. Ported from Composio agent-orchestrator.
- Disk-based state persistence: events/<event_id>.json, outbox/<alias>/next.json, claimed/<alias>/<event_id>.json.
- Codex delivery prompts: 5-task decomposition (lease.py, relay.py, ghwrapper.py, PR lifecycle, decouple refresh from notify).

**What the Go rewrite replaces:**

- Python's single-threaded event loop → Go goroutines for concurrent dispatch, lease monitoring, stale checking.
- File-based JSON state → SQLite (WAL mode) as durable store + Redis for ephemeral coordination.
- Direct TTY push (notify_agent via /dev/pts/*) → inbox.log append-only delivery with Redis-atomic seq numbers.
- Manual webhook/polling → Structured webhook receiver with Redis-backed dedup.

**What does NOT change:**

- API contracts (endpoint shapes, error format {"error": str, "detail": str|null}).
- State machine transitions (exact same table).
- Reactions config shape (env vars: REACTION_<TYPE>=auto|notify|ignore).
- Dashboard data model (extends, does not replace).
- codero.goels.in continues as the web dashboard surface.

---

## Resolved Decisions

Every ⚠ marker from v3 is resolved here. These are committed decisions, not open questions.

**DECISION 1 — Stage 8 / Stage 5.5 sequencing conflict.**
Resolution: Pull LiteLLM client wrapper and precommit-review alias into Stage 5.5 as tasks P1-S5.5-07 and P1-S5.5-08. The remaining 5 aliases (review-judge, feedback-summarizer, operator-chat, anomaly-detector, summary-fast) stay in Stage 8. Stage 8 is not resequenced.

**DECISION 2 — BullMQ / Go language mismatch.**
Resolution: Use Asynq (github.com/hibiken/asynq) instead of BullMQ for Phase 2. Asynq is Redis-native, idiomatic Go, and covers named queues, retries, timeouts, and crash recovery. River (PostgreSQL-backed) is not chosen — the architecture already separates coordination (Redis) from durability (Postgres). Deferral register updated.

**DECISION 3 — ElastiCache cluster mode risk.**
Resolution: Commit to non-cluster mode (single-node or replica set) as the Phase 2 baseline. Add a validation task (P2-S1-04) to confirm keyspace notification delivery end-to-end in the managed environment before cutover. If cluster mode is later required for capacity, the lease expiry mechanism needs architectural redesign — add a revisit trigger at 50 concurrent tenants.

**DECISION 4 — CodeRabbit CLI cost assumption.**
Resolution: The ~$0.25/file figure is unverified. Add a config cap on daily pre-commit reviews (default: 50/day) until actual cost per invocation is confirmed from a real invoice in the first week of Phase 1. Task P1-S7-07 tracks this validation. Cap is a hard limit enforced by Redis INCR with TTL.

**DECISION 5 — Pre-commit hook enforcement gap.**
Resolution: Add task P1-S5.5-06: install .git/hooks/pre-commit in each agent worktree that calls codero-cli commit-gate and aborts on non-zero exit. Since there is no scheduler wrapper, the agent operator is responsible for setting up worktrees. codero-cli init-worktree <path> installs the hook automatically. Document in setup guide.

---

## Technical Corrections (from v3 review)

These fixes are incorporated into the relevant stage tasks below.

**FIX 1 — Keyspace notifications are best-effort.** Added a periodic lease audit goroutine (every 30s) that scans active leases via SCAN and compares TTLs against SQLite state. Keyspace notifications become a latency optimisation, not a correctness dependency. Task P1-S3-07.

**FIX 2 — ZPOPMAX race under concurrent dispatch.** Phase 1 uses a single-goroutine dispatcher. ZPOPMAX is safe without Lua wrapping in this model. Document that Phase 2 dispatch parallelism requires a Lua-wrapped pop-verify-lease sequence. Task P1-S3-03.

**FIX 3 — Seq number gap on crash.** Seq numbers are monotonic but not necessarily contiguous. A crash between Redis INCR and O_APPEND write produces a harmless gap. Agents polling --since=N skip missing seqs correctly. Documented in task P1-S4-03.

**FIX 4 — Circuit breaker Redis-down fallback.** If Redis is unreachable during a pre-commit review, treat circuit breaker as closed (allow LiteLLM calls) and let the 5-second timeout handle failures naturally. A missing breaker state must not block the agent. Task P1-S5.5-08.

**FIX 5 — WFQ score TOCTOU.** Scores are recomputed on each dispatch tick, but active_jobs changes between ZADD and ZPOPMAX. For Phase 1 with single-goroutine dispatch this is negligible. Documented as a known imprecision that Phase 2 must address if dispatch is parallelised. Task P1-S3-03.

**FIX 6 — git diff HEAD misses untracked files.** Working tree diff handling uses git add -N (intent to add) before git diff HEAD to include new files. Task P1-S5.5-05.

---

## Job Triggering Model

There is no scheduler wrapper. All agent work is triggered manually by the operator or via external orchestration (cron, launchd, CI, or any tool the operator chooses). codero does not own agent lifecycle.

codero's responsibility boundary: receive submissions, queue them, dispatch reviews, deliver findings, track state. The operator or external orchestration is responsible for: starting agent sessions, creating worktrees, triggering pre-commit reviews, and re-triggering agents on findings.

The full automation pipeline is: operator (or cron) starts agent → agent works → agent calls codero-cli precommit-review --wait → fixes if needed → agent calls codero-cli commit-gate → commits → agent calls codero-cli submit → codero queues and reviews → findings delivered to inbox.log → operator (or cron) re-triggers agent with codero-cli poll --since=N → agent fixes → cycle repeats.

---

## Canonical State Machine

This is the single source of truth for all states and transitions. Every state is listed. Every valid transition is listed. Invalid transitions are rejected with InvalidTransition(from_state, to_state).

| # | FROM STATE | TO STATE | TRIGGER | NOTES |
|---|-----------|----------|---------|-------|
| T01 | (new) | coding | codero-cli register or first submit | Branch enters the system |
| T02 | coding | local_review | Agent signals working tree changes ready | Pre-commit loops begin |
| T03 | local_review | coding | Either pre-commit loop fails | Agent must fix and re-submit |
| T04 | local_review | queued_cli | Both pre-commit loops pass | Agent may commit; branch enters queue |
| T05 | coding | queued_cli | codero-cli submit (direct path, no pre-commit) | Skip pre-commit when loops not configured |
| T06 | queued_cli | cli_reviewing | Lease issued, CodeRabbit CLI invoked | Dispatch picks branch from WFQ |
| T07 | cli_reviewing | queued_cli | Lease expires (review hung) | retry_count incremented |
| T08 | cli_reviewing | reviewed | Review completes, findings delivered | Findings in inbox.log |
| T09 | reviewed | coding | Agent picks up findings | Re-enters development cycle |
| T10 | reviewed | merge_ready | approved=true AND ci_green=true AND pending_events=0 AND no unresolved threads | Recomputed on every watch tick |
| T11 | merge_ready | coding | New findings arrive or approval revoked | Reverts to active development |
| T12 | any active | stale_branch | HEAD hash mismatch on dispatch | Force-push detected |
| T13 | stale_branch | queued_cli | Agent re-submits with new HEAD | retry_count resets to 0 |
| T14 | any active | abandoned | Heartbeat TTL expires (1800s) | Operator must reactivate |
| T15 | abandoned | queued_cli | codero-cli reactivate | retry_count resets |
| T16 | any active | blocked | retry_count >= max_retries | Operator intervention required |
| T17 | blocked | queued_cli | Operator releases via TUI/CLI | retry_count resets |
| T18 | any | closed | PR merged/closed or operator closes | Terminal state |
| T19 | queued_cli | paused | Operator pauses branch | Blocks dispatch, does not cancel active lease |
| T20 | paused | queued_cli | Operator resumes branch | Re-enters queue |

**"any active" means:** coding, local_review, queued_cli, cli_reviewing, reviewed, merge_ready.

**Terminal states:** closed. **Operator-only transitions:** T15, T17, T18 (close), T19, T20.

---

## Redis Coordination Map

Redis is the ephemeral coordination layer in both phases. It never replaces SQLite (Phase 1) or PostgreSQL (Phase 2) as the durable source of truth. Every Redis value can be lost and rebuilt from the durable store without data loss.

| CONCERN | REDIS PRIMITIVE | WHAT IT REPLACES |
|---------|----------------|------------------|
| Lease acquire/release | SET NX + TTL | Background goroutine polling lease_expires_at in SQLite |
| WFQ dispatch queue | Sorted Set (ZADD / ZPOPMAX) | SELECT WHERE state=queued ORDER BY score on every tick |
| Concurrent slot counter | INCR / DECR via Lua script | In-process mutex counter — lost on SIGKILL |
| Heartbeat TTL | SET EX 1800 + keyspace notification | Background goroutine polling owner_session_last_seen |
| inbox.log seq number | INCR seq:<repo> | File-scan on startup to find max seq — fragile under concurrency |
| Webhook dedup hot path | SET NX EX 86400 on X-GitHub-Delivery | Disk write to processed_events inside 10-second GitHub window |
| Stale check trigger | Keyspace notification on lease expiry + 30s audit fallback | 60-second polling tick — now event-driven with safety net |
| LiteLLM circuit breaker | Hash: cb:state:<alias> | In-process state — lost on restart |
| Pre-commit rate limiting | Sorted Set + Lua (CodeRabbit slots) | Uncoordinated CLI invocations hitting provider rate limits |
| Daily pre-commit cap | INCR precommit:daily:<date> EX 86400 | No cap existed — cost risk |
| Phase 2: job queue | Asynq (Redis-native Go) | Ambiguity resolved — Asynq replaces BullMQ |
| Phase 2: rate limiting | Sliding window (ZADD + ZREMRANGEBYSCORE) | No rate limiting existed |
| Phase 2: live dashboard | Pub/sub on state transition events | Polling /queue endpoint — now push-based |

---

## Phase 1 — Personal Project

Local-first. Single user. Prove the core.

Phase 1 is the complete, fully functional system for personal use. It runs locally, manages a single developer's review workflow across two active projects, and proves every core architectural idea in daily practice. Nothing in Phase 1 is a prototype — it is production-quality code that you ship to yourself.

Success is using codero every day for six months and finding that it is boring — reviews arrive, stale branches get caught, the queue is fair, agents commit clean code without hitting rate limits. That boredom is the gate to Phase 2.

**Agent workflow (no scheduler wrapper — manual or external orchestration):** The operator (or a cron job / external orchestrator) starts a Claude Code agent against one of the two active projects. The agent works in an isolated git worktree. Before committing, the agent must pass two sequential local review loops managed by codero (Loop 1: LiteLLM review of working tree diff; Loop 2: CodeRabbit CLI review of working tree diff, rate-limited by codero). Only after both loops pass does the agent commit and call codero-cli submit. codero then queues the branch, runs the full CodeRabbit PR review, and routes findings back as structured input. The operator re-triggers the agent with findings from codero-cli poll. The agent fixes, pushes, re-submits. codero cross-checks GitHub to confirm all comment threads are resolved before advancing to merge_ready. On merge_ready, codero sets the GitHub status check to passing and auto-merge completes the cycle. No human in the loop unless a branch stalls.

### Phase 1 Data Layer

| LAYER | ROLE | OWNS |
|-------|------|------|
| SQLite (WAL mode) | Durable source of truth | Branch state records, feedback bundles, transition audit log, migration history, agent effectiveness records. Every Redis value can be reconstructed from SQLite on startup. |
| Redis (local) | Ephemeral coordination | Lease TTLs, WFQ sorted set, slot counter, heartbeat keys, seq counter, webhook dedup keys, LiteLLM circuit breaker state, pre-commit CodeRabbit rate-limit slots, daily pre-commit cap counter. Lost on Redis restart — rebuilt from SQLite automatically. |
| inbox.log (file) | Append-only delivery | Agent-facing feedback delivery. Seq numbers sourced from Redis INCR for atomicity. File itself never changes a written line. Seq numbers are monotonic but not necessarily contiguous (crash between INCR and O_APPEND produces a harmless gap). |

---

### Stage 0.5 — Core Daemon

Before anything else, codero needs to exist as a proper long-running background service. Redis is started as a dependency alongside the daemon — codero will not start if Redis is unreachable.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S0.5-01 | Single binary with subcommands | codero daemon starts the long-running process. codero-cli is the client. Both live in the same Go binary — one install, no separate processes to manage. | — |
| P1-S0.5-02 | Background service integration | systemd unit file (Linux) and launchd plist (macOS). The service manifest starts both the codero daemon and a local redis-server as a dependency. Redis starts first. | P1-S0.5-01 |
| P1-S0.5-03 | Redis startup health check | On daemon start: PING to configured Redis address. If unreachable after 3 retries with 1-second backoff, codero exits with REDIS_UNAVAILABLE. Lease and queue primitives are not optional — do not start degraded. | P1-S0.5-01 |
| P1-S0.5-04 | Redis reconnect on transient failure | If Redis becomes unreachable after a successful start: no new dispatches, in-flight leases run to completion, operator notified via TUI and system bundle. Automatic reconnect with exponential backoff. Dispatch resumes when Redis is healthy. | P1-S0.5-03 |
| P1-S0.5-05 | SIGTERM handling | Stop accepting new submissions. Allow in-flight leases and pre-commit reviews to complete (configurable grace period). Flush internal log. Exit cleanly. | P1-S0.5-01 |
| P1-S0.5-06 | SIGKILL recovery | Cannot be caught — Stage 1 crash recovery handles the aftermath. On restart: Redis is checked first, then stale lease keys are audited against SQLite for consistency. | P1-S0.5-03, P1-S1-05 |
| P1-S0.5-07 | PID file | Write on startup. codero-cli uses it to detect whether the daemon is running, surfaces a clear error if not — never hangs silently. | P1-S0.5-01 |
| P1-S0.5-08 | codero status command | Reads PID file, /health endpoint, and performs a Redis PING. Reports daemon state, uptime, Redis connectivity, and any degraded components. | P1-S0.5-07 |

**Exit Gate:** Daemon starts cleanly via systemd/launchd with Redis as a dependency. SIGTERM triggers graceful shutdown with in-flight work completing. SIGKILL followed by restart produces correct crash recovery. codero-cli status correctly reports running/stopped state and Redis connectivity.

---

### Stage 1 — Foundation

The state machine, SQLite store, Redis coordination layer, and crash recovery form the foundation that every other stage depends on. Do not move to Stage 2 until this stage's exit gate is met.

**Language: Go.** Tiny binaries with no runtime dependencies. Native SQLite + WAL support. go-redis client for Redis. Lightweight background goroutines for lease monitor and stale checker. Prometheus client and HTTP server built in. Bubble Tea for the three-pane TUI. codero-cli and daemon ship as a single binary. Lock this on Day 1 — it does not change.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S1-01 | Repo structure and module layout | cmd/codero (daemon + CLI) and internal packages per domain: state, scheduler, delivery, webhook, tui, precommit. Add internal/redis package wrapping go-redis with all key-naming conventions and Lua scripts centralised — never scatter raw Redis commands across packages. | P1-S0.5-01 |
| P1-S1-02 | Config file loader | Explicit repo list. Required GitHub token scopes validated on startup: repo (read), checks:write, admin:repo_hook. Redis address and optional password configurable. Unknown config fields are errors, not warnings. | P1-S1-01 |
| P1-S1-03 | SQLite state store with WAL mode | Schema covers all state machine fields from the Canonical State Machine table (all 10 states, all 20 transitions). golang-migrate embedded in the binary — every schema change is a numbered migration file run automatically on startup. Exit with a clear error if migration fails. | P1-S1-01 |
| P1-S1-04 | Redis client initialisation | go-redis client at startup. All key names use consistent namespace: codero:<repo>:<type>:<id>. Lua scripts for atomic operations (slot acquire, seq increment, pre-commit slot acquire, daily cap check) compiled at startup and cached as script SHA. Connection pool sized to max goroutine concurrency. | P1-S1-01 |
| P1-S1-05 | State machine implementation | All 10 states and all 20 valid transitions from the Canonical State Machine table implemented as an explicit transition table. Includes local_review state with all transitions (T02, T03, T04) fully specified even though loop machinery lands in Stage 5.5. Invalid transitions are rejected and logged with the attempted transition — never silently ignored. | P1-S1-03 |
| P1-S1-06 | Remove write serialisation channel | With WFQ queue and lease operations moved to Redis, write contention on SQLite shrinks to state record updates only. A single write-connection pool with 5-second busy_timeout is sufficient. Remove the serialisation channel from the design. | P1-S1-03, P1-S1-04 |
| P1-S1-07 | Startup crash recovery | On boot: (1) verify Redis connectivity, (2) scan SQLite for branches in cli_reviewing, (3) check each against Redis for a live lease key — absent key + cli_reviewing state → transition to queued_cli + increment retry_count, (4) revalidate all queued branch hashes against current HEAD. | P1-S1-03, P1-S1-04, P1-S1-05 |
| P1-S1-08 | Structured internal log | Every state transition, rejection, and system event written to JSON lines. Primary debug surface throughout Phase 1. | P1-S1-05 |
| P1-S1-09 | Basic test harness | Unit tests for state machine transitions (all 20), WFQ scorer, and Redis key operations using miniredis (in-process, no external Redis required in CI). End-to-end tests with mocked CodeRabbit CLI. GitHub Actions CI runs the full suite on every push. | P1-S1-05, P1-S1-04 |

**Exit Gate:** State machine accepts and rejects all 20 transitions correctly including local_review state. SQLite persists across restarts. Redis connectivity verified on startup with a named error on failure. Crash recovery correctly distinguishes live leases (Redis key present) from expired leases (Redis key absent). Concurrent writes produce zero SQLITE_BUSY errors. CI passes including Redis-dependent tests via miniredis.

---

### Stage 1.5 — Automated Code Review Integration (Free Tooling)

Before the CLI submission client is built, the GitHub CI pipeline should enforce code quality automatically on every pull request. All tools in this stage are free or free-tier. This stage runs in parallel with Stage 1 development and is complete before Stage 2 begins.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S1.5-01 | ESLint + Prettier (GitHub Action) | Free, unlimited. GitHub Actions workflow that runs ESLint and Prettier on every push and PR. Fails the check if lint errors or formatting violations found. Committed in the same PR as initial repo structure (P1-S1-01). | P1-S1-01 |
| P1-S1.5-02 | Danger JS | Fully free and open source. Runs as a GitHub Action. Write custom rules: flag missing TODO owner tags, PRs that modify state machine transitions without updating tests, commits that touch inbox.log writer without a bundle idempotency test. Uses built-in GITHUB_TOKEN secret. | P1-S1-01 |
| P1-S1.5-03 | CodeRabbit (free tier) | Free tier: unlimited repos, rate-limited to 200 files/hour and 4 PR reviews/hour. Install via GitHub Marketplace. This is the GitHub PR review integration, separate from the CodeRabbit CLI used in pre-commit loops (Stage 5.5). The two operate independently. | P1-S1-01 |
| P1-S1.5-04 | Qodo (free tier) — optional | Free for individual developers: 75 PRs and 250 LLM credits per month. Consider as complement to CodeRabbit if the 4 PR/hour rate limit becomes a constraint during active development sprints. | P1-S1-01 |

**Exit Gate:** ESLint + Prettier GitHub Action runs on every push and fails on violations. Danger JS posts at least one rule-violation comment on a test PR. CodeRabbit GitHub integration posts an AI review on the first real PR. All three layers operate with no paid accounts.

---

### Stage 2 — CLI Submission Client

The interface through which agents and you interact with codero daily. Every command is something used in your own workflow.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S2-01 | codero-cli submit | Strict JSON schema validation. Captures branch, HEAD hash, session ID. On successful submission, branch added to Redis WFQ sorted set via ZADD. Rejects malformed submissions with clear error. | P1-S1-05, P1-S1-04 |
| P1-S2-02 | --dry-run flag | Validates and scores without enqueuing. Computes WFQ score from current Redis sorted set. No writes to SQLite or Redis. | P1-S2-01 |
| P1-S2-03 | codero-cli heartbeat | Resets heartbeat:<sessionID> TTL via SET EX 1800 in Redis. Does not write to SQLite on every call — owner_session_last_seen in SQLite updated only on meaningful events. | P1-S1-04 |
| P1-S2-04 | codero-cli poll [--since=N] | Reads inbox.log from sequence number N. --follow uses inotify/kqueue to watch the file — event-driven, no busy loop. | P1-S1-01 |
| P1-S2-05 | codero-cli why <branch> | Full score breakdown: queue_priority, wait contribution, retry penalty, concurrency penalty, estimated wait, blocking reason. Reads live score from Redis sorted set. | P1-S2-01 |
| P1-S2-06 | codero-cli register | Optional explicit branch registration into coding state. If omitted, submit is the entry point and the branch enters at local_review (pre-commit loops pending) or queued_cli (submit-only path). Both paths documented. | P1-S1-05 |
| P1-S2-07 | codero-cli reactivate | Moves branch from abandoned to queued_cli. Resets retry_count. Re-adds to Redis sorted set. | P1-S1-05 |
| P1-S2-08 | codero-cli init-worktree <path> | Creates the agent worktree and installs .git/hooks/pre-commit that calls codero-cli commit-gate. This is the setup command operators run before handing a worktree to an agent. | P1-S1-01 |

**Exit Gate:** Full round-trip works in terminal. submit enqueues in Redis sorted set. poll returns findings via inotify/kqueue. dry-run returns score with no Redis writes. why produces accurate score from live Redis queue. reactivate correctly restores an abandoned branch. init-worktree installs a functioning pre-commit hook.

---

### Stage 3 — Scheduler and Lease Model

The scheduler prevents review collisions across multiple simultaneous branches. The lease model makes the system safe when a review hangs. Both are fundamentally simpler with Redis atomic primitives.

**Redis Layer:** Stage 3 is the primary Redis beneficiary in Phase 1. The WFQ queue replaces a polling SELECT with an O(log N) sorted set. The lease model replaces a background polling goroutine with native key TTL and keyspace notifications (with audit fallback). The slot counter becomes an atomic Lua script rather than a mutex. All three changes remove entire classes of failure.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S3-01 | WFQ scorer | Score = queue_priority + (minutes_waiting × wait_factor) − (retry_count × retry_penalty) − (active_jobs × concurrency_penalty). Computed at dispatch time. Scores recomputed and sorted set updated on every dispatch tick — ZADD uses XX flag to update existing members. On Redis loss recovery: recompute all WFQ scores from submission_time in SQLite before re-populating sorted set via ZADD. | P1-S1-04, P1-S1-03 |
| P1-S3-02 | Priority ceiling | queue_priority capped at 20 — enforced at submit time before ZADD. Values above 20 rejected with clear error. | P1-S3-01 |
| P1-S3-03 | Dispatch loop (single-goroutine) | Single goroutine. ZPOPMAX to atomically dequeue highest-scored branch. No SELECT on SQLite. Verifies HEAD hash in SQLite before issuing lease. HEAD mismatch → stale_branch, key discarded. Known imprecision: WFQ score TOCTOU between ZADD and ZPOPMAX is negligible with single-goroutine dispatch. Document that Phase 2 parallel dispatch requires Lua-wrapped pop-verify-lease. | P1-S3-01 |
| P1-S3-04 | Lease issuance | SET lease:<branchID> <payload> EX <timeout_sec> NX — atomic. NX failure (lease exists) → log and skip, do not double-lease. Payload contains branch, commit, leased_at for crash recovery. | P1-S3-03 |
| P1-S3-05 | Lease expiry (event-driven + audit fallback) | Enable Redis keyspace notifications (notify-keyspace-events Ex). Subscribe to __keyevent@0__:expired. On expiry of lease:<branchID>: SIGTERM to CLI process, retry_count increments in SQLite, branch returns to queued_cli and re-added to sorted set, system bundle appended. | P1-S3-04 |
| P1-S3-06 | Slot counter | Lua atomic script: if slots:active >= max_slots then return 0 else INCR return 1 end. DECR on release. Survives SIGKILL unlike an in-process mutex. | P1-S1-04 |
| P1-S3-07 | Lease audit goroutine (safety net) | Runs every 30 seconds. SCANs all lease:* keys in Redis, compares TTLs against SQLite state. Detects: (a) SQLite says cli_reviewing but no Redis lease key → transition to queued_cli, (b) Redis lease exists but SQLite says different state → log inconsistency. This is the correctness backstop — keyspace notifications are the fast path but are best-effort. | P1-S3-04, P1-S1-03 |
| P1-S3-08 | Global retry exhaustion | After each ZPOPMAX: fire queue_stalled when ALL members of the sorted set have retry_count >= max_retries — not only when the sorted set is empty after a single exhausted pop. When queue_stalled fires: dispatch halts, event fires, operator must intervene. | P1-S3-03 |
| P1-S3-09 | Configurable WFQ coefficients | All four factors readable from config. Hot-reload via SIGHUP — coefficient changes take effect at next dispatch cycle without restart. Sorted set scores are not retroactively updated on coefficient change — document this behaviour. | P1-S3-01 |

**Exit Gate:** Branches dispatch in correct WFQ priority order verified against Redis sorted set contents. Lease expiry fires via keyspace notification AND is confirmed by audit goroutine within 30 seconds. Slot counter is atomic and accurate after simulated SIGKILL mid-review. queue_stalled fires when all branches are blocked (not just when set is empty). Coefficients change via SIGHUP without restart.

---

### Stage 4 — CodeRabbit CLI Integration and Feedback Delivery

Full PR reviews run. Findings land in inbox. Stale branches are caught before wasting a CLI slot.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S4-01 | CodeRabbit CLI runner | Always invoke with --prompt-only flag. Token-dense structured output (file, line, defect) designed for programmatic consumption. Interactive modes break the normaliser. | P1-S3-04 |
| P1-S4-02 | Finding normaliser | Parses --prompt-only output into the feedback bundle schema. Classifies severity. Every required field populated — no partial bundles reach inbox.log. | P1-S4-01 |
| P1-S4-03 | inbox.log seq number (Redis-atomic) | Before appending a bundle, issue INCR seq:<repo> to obtain next sequence number atomically. Initialised from max seq in SQLite on first Redis startup. Never resets — even if Redis restarts, init reads the floor from SQLite. Document: seq numbers are monotonic but not necessarily contiguous (crash between INCR and O_APPEND produces harmless gap). | P1-S1-04, P1-S1-03 |
| P1-S4-04 | inbox.log writer | Atomic append using O_APPEND. One JSON object per line. Seq number sourced from Redis INCR — monotonically correct under concurrent writers. | P1-S4-03 |
| P1-S4-05 | Bundle idempotency | bundle_id = hash(source + pr_number + file + line + comment_id). Checked against SQLite feedback_bundles table before appending. Duplicate bundle_ids silently dropped and logged. | P1-S4-04, P1-S1-03 |
| P1-S4-06 | Background stale checker (event-driven) | Lease key expiry keyspace notification immediately triggers HEAD hash check for the affected branch. The lease audit goroutine (P1-S3-07) is the fallback for queued_cli branches that never acquired a lease. | P1-S3-05, P1-S3-07 |
| P1-S4-07 | Force-push retry reset | When an agent re-submits after stale_branch, retry_count resets to 0 in SQLite and the branch is re-added to Redis sorted set with a fresh score. | P1-S1-05 |
| P1-S4-08 | LiteLLM circuit breaker | All LiteLLM calls wrapped in a circuit breaker with 5-second timeout. Circuit breaker state (open/closed/half-open) stored in Redis — survives process restarts. If LiteLLM is unreachable, review-judge falls back to heuristic severity classifier with no pipeline stall. | P1-S1-04 |
| P1-S4-09 | System bundle format | All system-generated messages use source: system with a message field. Same inbox.log format, distinguishable by source field. | P1-S4-04 |

**Exit Gate:** Full pipeline runs end-to-end. Findings appear in inbox.log with correct seq numbers sourced from Redis INCR. Seq counter survives Redis restart by initialising from SQLite floor. Stale detection fires immediately on lease expiry via keyspace notification AND confirmed by audit goroutine. LiteLLM circuit breaker trips and falls back without blocking delivery.

---

### Stage 5 — GitHub Webhook Ingestion and Reconciliation

PR feedback from GitHub arrives reliably. Missed webhooks are caught. Duplicates never reach you.

**Phase 1 Webhook Decision:** Polling-only mode is the Phase 1 default. webhook_mode: polling in config. Reconciliation runs every 60 seconds and is the primary delivery path. Webhook mode (via cloudflared named tunnel with fixed domain) is opt-in. This avoids the ngrok URL rotation problem and silent webhook deregistration on daemon restart. If webhook mode is enabled, the daemon re-registers the tunnel URL via GitHub API on every start.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S5-01 | Webhook receiver | Accepts pull_request_review, pull_request_review_comment, issue_comment, check_run. Must respond 2XX within 10 seconds — GitHub terminates and retries if missed. Only started when webhook_mode: tunnel. | P1-S1-01 |
| P1-S5-02 | X-GitHub-Delivery dedup (Redis) | SET dedup:<X-GitHub-Delivery> 1 EX 86400 NX. If NX returns 0 (key exists), drop immediately — no JSON parsing, no database read. Under 1ms. Keeps SQLite out of the 10-second response window. | P1-S1-04 |
| P1-S5-03 | Event normaliser | Maps all four event types to the feedback bundle schema. Output identical regardless of which event type triggered the bundle. | P1-S5-01, P1-S4-02 |
| P1-S5-04 | Bundle idempotency (secondary) | bundle_id hash check against SQLite as second layer after Redis dedup. Catches content-level duplicates arriving via different delivery IDs (reconciliation vs webhook). | P1-S4-05 |
| P1-S5-05 | Branch-to-session router | Looks up owner_session from SQLite by branch name. Appends bundle to the correct inbox.log entry. | P1-S1-03, P1-S4-04 |
| P1-S5-06 | Reconciliation loop | Background poll of GitHub API every 60 seconds (polling-only mode, primary path) or every 5 minutes (webhook mode, catch-up). Detects PRs whose state in codero differs from GitHub's actual state. PR closed outside codero → state transitions to closed, system bundle appended. Document rate budget: each poll costs ~N API calls (list PRs + list reviews + list check runs per open PR). At 60-second intervals for 2 repos, well within 5000/hour authenticated limit. | P1-S1-03, P1-S4-09 |
| P1-S5-07 | Heartbeat TTL and abandoned state | heartbeat:<sessionID> expires after 1800 seconds. Keyspace notification on expiry triggers abandoned state transition in SQLite and system bundle. Agent reconnects via heartbeat to reset TTL. Operator must manually re-activate via codero-cli reactivate. | P1-S2-03, P1-S1-05 |
| P1-S5-08 | Polling-only fallback mode | Config flag: webhook_mode: tunnel | polling. In polling mode, webhook receiver not started. Reconciliation runs every 60 seconds. Phase 1 fully functional without a tunnel. This is the default. | P1-S5-06 |
| P1-S5-09 | Tunnel URL auto-registration (opt-in) | When webhook_mode: tunnel, on daemon start: register/update the cloudflared tunnel URL via GitHub API. Requires admin:repo_hook scope. Eliminates silent deregistration on restart. | P1-S5-01 |

**Exit Gate:** Webhook events appear in inbox.log (when tunnel active). X-GitHub-Delivery Redis check fires in under 1ms. Duplicate events at both header and content level dropped. Heartbeat expiry transitions correctly to abandoned. Abandoned re-activation works via CLI and TUI. Polling-only fallback mode confirmed functional without a tunnel.

---

### Stage 5.5 — Pre-commit Local Review Loops

Before any agent can commit to its branch, it must pass two sequential local review loops. These are not optional gates — they are enforced by codero via .git/hooks/pre-commit installed by codero-cli init-worktree.

**Why This Stage Exists:** Without pre-commit gates, the CodeRabbit PR review becomes the first quality check — wasted review slots on commits that should never have been submitted. The LiteLLM loop catches obvious issues cheaply. The CodeRabbit loop catches deeper structural issues. Both must pass. Order is fixed: LiteLLM first, CodeRabbit second, always sequential.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S5.5-01 | local_review state activation | Branch enters local_review when the agent signals working tree changes ready for pre-commit review. Branch cannot transition to queued_cli until both loops pass. Transition: local_review → queued_cli on dual pass; local_review → coding on either loop failure. State machine already has these transitions (T02, T03, T04) from Stage 1 — this task activates the machinery. | P1-S1-05 |
| P1-S5.5-02 | Loop 1: LiteLLM review | Lightweight first pass. LiteLLM reviews the working tree diff directly (no commit required). Findings returned as structured feedback to the agent inline. Agent fixes and re-runs until it passes. Does not consume a CodeRabbit rate-limit slot. | P1-S5.5-01, P1-S5.5-07 |
| P1-S5.5-03 | Loop 2: CodeRabbit pre-commit review | Runs after Loop 1 passes. CodeRabbit CLI reviews the working tree diff in --prompt-only mode. Consumes a CodeRabbit API slot — subject to rate limiting. codero manages slot availability before invoking. If slot not available, agent waits in local_review with estimated wait time. | P1-S5.5-02, P1-S5.5-04 |
| P1-S5.5-04 | CodeRabbit pre-commit rate limiter | Redis sorted set of pre-commit review requests with WFQ logic. Lua atomic script acquires a pre-commit slot before each CodeRabbit invocation and releases immediately after. Max concurrent pre-commit reviews configurable and separate from PR review slot limit. Daily cap: INCR precommit:daily:<date> EX 86400, reject if >= configured cap (default 50). Cap is a hard limit until actual CodeRabbit cost per invocation is validated from a real invoice. | P1-S1-04 |
| P1-S5.5-05 | Working tree diff handling | CodeRabbit CLI receives a diff generated from: git add -N (intent to add for untracked files) then git diff HEAD (staged + unstaged + newly tracked). No temporary commit required. If diff is empty, both loops considered passed — codero logs as no-op review. | P1-S5.5-03 |
| P1-S5.5-06 | Pre-commit hook installation | codero-cli init-worktree <path> installs .git/hooks/pre-commit that calls codero-cli commit-gate and aborts on non-zero exit. This is the enforcement mechanism — the agent cannot bypass loops by committing directly. Operator runs init-worktree when setting up each agent worktree. | P1-S2-08 |
| P1-S5.5-07 | LiteLLM client wrapper (minimal) | Single internal LiteLLM client. Strict JSON schema mode. Circuit breaker state in Redis (reuses P1-S4-08 infrastructure). Malformed responses discarded and logged. If Redis unreachable during pre-commit review: treat circuit breaker as closed (allow calls), let 5-second timeout handle failures — missing breaker state must not block agent. | P1-S4-08 |
| P1-S5.5-08 | precommit-review alias | Powers Loop 1. Receives working tree diff. Returns structured findings (file, line, severity, message) in JSON. Called synchronously within codero-cli precommit-review --wait. Must complete within 30 seconds or loop marked timed-out and agent notified. | P1-S5.5-07 |
| P1-S5.5-09 | Pre-commit findings delivery | Loop findings returned synchronously to agent via codero-cli precommit-review --wait. Structured findings (file, line, severity, message). inbox.log is reserved for PR-level feedback only. | P1-S5.5-02, P1-S5.5-03 |
| P1-S5.5-10 | Commit gate enforcement | codero-cli commit-gate <branch> returns exit code 0 (both loops passed, safe to commit) or non-zero (loops pending or failed). Called by .git/hooks/pre-commit installed in P1-S5.5-06. | P1-S5.5-01 |
| P1-S5.5-11 | Sequential arrangement documentation | Document in setup guide: two loops are permanently sequential (LiteLLM first, CodeRabbit second). Not parallel. CodeRabbit slots are expensive — LiteLLM first filters commits that would waste a slot. | P1-S5.5-02, P1-S5.5-03 |

**Exit Gate:** Branch in local_review cannot transition to queued_cli until both loops pass. Loop 1 runs and returns findings synchronously within 30 seconds. Loop 2 waits for a rate-limit slot before invoking. Pre-commit rate limiter correctly queues concurrent requests from both projects without hitting CodeRabbit API rate limit. Daily cap enforced. commit-gate returns correct exit codes. Pre-commit hook blocks commit on non-zero exit.

---

### Stage 5.6 — Agent Effectiveness Metrics

Pre-commit loops generate rich signal about agent code quality. This stage captures, persists, and surfaces it.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S5.6-01 | Per-agent effectiveness record | SQLite table: agent_id, project, timestamp, loop1_pass (bool), loop1_findings_count, loop2_pass (bool), loop2_findings_count, loop2_slot_wait_seconds, commit_submitted (bool). One row per pre-commit review attempt. agent_id is the session ID. | P1-S5.5-02, P1-S5.5-03, P1-S1-03 |
| P1-S5.6-02 | First-pass rate metric | Percentage of pre-commit attempts where both loops pass on first invocation. Per agent and per project. Below 50% is a signal — surface prominently. | P1-S5.6-01 |
| P1-S5.6-03 | Findings per commit metric | Average Loop 1 and Loop 2 findings per submitted commit, tracked separately. Loop 2 findings per commit is the more important signal — measures what slipped past LiteLLM filter. | P1-S5.6-01 |
| P1-S5.6-04 | Retry rate per agent/project | Average pre-commit review cycles before clean pass. Per agent and per project. Rising retry rates indicate regression. | P1-S5.6-01 |
| P1-S5.6-05 | Slot wait time tracking | How long agent waits for a CodeRabbit pre-commit slot. Tracked as histogram. High p95 indicates slot limit needs increasing. | P1-S5.6-01 |
| P1-S5.6-06 | TUI effectiveness pane | Fourth pane (or tab within Branch Detail): per-agent and per-project effectiveness metrics for last 7 and 30 days. First-pass rate, avg findings per commit, avg retry rate, avg slot wait. Sortable. | P1-S5.6-01, P1-S6-01 |
| P1-S5.6-07 | codero dashboard API integration | Expose effectiveness metrics on /api/v1/agent-metrics endpoint (JSON). codero.goels.in polls this endpoint for the web dashboard. Schema: { agent_id, project, period_days, first_pass_rate, avg_loop1_findings, avg_loop2_findings, avg_retries, avg_slot_wait_ms, as_of }. | P1-S5.6-01 |
| P1-S5.6-08 | Metrics reset boundary | Effectiveness metrics retained in SQLite for the full Phase 1 period. The compact command explicitly skips the agent_effectiveness table. | P1-S5.6-01 |

**Exit Gate:** Pre-commit review attempts recorded in SQLite with all fields populated. TUI pane displays correct per-agent and per-project metrics. /api/v1/agent-metrics returns correct JSON. Metrics exclude no-op reviews (empty diffs). compact does not truncate the effectiveness table.

---

### Stage 6 — Operator TUI

The TUI is your control surface. All operator actions have precise, documented semantics. The advisory assistant is strictly read-only.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S6-01 | Three-pane layout + effectiveness tab | Queue pane (live scores, positions from Redis), Branch Detail pane (all tracked fields), Event Log pane (tail of structured log). Effectiveness tab within Branch Detail shows agent metrics from Stage 5.6. | P1-S3-01, P1-S1-08 |
| P1-S6-02 | reprioritize <branch> <0-20> | Sets queue_priority. Validated against ceiling. Takes effect at next dispatch cycle. Does not affect in-flight leases. | P1-S3-02 |
| P1-S6-03 | pause / resume <branch> | Pause blocks dispatch for this branch. Does not cancel active lease. System bundle appended on pause. | P1-S1-05, P1-S4-09 |
| P1-S6-04 | drain queue / resume | Halts new lease issuance. In-flight leases run to completion. New submissions accepted but not dispatched. Use for: patching host, rotating keys, planned downtime. | P1-S3-04 |
| P1-S6-05 | release <branch> | Moves branch from blocked to queued_cli. Resets retry_count. Re-adds to Redis sorted set. | P1-S1-05 |
| P1-S6-06 | reactivate <branch> | Moves branch from abandoned to queued_cli. Resets retry_count. Re-adds to Redis sorted set. Equivalent to codero-cli reactivate. | P1-S2-07 |
| P1-S6-07 | abandon / close <branch> | abandon → abandoned state, system bundle, queue slot freed. close → closed (terminal). Both operator-only, irreversible without re-submission. | P1-S1-05 |
| P1-S6-08 | replay <branch> --since=N | Re-appends all bundles for this branch from seq N to inbox.log. Does not re-run review. For recovering from a missed poll. | P1-S4-04 |
| P1-S6-09 | why <branch> | Full score breakdown from live Redis sorted set. Same data as codero-cli why. | P1-S2-05 |
| P1-S6-10 | release slot | Manually frees a CLI slot when the process is known dead but lease has not yet expired. Emergency use only. | P1-S3-06 |
| P1-S6-11 | Advisory assistant pane | operator-chat LiteLLM alias. All output prefixed [advisory]. Cannot execute any action — every suggested command requires explicit keypress to confirm. Can read event log, queue state, effectiveness metrics. No write access. Uses alias defined in Stage 8 (P1-S8-04). Stub until then. | P1-S6-01 |

**Exit Gate:** All operator actions work with correct semantics. reactivate restores abandoned branches in both Redis and SQLite. Effectiveness tab displays correct metrics. Advisory pane cannot execute actions. drain queue allows in-flight leases to complete.

---

### Stage 7 — Observability

If you cannot see what codero is doing, you cannot debug it. This stage makes the system inspectable from outside the process.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S7-01 | HTTP server on localhost:8080 | Minimal embedded server. Five endpoints. | P1-S0.5-01 |
| P1-S7-02 | /health | JSON: status, uptime, sqlite_ok, redis_ok (PING result), webhook_receiver_ok. Returns 200 if healthy, 503 if degraded. | P1-S7-01 |
| P1-S7-03 | /queue | JSON snapshot of full queue with current scores and positions from Redis sorted set. | P1-S7-01, P1-S3-01 |
| P1-S7-04 | /metrics | Prometheus text format. Original 9 metrics plus: redis_operation_latency_ms histogram, redis_reconnect_total counter, daily_cost_estimate gauge, precommit_slot_wait_seconds histogram, precommit_first_pass_rate gauge per project. | P1-S7-01 |
| P1-S7-05 | /api/v1/agent-metrics | JSON endpoint for codero dashboard consumption. Returns per-agent and per-project effectiveness metrics as defined in Stage 5.6. Read-only. | P1-S5.6-07 |
| P1-S7-06 | Redis operation latency histogram | Instrument all Redis operations with timing. Alert if p99 exceeds 10ms. | P1-S1-04 |
| P1-S7-07 | daily_cost_estimate metric + cost validation | Track estimated daily spend: CodeRabbit CLI invocations, LiteLLM calls. Gauge visible in /metrics and TUI. CRITICAL: validate the ~$0.25/file CodeRabbit cost assumption from a real invoice in the first week. Adjust daily pre-commit cap (P1-S5.5-04) if actual cost is 2-3x higher. | P1-S4-01, P1-S5.5-03 |
| P1-S7-08 | Metrics wired to state machine | Every state transition, lease event, Redis operation, and pre-commit review attempt increments or records the correct metric in real time. | P1-S1-05, P1-S7-04 |

**Exit Gate:** All five endpoints respond correctly. /health includes Redis PING result. /queue reads from Redis sorted set. Prometheus scrape includes redis_operation_latency_ms and precommit metrics. /api/v1/agent-metrics returns correct JSON. daily_cost_estimate visible in both /metrics and TUI. CodeRabbit cost assumption validated or cap adjusted.

---

### Stage 8 — LiteLLM Integration (Remaining Aliases)

The LiteLLM client wrapper and precommit-review alias were built in Stage 5.5. This stage completes the remaining aliases. Hard constraint: no LiteLLM output can trigger a state transition.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S8-01 | review-judge alias | Classifies ambiguous severity during normalisation of PR-level findings. Called asynchronously — does not block finding delivery. Falls back to heuristic classifier if circuit open. | P1-S5.5-07, P1-S4-08 |
| P1-S8-02 | feedback-summarizer alias | Summarises multi-comment PR bundles before inbox delivery. Optional enrichment, not a delivery dependency. | P1-S5.5-07 |
| P1-S8-03 | operator-chat alias | Powers TUI advisory assistant pane. Advisory only. Replaces the stub from P1-S6-11. | P1-S5.5-07, P1-S6-11 |
| P1-S8-04 | anomaly-detector alias | Background scan for unusual retry, stale, or effectiveness patterns. Alert only — cannot modify state. | P1-S5.5-07 |
| P1-S8-05 | summary-fast alias | On-demand branch status summary in TUI. | P1-S5.5-07 |
| P1-S8-06 | LiteLLM log database — disabled | Disable LiteLLM's internal PostgreSQL request logging. Tracing moves entirely to inbox.log and Prometheus. | P1-S5.5-07 |
| P1-S8-07 | Pydantic hot path audit | Verify no LiteLLM call site sits in a hot synchronous path — especially precommit-review (agent's commit-critical path). Cache schema validator instances. | P1-S5.5-08 |
| P1-S8-08 | Hard constraint audit | Review every LiteLLM call site. Confirm zero code paths where model output can directly trigger a state transition. Document in code with a comment. This audit is a deliverable, not a verbal assurance. | all P1-S8 aliases |

**Exit Gate:** All 5 remaining aliases work. Malformed model output does not block or crash pipeline. Circuit breaker state correctly stored in Redis. Hard constraint audit committed alongside code.

---

### Stage 9 — Hardening and Phase 1 Sign-off

Phase 1 is not done until the system has been put under the failure conditions it will actually encounter.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P1-S9-01 | Failure mode audit | Walk every failure mode. Each must produce documented state transition and system bundle. Include: Redis restart mid-lease, Redis reconnect during dispatch, Redis key eviction under memory pressure, pre-commit loop failures (CodeRabbit timeout, LiteLLM circuit open, slot exhaustion). | all prior stages |
| P1-S9-02 | End-to-end integration test | Operator triggers agent → working tree changes → Loop 1 (LiteLLM) → Loop 2 (CodeRabbit, rate-limited) → commit → codero-cli submit → queue → PR review → findings routed back → operator re-triggers agent with findings → fixes → re-submit → unresolved comment cross-check → merge_ready → auto-merge. Every state transition and inbox.log entry confirmed. | all prior stages |
| P1-S9-03 | Concurrent branch stress test | Multiple branches from both projects submitting simultaneously. Confirm WFQ ordering via Redis sorted set, no slot over-allocation (atomic Lua script), no pre-commit slot collisions, zero SQLITE_BUSY errors. | P1-S3-06, P1-S5.5-04 |
| P1-S9-04 | Redis failure and recovery test | Kill Redis mid-dispatch. Confirm: in-flight lease completes from process memory, daemon enters degraded mode, on Redis restart slot counter and sorted set rebuild correctly from SQLite, dispatch resumes automatically. | P1-S0.5-04, P1-S3-07 |
| P1-S9-05 | Crash recovery test | SIGKILL mid-lease. Restart. Confirm lease key absent in Redis, SQLite branch in cli_reviewing, crash recovery transitions to queued_cli correctly. | P1-S1-07 |
| P1-S9-06 | Webhook replay test | Send duplicate events (same X-GitHub-Delivery). Confirm Redis NX check drops in under 1ms with no SQLite write. Send out-of-order events. Confirm exactly-once processing. | P1-S5-02 |
| P1-S9-07 | Pre-commit rate limit stress test | Simulate both projects submitting pre-commit reviews simultaneously at maximum rate. Confirm no CodeRabbit API rate limit errors. Confirm agents receive accurate wait time estimates and queue positions. | P1-S5.5-04 |
| P1-S9-08 | Unresolved comment cross-check test | Open PR with multiple review comment threads. Resolve some but not all. Confirm codero does not advance to merge_ready while any thread open. Resolve all. Confirm merge_ready fires. | P1-S5-06, P1-S1-05 |
| P1-S9-09 | inbox.log compaction | codero-cli compact --older-than 90d rewrites inbox.log retaining only bundles newer than cutoff. On compaction: write seq floor to SQLite first (durable), then SET seq_floor:<repo> in Redis. On recovery: read both, take max. compact explicitly skips agent_effectiveness table. Operator confirmation required. | P1-S4-04, P1-S5.6-08 |
| P1-S9-10 | GitHub token scope documentation | Document required scopes: repo (read), checks:write, admin:repo_hook. Missing scopes produce named error at startup, not cryptic 403 at runtime. | P1-S1-02 |
| P1-S9-11 | 30-day real use sign-off | Use codero daily on both projects for 30 days. Minimum thresholds: 3 branches reviewed/week, 2 stale detections observed, 1 lease expiry observed, 10 pre-commit reviews/project/week. SLO targets: zero missed feedback deliveries, zero silent queue stalls, zero undetected stale branches over the 30-day period. Phase 1 complete when daily use is unremarkable AND SLOs met. | all prior stages |

**Exit Gate:** All failure modes confirmed including Redis failure/recovery and pre-commit loop failures. Full end-to-end cycle confirmed including unresolved comment cross-check and auto-merge. Redis stress test passes. Webhook Redis NX dedup verified under 1ms. Pre-commit rate limit stress test produces zero API rate limit errors. inbox.log compaction preserves seq floor in both Redis (written second) and SQLite (written first). 30-day daily use at minimum activity thresholds completed. SLOs met. Phase 1 signed off.

---

## Phase Gate

~6 months of real-world personal use.

Phase 2 begins only after Phase 1 is proven in daily use. No commercial work starts earlier.

---

## Phase 2 — Commercial SaaS

Multi-tenant. Scalable. Sellable.

Phase 2 begins after ~6 months of proven Phase 1 operation. The state machine, WFQ scheduler, feedback bundle schema, inbox.log delivery model, pre-commit loop semantics, and all operator action semantics are preserved entirely. Phase 2 replaces the infrastructure beneath them and adds surfaces on top.

**What carries forward:** Redis moves from local single-node to a managed instance (non-cluster mode — see Decision 3), but all key naming conventions, Lua scripts, and keyspace notification patterns are unchanged. Pre-commit loop rate limiter scales to multi-tenant by namespacing slots per tenant. Agent effectiveness metrics gain a tenant_id dimension.

### Phase 2 Data Layer

| LAYER | ROLE | OWNS |
|-------|------|------|
| PostgreSQL (RDS/managed) | Durable source of truth | Multi-tenant branch state, feedback bundles, metering, audit log, agent effectiveness records. tenant_id on every table. Row-level security. |
| Redis (managed, non-cluster) | Ephemeral coordination + job queue | All Phase 1 Redis roles plus Asynq job queues, per-tenant rate limiting, pub/sub for live dashboard, per-tenant slot counters, per-tenant pre-commit rate limiters. |
| inbox.log (S3 / object store) | Append-only delivery (per-tenant) | One log stream per tenant/repo. Seq numbers still sourced from Redis INCR. Webhook delivery endpoint also available for tenants who register one. |

---

### Stage S1 — Infrastructure Overhaul

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P2-S1-01 | PostgreSQL migration | Replaces SQLite. Multi-tenant schema: tenant_id on all tables. Row-level security. golang-migrate handles migration with same numbered convention. | Phase 1 complete |
| P2-S1-02 | Connection pooling | PgBouncer or pgx connection pool. Required at SaaS scale. | P2-S1-01 |
| P2-S1-03 | Redis upgrade to managed instance | Phase 1 local Redis → Redis Cloud or AWS ElastiCache (non-cluster mode). All key naming conventions unchanged. Keyspace notifications explicitly enabled on managed Redis — add to setup checklist. | Phase 1 complete |
| P2-S1-04 | Keyspace notification validation | End-to-end validation of keyspace notification delivery in managed environment before cutover. Lease expiry, heartbeat abandoned, stale check triggers all confirmed working. Revisit cluster mode only at 50+ concurrent tenants. | P2-S1-03 |
| P2-S1-05 | Asynq job queue (Redis-native Go) | Replaces SQLite dispatcher. Per-tenant named queues: queue:<tenantID>:<repo>. Asynq provides durable execution, retries, timeouts, crash recovery using existing Redis. Idiomatic Go, no Node.js dependency. | P2-S1-03 |
| P2-S1-06 | Serverless webhook ingestion | Edge functions (AWS Lambda or Cloudflare Workers). Validates X-GitHub-Delivery via Redis NX (same Phase 1 pattern), drops payload to Asynq job, returns 200 immediately. | P2-S1-05, P2-S1-03 |
| P2-S1-07 | Containerised deployment | Docker + docker-compose for local dev. Kubernetes for production. Stateless application layer — all state in PostgreSQL and Redis. | P2-S1-01, P2-S1-03 |
| P2-S1-08 | Secrets management | All API keys move to AWS Secrets Manager or Vault. No credentials in environment variables in production. | P2-S1-07 |
| P2-S1-09 | V1 → SaaS migration tooling | Migrates Phase 1 SQLite to multi-tenant PostgreSQL, assigns tenant_id, migrates inbox.log to S3, migrates agent_effectiveness table, validates Redis key consistency. You are the first customer. | P2-S1-01, P2-S1-03 |

**Exit Gate:** PostgreSQL handles concurrent writes from multiple tenants. Managed Redis operational with keyspace notifications confirmed end-to-end. Asynq survives process kill and recovers. Webhook receiver returns 200 under load via Redis NX. V1-to-SaaS migration runs cleanly on Phase 1 database including agent_effectiveness data.

---

### Stage S2 — Tenant Onboarding and Multi-Org

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P2-S2-01 | GitHub App | Requests repo read, PR write, status check, pull_requests:write permissions. Complete permission set covering through Stage S4. | P2-S1-07 |
| P2-S2-02 | OAuth and installation flow | Web-based: GitHub OAuth → org selection → App installation → tenant provisioning. Must be frictionless. | P2-S2-01 |
| P2-S2-03 | Org-level routing rules | Tenant configures review policies at org level. Rules propagate to all repos automatically. | P2-S2-02 |
| P2-S2-04 | Multi-repo activation | tenant_id schema supports multiple repos per tenant. Per-tenant slot counters in Redis. Per-tenant pre-commit rate limiters namespaced as precommit:slots:<tenantID>. | P2-S1-01, P2-S1-03 |
| P2-S2-05 | Per-tenant queue isolation | WFQ scoring computed within tenant's Asynq named queue. High-volume tenant cannot affect another's queue. | P2-S1-05 |
| P2-S2-06 | Inter-tenant fairness | Global scheduler allocates dispatch slots across tenants using Redis sorted set of tenant queues, scored by tenant wait time and plan tier. Minimum guaranteed dispatch rate per plan. | P2-S2-05 |
| P2-S2-07 | Per-tenant rate limiting | Sliding window on webhook ingestion: ZADD ratelimit:<tenantID> <timestamp> <eventID>, ZREMRANGEBYSCORE, ZCARD. Tenants exceeding limit receive 429 with Retry-After. | P2-S1-03 |
| P2-S2-08 | Webhook routing by install ID | GitHub App webhooks include installation_id. Route to correct tenant's Asynq queue by this ID, not repo name. | P2-S2-01, P2-S1-05 |
| P2-S2-09 | Tenant provisioning API | Internal API for creating, suspending, deleting tenants. Deletion removes all PostgreSQL records, Redis keys in tenant namespace, inbox data from S3, and agent_effectiveness records. | P2-S1-01, P2-S1-03 |

**Exit Gate:** New org installs GitHub App, completes onboarding, receives review on first PR without manual config. Two tenants cannot affect each other's queue scores or slot allocation. Rate limiting returns 429 with Retry-After. Tenant deletion removes all data across PostgreSQL, Redis, and S3.

---

### Stage S3 — Web Dashboard and RBAC

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P2-S3-01 | Web application | Next.js or equivalent. Authenticated via GitHub OAuth session. | P2-S2-02 |
| P2-S3-02 | Live queue dashboard (Redis pub/sub) | State transition events published to Redis pub/sub: PUBLISH transitions:<tenantID> <event>. Web dashboard subscribes via WebSocket relay. Real-time without polling. | P2-S3-01, P2-S1-03 |
| P2-S3-03 | Agent effectiveness dashboard | Web equivalent of TUI effectiveness pane. Per-agent/per-project metrics with 7-day and 30-day views. Tenants see their own data only. | P2-S3-01, P2-S2-04 |
| P2-S3-04 | Branch detail and event log | State, retry count, lease status, inbox bundles. All operator actions available from detail view. | P2-S3-01 |
| P2-S3-05 | All operator actions ported | Every Phase 1 TUI action works in web UI with identical semantics. No regression. Includes reactivate. | P2-S3-01 |
| P2-S3-06 | RBAC | Viewer (read-only), Operator (all queue actions), Admin (billing, RBAC, tenant config). drain, abandon, release slot require Operator or Admin. | P2-S3-01 |
| P2-S3-07 | Audit log | All operator actions recorded with actor, timestamp, action, affected branch. Immutable. Admin-visible only. PostgreSQL. | P2-S3-06, P2-S1-01 |
| P2-S3-08 | Advisory assistant | operator-chat alias in web chat panel. Same [advisory]-only constraints. | P2-S3-01 |
| P2-S3-09 | TUI preservation | Phase 1 TUI remains fully functional for self-hosted. Not deprecated. | — |

**Exit Gate:** Live dashboard updates via Redis pub/sub without polling. Agent effectiveness dashboard displays per-tenant data. Viewer cannot execute state-changing actions. All actions produce identical transitions in web UI and TUI. All actions in audit log.

---

### Stage S4 — GitHub Status Checks and PR Integration

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P2-S4-01 | GitHub Checks API | Create check run on PR commit (in_progress). Update to success/failure on complete. | P2-S2-01 |
| P2-S4-02 | Merge blocking on critical findings | Error-severity findings → check run failed. GitHub branch protection blocks merge. Decision made by state machine, not AI. | P2-S4-01 |
| P2-S4-03 | Severity threshold config | Per-repo or per-org configurable severity level for blocking failure. | P2-S4-02, P2-S2-03 |
| P2-S4-04 | Inline PR annotations | Surface findings as inline code annotations in GitHub PR diff, linked to file and line from feedback bundle. | P2-S4-01 |
| P2-S4-05 | Re-run trigger | Developer clicks Re-run on failed check → new review request to codero via Asynq without CLI access. | P2-S4-01, P2-S1-05 |
| P2-S4-06 | Stale branch check status | stale_branch state updates check run with human-readable explanation. | P2-S4-01 |
| P2-S4-07 | Auto-merge via status check | On merge_ready, codero sets GitHub status check to passing. Relies on repo's auto-merge setting. codero never calls merge API directly — minimal permission scope. | P2-S4-01 |

**Exit Gate:** PR with error finding cannot merge with branch protection enabled. Clean review unblocks PR. Stale branch updates check run. Re-trigger works from GitHub UI. Auto-merge fires on merge_ready without calling merge API.

---

### Stage S5 — Pluggable Review Backends

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P2-S5-01 | Backend interface | Contract: receives (repo, branch, commit_hash, files_changed OR working_tree_diff), returns normalised findings. Supports both PR review mode and pre-commit diff mode. | P2-S4-01 |
| P2-S5-02 | CodeRabbit CLI backend (refactor) | Wrap existing CLI runner as conforming backend. No behaviour change. | P2-S5-01 |
| P2-S5-03 | Direct model API backend | Backend calling Anthropic, OpenAI, or Gemini directly. Owns prompt construction, chunking, response parsing. Token limits and diff size caps enforced per-invocation. | P2-S5-01 |
| P2-S5-04 | Per-tenant backend selection | Tenants configure which backend for PR reviews and pre-commit Loop 2 separately. Switching requires no infrastructure change. | P2-S5-02, P2-S5-03 |
| P2-S5-05 | Backend health monitoring | Health state cached in Redis with 30-second TTL. Scheduler detects outages, routes to fallback. Operator notified on failover. | P2-S5-04, P2-S1-03 |

**Exit Gate:** Either backend produces identical normalised findings. Switching requires no code change. Backend outage triggers automatic failover. Direct model API backend enforces per-invocation limits.

---

### Stage S6 — Billing, Metering, and Enterprise Security

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P2-S6-01 | Usage metering | Tracks billable events: PR reviews, pre-commit reviews, findings delivered, re-runs. Redis INCR for high-frequency counting with periodic PostgreSQL flush. | P2-S1-03, P2-S1-01 |
| P2-S6-02 | Stripe integration | Plan tiers (free, team, enterprise) with limits on concurrent reviews, repos, monthly reviews, pre-commit quota. Upgrade prompts at limits. | P2-S6-01 |
| P2-S6-03 | Plan limit enforcement | Scheduler checks plan limits from Redis cache (TTL 60s) before dispatch. Tenant at limit receives system bundle explaining block — not silent queue stall. | P2-S6-02, P2-S1-03 |
| P2-S6-04 | Stateless processing guarantee | Code content not written to any persistent store. Only metadata persists. Verified by dedicated audit. | P2-S1-01 |
| P2-S6-05 | Data residency documentation | Formal: what is stored, where, how long, deletion policy. Required for enterprise procurement. | P2-S6-04 |
| P2-S6-06 | Tenant data deletion | On cancellation: purge PostgreSQL, Redis, S3, agent_effectiveness. Return signed deletion certificate. | P2-S2-09 |
| P2-S6-07 | SOC 2 readiness gap analysis | Run at start of S6. Findings may require 3-6 months remediation. Audit log, RBAC, stateless processing, secrets management, access controls form Type 1 evidence set. | P2-S3-07, P2-S3-06, P2-S6-04, P2-S1-08 |

**Exit Gate:** Tenant at plan limit receives system bundle, not silence. Usage metering Redis counters flush to PostgreSQL. Code audit confirms zero code content in persistent store. Tenant offboarding deletes all data with signed certificate. SOC 2 gap analysis initiated with remediation timeline.

---

### Stage S7 — Temporal Upgrade (Conditional)

Only adopt Temporal when one of these triggers is met: (1) review workflows require multi-step DAGs Asynq cannot model cleanly, (2) concurrent tenant count produces measurable state drift in Asynq, or (3) engineering team has capacity to absorb a Temporal cluster.

| TASK ID | TASK | NOTES | DEPENDS ON |
|---------|------|-------|------------|
| P2-S7-01 | Temporal cluster provisioning | Worker nodes, frontend server, matching service. Backed by PostgreSQL from S1. Use Temporal Cloud if self-managed premature. | trigger condition met |
| P2-S7-02 | Workflow migration | Each review pipeline becomes a Temporal workflow. State transitions become activities. Asynq queue drained before cutover. Redis retained for leases, rate limiting, pub/sub, dedup, pre-commit slots. | P2-S7-01 |
| P2-S7-03 | Asynq deprecation | Once Temporal workflows stable in production for 30 days, Asynq deprecated. Redis not replaced. | P2-S7-02 |

**Exit Gate:** If adopted: all review workflows run as Temporal workflows for 30 days without state drift. Asynq drained and deprecated. Redis retained for all coordination roles. If deferred: Asynq documented as long-term solution with revisit date recorded.

---

## Execution Sequence Summary

This is the critical path through Phase 1. Stages can overlap where dependencies allow.

**Sequential critical path:** S0.5 → S1 → S2 → S3 → S4 → S5 → S5.5 → S5.6 → S6 → S7 → S8 → S9

**Parallel tracks:** S1.5 runs in parallel with S1 and S2 (CI/CD tooling). S6 (TUI) can begin once S3 (scheduler) is complete and advance incrementally as later stages add features. S7 (observability) can begin the HTTP server early and add metrics as they become available.

**Strict ordering constraints:**

- S1 must complete before S2 (CLI needs state machine).
- S3 must complete before S4 (CodeRabbit runner needs lease/dispatch).
- S4 must complete before S5 (webhook ingestion needs finding normaliser and inbox writer).
- S5.5 depends on S4 (LiteLLM circuit breaker) and S2 (CLI commands). The precommit-review LiteLLM alias (P1-S5.5-07, P1-S5.5-08) is built here, not in S8.
- S5.6 depends on S5.5 (needs pre-commit review data to measure).
- S8 depends on S5.5 (LiteLLM client wrapper already exists; S8 adds remaining aliases).
- S9 depends on all prior stages (integration testing + sign-off).

**Total Phase 1 task count:** 113 tasks across 12 stages. Phase 2 adds 49 tasks across 7 stages.

---

## Complete Deferral Register

Every item deferred from Phase 1 with target phase and reason.

| DEFERRED ITEM | DEFERRED TO | WHY |
|---------------|-------------|-----|
| PostgreSQL / distributed infrastructure | Phase 2 — S1 | Single-user has no concurrent write problem. |
| Redis upgrade to managed instance (non-cluster) | Phase 2 — S1 | Local Redis sufficient for Phase 1. |
| Serverless webhook ingestion | Phase 2 — S1 | Personal use load does not threaten 10-second window. |
| Asynq job queue (replaces BullMQ — Go-native) | Phase 2 — S1 | SQLite dispatcher + Redis sorted set sufficient for single user. |
| V1 → SaaS migration tooling | Phase 2 — S1 (pre-launch) | Required before first commercial customer. |
| Multi-tenant schema and queue isolation | Phase 2 — S1/S2 | Single user. No tenants. |
| Inter-tenant fairness mechanism | Phase 2 — S2 | No multiple tenants in Phase 1. |
| Per-tenant pre-commit rate limiters | Phase 2 — S2 | Single rate limiter suffices. |
| Per-tenant webhook rate limiting | Phase 2 — S2 | No external webhook traffic at personal scale. |
| GitHub App and org-level installation | Phase 2 — S2 | Personal use only needs single repo installation. |
| Multi-repo and multi-org activation | Phase 2 — S2 | Architected in S1 schema but not activated until S2. |
| Web dashboard (Redis pub/sub) | Phase 2 — S3 | TUI + codero.goels.in API sufficient for personal use. |
| RBAC | Phase 2 — S3 | Single user. No role separation. |
| Audit log | Phase 2 — S3 | Single user. Internal structured log sufficient. |
| GitHub Checks API and merge blocking | Phase 2 — S4 | inbox.log polling sufficient. Checks require GitHub App. |
| Inline PR annotations | Phase 2 — S4 | Requires GitHub App write permissions. |
| Auto-merge via status check (formal) | Phase 2 — S4 | Phase 1 relies on repo setting + status check. |
| Pluggable review backends | Phase 2 — S5 | CodeRabbit CLI is the only backend needed. |
| Direct model API backend — cost controls | Phase 2 — S5 | No per-invocation cost risk at personal scale. |
| Redis-cached backend health state | Phase 2 — S5 | Only one backend in Phase 1. |
| Billing and metering (Redis-assisted) | Phase 2 — S6 | No commercial activity. |
| Stripe integration and plan limits | Phase 2 — S6 | No commercial activity. |
| Tenant data deletion / offboarding | Phase 2 — S6 | No tenants. Legal requirement before first customer. |
| Stateless processing formal audit | Phase 2 — S6 | Phase 1 disables LiteLLM logging. Formal audit deferred. |
| Data residency documentation | Phase 2 — S6 | Not a personal use concern. |
| SOC 2 readiness | Phase 2 — S6 (start) | Enterprise compliance deferred until enterprise customers exist. |
| Temporal durable execution | Phase 2 — S7 (conditional) | Asynq (Redis-native Go) sufficient. Temporal only at multi-tenant DAG complexity. |

---

*This document is implementation-ready. Task IDs are stable references for agent execution. Each stage is an independently reviewable unit of work. Start with Stage 0.5 (P1-S0.5-01).*
