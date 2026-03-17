package gatecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Engine runs all configured checks and produces a Report.
type Engine struct {
	cfg EngineConfig
}

// NewEngine creates an Engine with the given configuration.
func NewEngine(cfg EngineConfig) *Engine {
	return &Engine{cfg: cfg}
}

// Run executes all checks and returns the canonical Report.
// Checks are never omitted from the output: disabled and skipped checks appear
// with appropriate status and reason codes.
func (e *Engine) Run(ctx context.Context) Report {
	staged := e.stagedFiles()

	var checks []CheckResult
	var infraCount int

	runners := e.buildRunners()
	for _, r := range runners {
		result := r(ctx, e.cfg, staged)
		if result.Status == StatusInfraBypass {
			infraCount++
			if infraCount > e.cfg.MaxInfraBypass {
				result.Status = StatusFail
				result.Reason = fmt.Sprintf("infra bypass budget exceeded (%d/%d): %s",
					infraCount, e.cfg.MaxInfraBypass, result.Reason)
			}
		}
		checks = append(checks, result)
	}

	summary := ComputeSummary(checks, e.cfg.Profile, e.cfg.AllowRequiredSkip)
	return Report{
		Summary: summary,
		Checks:  checks,
		RunAt:   time.Now().UTC(),
	}
}

// stagedFiles returns the list of staged files for this engine run.
func (e *Engine) stagedFiles() []string {
	if e.cfg.StagedFiles != nil {
		return e.cfg.StagedFiles
	}
	repoPath := e.cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	args := []string{"-C", repoPath, "diff", "--cached", "--name-only", "--diff-filter=ACM"}
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	out, err := exec.Command("git", args...).Output() //nolint:gosec
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files
}

// buildRunners returns the ordered list of check runner functions.
func (e *Engine) buildRunners() []func(context.Context, EngineConfig, []string) CheckResult {
	return []func(context.Context, EngineConfig, []string) CheckResult{
		runFileSizeCheck,
		runMergeMarkersCheck,
		runTrailingWhitespaceCheck,
		runFinalNewlineCheck,
		runForbiddenPathsCheck,
		runConfigValidationCheck,
		runLockfileSyncCheck,
		runExecBitPolicyCheck,
		runGofmtCheck,
		runGitleaksCheck,
		runSemgrepCheck,
		runRuffLintCheck,
		runAIGateCheck,
	}
}

// --- helpers ---

// findTool resolves a tool path. If nameOrPath contains a slash, it is used
// directly (stat'd for existence). Otherwise PATH is searched.
// Returns (resolvedPath, found).
func findTool(nameOrPath string) (string, bool) {
	if strings.ContainsRune(nameOrPath, '/') {
		if _, err := os.Stat(nameOrPath); err == nil {
			return nameOrPath, true
		}
		return "", false
	}
	p, err := exec.LookPath(nameOrPath)
	if err != nil {
		return "", false
	}
	return p, true
}

// filterBySuffix returns staged files matching any of the given suffixes.
func filterBySuffix(staged []string, suffixes ...string) []string {
	var out []string
	for _, f := range staged {
		for _, suf := range suffixes {
			if strings.HasSuffix(strings.ToLower(f), suf) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// start records a start time for duration measurement.
func start() time.Time { return time.Now() }

// elapsedMS returns elapsed milliseconds since t.
func elapsedMS(t time.Time) int64 { return time.Since(t).Milliseconds() }

// disabledResult returns a DISABLED CheckResult with the given reason.
func disabledResult(id, name string, group Group, required bool, code ReasonCode, reason string) CheckResult {
	return CheckResult{
		ID: id, Name: name, Group: group, Required: required,
		Enabled: false, Status: StatusDisabled,
		ReasonCode: code, Reason: reason,
	}
}

// skipResult returns a SKIP CheckResult.
func skipResult(id, name string, group Group, required bool, code ReasonCode, reason string) CheckResult {
	return CheckResult{
		ID: id, Name: name, Group: group, Required: required,
		Enabled: true, Status: StatusSkip,
		ReasonCode: code, Reason: reason,
	}
}

// --- check runners ---

func runFileSizeCheck(_ context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "file-size", "File size limit"
	t := start()
	result := CheckResult{ID: id, Name: name, Group: GroupFormat, Required: true, Enabled: true}
	if len(staged) == 0 {
		result.Status = StatusSkip
		result.ReasonCode = ReasonNotInScope
		result.Reason = "no staged files"
		result.DurationMS = elapsedMS(t)
		return result
	}
	limit := cfg.MaxStagedFileBytes
	if limit <= 0 {
		limit = 1 << 20 // 1 MiB
	}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	var offenders []string
	for _, f := range staged {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(repoPath, f)
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if info.Size() > limit {
			offenders = append(offenders, fmt.Sprintf("%s (%d bytes)", f, info.Size()))
		}
	}
	result.DurationMS = elapsedMS(t)
	if len(offenders) > 0 {
		result.Status = StatusFail
		result.Details = fmt.Sprintf("files exceeding %d bytes: %s", limit, strings.Join(offenders, "; "))
		return result
	}
	result.Status = StatusPass
	return result
}

func runMergeMarkersCheck(_ context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "merge-markers", "Merge conflict markers"
	t := start()
	result := CheckResult{ID: id, Name: name, Group: GroupFormat, Required: true, Enabled: true}
	if len(staged) == 0 {
		result.Status = StatusSkip
		result.ReasonCode = ReasonNotInScope
		result.Reason = "no staged files"
		result.DurationMS = elapsedMS(t)
		return result
	}
	markers := []string{"<<<<<<< ", "=======", ">>>>>>> "}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	var hits []string
	for _, f := range staged {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(repoPath, f)
		}
		data, err := os.ReadFile(abs) //nolint:gosec
		if err != nil {
			continue
		}
		for _, m := range markers {
			if bytes.Contains(data, []byte(m)) {
				hits = append(hits, f)
				break
			}
		}
	}
	result.DurationMS = elapsedMS(t)
	if len(hits) > 0 {
		result.Status = StatusFail
		result.Details = "merge conflict markers found in: " + strings.Join(hits, ", ")
		return result
	}
	result.Status = StatusPass
	return result
}

func runTrailingWhitespaceCheck(_ context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "trailing-whitespace", "Trailing whitespace"
	t := start()
	if cfg.Profile == ProfileOff {
		r := skipResult(id, name, GroupFormat, false, ReasonNotInScope, "profile=off")
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupFormat, Required: false, Enabled: true}
	if len(staged) == 0 {
		result.Status = StatusSkip
		result.ReasonCode = ReasonNotInScope
		result.Reason = "no staged files"
		result.DurationMS = elapsedMS(t)
		return result
	}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	var hits []string
	for _, f := range staged {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(repoPath, f)
		}
		data, err := os.ReadFile(abs) //nolint:gosec
		if err != nil {
			continue
		}
		if !isTextContent(data) {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if len(line) > 0 && (line[len(line)-1] == ' ' || line[len(line)-1] == '\t') {
				hits = append(hits, f)
				break
			}
		}
	}
	result.DurationMS = elapsedMS(t)
	if len(hits) > 0 {
		result.Status = StatusFail
		result.Details = "trailing whitespace in: " + strings.Join(hits, ", ")
		return result
	}
	result.Status = StatusPass
	return result
}

func runFinalNewlineCheck(_ context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "final-newline", "Missing final newline"
	t := start()
	if cfg.Profile == ProfileOff {
		r := skipResult(id, name, GroupFormat, false, ReasonNotInScope, "profile=off")
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupFormat, Required: false, Enabled: true}
	if len(staged) == 0 {
		result.Status = StatusSkip
		result.ReasonCode = ReasonNotInScope
		result.Reason = "no staged files"
		result.DurationMS = elapsedMS(t)
		return result
	}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	var hits []string
	for _, f := range staged {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(repoPath, f)
		}
		data, err := os.ReadFile(abs) //nolint:gosec
		if err != nil || len(data) == 0 {
			continue
		}
		if !isTextContent(data) {
			continue
		}
		if data[len(data)-1] != '\n' {
			hits = append(hits, f)
		}
	}
	result.DurationMS = elapsedMS(t)
	if len(hits) > 0 {
		result.Status = StatusFail
		result.Details = "missing final newline in: " + strings.Join(hits, ", ")
		return result
	}
	result.Status = StatusPass
	return result
}

func runForbiddenPathsCheck(_ context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "forbidden-paths", "Forbidden path blocker"
	t := start()
	if !cfg.EnforceForbiddenPaths || cfg.ForbiddenPathRegex == "" {
		r := disabledResult(id, name, GroupConfig, true, ReasonNotApplicable, "CODERO_ENFORCE_FORBIDDEN_PATHS not set")
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupConfig, Required: true, Enabled: true}
	re, err := regexp.Compile(cfg.ForbiddenPathRegex)
	if err != nil {
		result.Status = StatusFail
		result.ReasonCode = ReasonExecError
		result.Reason = "invalid CODERO_FORBIDDEN_PATH_REGEX: " + err.Error()
		result.DurationMS = elapsedMS(t)
		return result
	}
	var hits []string
	for _, f := range staged {
		if re.MatchString(f) {
			hits = append(hits, f)
		}
	}
	result.DurationMS = elapsedMS(t)
	if len(hits) > 0 {
		result.Status = StatusFail
		result.Details = "forbidden paths: " + strings.Join(hits, ", ")
		return result
	}
	result.Status = StatusPass
	return result
}

func runConfigValidationCheck(_ context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "config-validation", "Config file validation (JSON/YAML)"
	t := start()
	result := CheckResult{ID: id, Name: name, Group: GroupConfig, Required: true, Enabled: true}

	jsonFiles := filterBySuffix(staged, ".json")
	yamlFiles := filterBySuffix(staged, ".yaml", ".yml")
	all := append(jsonFiles, yamlFiles...) //nolint:gocritic

	if len(all) == 0 {
		result.Status = StatusSkip
		result.ReasonCode = ReasonNotInScope
		result.Reason = "no JSON/YAML staged files"
		result.DurationMS = elapsedMS(t)
		return result
	}

	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	var errs []string
	for _, f := range jsonFiles {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(repoPath, f)
		}
		data, err := os.ReadFile(abs) //nolint:gosec
		if err != nil {
			continue
		}
		if !json.Valid(data) {
			errs = append(errs, fmt.Sprintf("%s: invalid JSON", f))
		}
	}
	for _, f := range yamlFiles {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(repoPath, f)
		}
		data, err := os.ReadFile(abs) //nolint:gosec
		if err != nil {
			continue
		}
		var node yaml.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", f, err))
		}
	}
	result.DurationMS = elapsedMS(t)
	if len(errs) > 0 {
		result.Status = StatusFail
		result.Details = strings.Join(errs, "; ")
		return result
	}
	result.Status = StatusPass
	return result
}

func runLockfileSyncCheck(_ context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "lockfile-sync", "Lockfile sync check"
	t := start()
	if !cfg.EnforceLockfileSync {
		r := disabledResult(id, name, GroupConfig, false, ReasonUserDisabled, "CODERO_ENFORCE_LOCKFILE_SYNC not set")
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupConfig, Required: false, Enabled: true}
	var hasGoMod, hasGoSum, hasPkgJSON, hasPkgLock bool
	for _, f := range staged {
		base := filepath.Base(f)
		switch base {
		case "go.mod":
			hasGoMod = true
		case "go.sum":
			hasGoSum = true
		case "package.json":
			hasPkgJSON = true
		case "package-lock.json":
			hasPkgLock = true
		}
	}
	var issues []string
	if hasGoMod && !hasGoSum {
		issues = append(issues, "go.mod staged without go.sum")
	}
	if hasPkgJSON && !hasPkgLock {
		issues = append(issues, "package.json staged without package-lock.json")
	}
	result.DurationMS = elapsedMS(t)
	if len(issues) > 0 {
		result.Status = StatusFail
		result.Details = strings.Join(issues, "; ")
		return result
	}
	if !hasGoMod && !hasPkgJSON {
		result.Status = StatusSkip
		result.ReasonCode = ReasonNotInScope
		result.Reason = "no lockfile pair staged"
		return result
	}
	result.Status = StatusPass
	return result
}

func runExecBitPolicyCheck(_ context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "exec-bit-policy", "Executable bit policy"
	t := start()
	if !cfg.EnforceExecutablePolicy {
		r := disabledResult(id, name, GroupConfig, false, ReasonUserDisabled, "CODERO_ENFORCE_EXECUTABLE_POLICY not set")
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupConfig, Required: false, Enabled: true}
	if len(staged) == 0 {
		result.Status = StatusSkip
		result.ReasonCode = ReasonNotInScope
		result.Reason = "no staged files"
		result.DurationMS = elapsedMS(t)
		return result
	}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	var hits []string
	for _, f := range staged {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(repoPath, f)
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if info.Mode()&0o111 != 0 {
			ext := strings.ToLower(filepath.Ext(f))
			if ext != ".sh" && ext != "" {
				hits = append(hits, f)
			}
		}
	}
	result.DurationMS = elapsedMS(t)
	if len(hits) > 0 {
		result.Status = StatusFail
		result.Details = "unexpected executable bit on non-shell files: " + strings.Join(hits, ", ")
		return result
	}
	result.Status = StatusPass
	return result
}

func runGofmtCheck(ctx context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "gofmt", "Go formatting (gofmt)"
	t := start()
	if cfg.Profile == ProfileOff {
		r := skipResult(id, name, GroupFormat, false, ReasonNotInScope, "profile=off")
		r.DurationMS = elapsedMS(t)
		return r
	}
	goFiles := filterBySuffix(staged, ".go")
	if len(goFiles) == 0 {
		r := skipResult(id, name, GroupFormat, false, ReasonNotInScope, "no staged .go files")
		r.DurationMS = elapsedMS(t)
		return r
	}
	toolPath, ok := findTool("gofmt")
	if !ok {
		r := disabledResult(id, name, GroupFormat, false, ReasonMissingTool, "gofmt not found in PATH")
		r.ToolName = "gofmt"
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupFormat, Required: false, Enabled: true, ToolName: "gofmt", ToolPath: toolPath}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	var unformatted []string
	for _, f := range goFiles {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(repoPath, f)
		}
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		out, err := exec.CommandContext(ctx, toolPath, "-l", abs).Output() //nolint:gosec
		if err != nil {
			continue
		}
		if len(strings.TrimSpace(string(out))) > 0 {
			unformatted = append(unformatted, f)
		}
	}
	result.DurationMS = elapsedMS(t)
	if len(unformatted) > 0 {
		result.Status = StatusFail
		result.Details = "unformatted Go files: " + strings.Join(unformatted, ", ")
		return result
	}
	result.Status = StatusPass
	return result
}

func runGitleaksCheck(ctx context.Context, cfg EngineConfig, _ []string) CheckResult {
	const id, name = "gitleaks-staged", "Secret scan (gitleaks)"
	t := start()
	if cfg.Profile == ProfileOff {
		r := skipResult(id, name, GroupSecurity, true, ReasonNotInScope, "profile=off")
		r.DurationMS = elapsedMS(t)
		return r
	}
	toolPath, ok := findTool(cfg.GitleaksPath)
	if !ok {
		r := disabledResult(id, name, GroupSecurity, true, ReasonMissingTool, "gitleaks not found")
		r.ToolName = "gitleaks"
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupSecurity, Required: true, Enabled: true, ToolName: "gitleaks", ToolPath: toolPath}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	args := []string{"protect", "--staged", "--no-banner", "--exit-code", "1"}
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, toolPath, args...) //nolint:gosec
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	result.DurationMS = elapsedMS(t)
	if err != nil {
		if isContextError(err) {
			result.Status = StatusInfraBypass
			result.ReasonCode = ReasonInfraTimeout
			result.Reason = "gitleaks timed out"
			return result
		}
		result.Status = StatusFail
		result.Details = strings.TrimSpace(string(out))
		return result
	}
	result.Status = StatusPass
	return result
}

func runSemgrepCheck(ctx context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "semgrep", "SAST scan (semgrep)"
	t := start()
	if cfg.Profile == ProfileOff {
		r := skipResult(id, name, GroupSecurity, false, ReasonNotInScope, "profile=off")
		r.DurationMS = elapsedMS(t)
		return r
	}
	if len(staged) == 0 {
		r := skipResult(id, name, GroupSecurity, false, ReasonNotInScope, "no staged files")
		r.DurationMS = elapsedMS(t)
		return r
	}
	toolPath, ok := findTool(cfg.SemgrepPath)
	if !ok {
		r := disabledResult(id, name, GroupSecurity, false, ReasonMissingTool, "semgrep not found")
		r.ToolName = "semgrep"
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupSecurity, Required: false, Enabled: true, ToolName: "semgrep", ToolPath: toolPath}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	args := []string{"--config", "auto", "--quiet", "--error"}
	args = append(args, staged...)
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, toolPath, args...) //nolint:gosec
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	result.DurationMS = elapsedMS(t)
	if err != nil {
		if isContextError(err) {
			result.Status = StatusInfraBypass
			result.ReasonCode = ReasonInfraTimeout
			result.Reason = "semgrep timed out"
			return result
		}
		outStr := strings.TrimSpace(string(out))
		lc := strings.ToLower(outStr)
		if strings.Contains(lc, "401") || strings.Contains(lc, "unauthorized") || strings.Contains(lc, "authentication") {
			result.Status = StatusInfraBypass
			result.ReasonCode = ReasonInfraAuth
			result.Reason = "semgrep authentication failure"
			return result
		}
		if strings.Contains(lc, "rate limit") || strings.Contains(lc, "429") {
			result.Status = StatusInfraBypass
			result.ReasonCode = ReasonInfraRateLimit
			result.Reason = "semgrep rate limited"
			return result
		}
		if strings.Contains(lc, "connection refused") || strings.Contains(lc, "network") || strings.Contains(lc, "timeout") {
			result.Status = StatusInfraBypass
			result.ReasonCode = ReasonInfraNetwork
			result.Reason = "semgrep network failure"
			return result
		}
		result.Status = StatusFail
		result.Details = outStr
		return result
	}
	result.Status = StatusPass
	return result
}

func runRuffLintCheck(ctx context.Context, cfg EngineConfig, staged []string) CheckResult {
	const id, name = "ruff-lint", "Python lint (ruff)"
	t := start()
	if cfg.Profile == ProfileOff {
		r := skipResult(id, name, GroupLint, false, ReasonNotInScope, "profile=off")
		r.DurationMS = elapsedMS(t)
		return r
	}
	pyFiles := filterBySuffix(staged, ".py")
	if len(pyFiles) == 0 {
		r := skipResult(id, name, GroupLint, false, ReasonNotInScope, "no staged .py files")
		r.DurationMS = elapsedMS(t)
		return r
	}
	toolPath, ok := findTool(cfg.RuffPath)
	if !ok {
		r := disabledResult(id, name, GroupLint, false, ReasonMissingTool, "ruff not found")
		r.ToolName = "ruff"
		r.DurationMS = elapsedMS(t)
		return r
	}
	result := CheckResult{ID: id, Name: name, Group: GroupLint, Required: false, Enabled: true, ToolName: "ruff", ToolPath: toolPath}
	repoPath := cfg.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	args := append([]string{"check", "--quiet"}, pyFiles...)
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, toolPath, args...) //nolint:gosec
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	result.DurationMS = elapsedMS(t)
	if err != nil {
		if isContextError(err) {
			result.Status = StatusInfraBypass
			result.ReasonCode = ReasonInfraTimeout
			result.Reason = "ruff timed out"
			return result
		}
		result.Status = StatusFail
		result.Details = strings.TrimSpace(string(out))
		return result
	}
	result.Status = StatusPass
	return result
}

func runAIGateCheck(_ context.Context, _ EngineConfig, _ []string) CheckResult {
	const id, name = "ai-gate", "AI review gate"
	t := start()
	r := disabledResult(id, name, GroupAI, false, ReasonNotInScope,
		"AI gate is run separately via `codero commit-gate`; use gate-check for local checks only")
	r.DurationMS = elapsedMS(t)
	return r
}

// isTextContent returns true if the data appears to be UTF-8 text (no null bytes).
func isTextContent(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	return !bytes.Contains(data, []byte{0})
}

// isContextError returns true if the error is due to context cancellation or timeout.
func isContextError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "signal: killed")
}
