// Package context implements the Repo-Context v1 spec: a repo-local,
// advisory-only code-intelligence subsystem backed by a SQLite graph store.
//
// Outputs from this package are strictly advisory. They MUST NOT drive
// branch-state transitions, gate ordering, merge-readiness, or any
// daemon/task/session state mutation.
package context

import "time"

// Schema version for the graph.db contract.
const SchemaVersion = "1"

// Symbol kinds (stable vocabulary per spec §5).
const (
	KindPackage   = "package"
	KindFile      = "file"
	KindFunction  = "function"
	KindMethod    = "method"
	KindStruct    = "struct"
	KindInterface = "interface"
	KindType      = "type"
	KindConst     = "const"
	KindVar       = "var"
)

// Edge kinds (stable vocabulary per spec §5).
const (
	EdgeImports    = "imports"
	EdgeDefines    = "defines"
	EdgeCalls      = "calls"
	EdgeReferences = "references"
	EdgeEmbeds     = "embeds"
)

// Index states (stable vocabulary per spec §5).
const (
	IndexReady           = "ready"
	IndexMissing         = "missing"
	IndexRebuildRequired = "rebuild_required"
)

// Risk levels (stable vocabulary per spec §5).
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Symbol represents a code symbol stored in the graph.
type Symbol struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	FilePath   string `json:"file_path"`
	LineStart  int    `json:"line_start"`
	LineEnd    int    `json:"line_end"`
	Signature  string `json:"signature"`
	Exported   bool   `json:"exported"`
	SourceHash string `json:"source_hash"`
}

// Edge represents a directed relationship between two symbols.
type Edge struct {
	SourceID   string `json:"source_id"`
	TargetID   string `json:"target_id"`
	Kind       string `json:"kind"`
	FilePath   string `json:"file_path"`
	LineNumber int    `json:"line_number"`
}

// DepEdge is an edge as returned by the deps command (target is expanded).
type DepEdge struct {
	Kind       string `json:"kind"`
	SourceID   string `json:"source_id"`
	Target     Symbol `json:"target"`
	FilePath   string `json:"file_path"`
	LineNumber int    `json:"line_number"`
}

// RdepEdge is an edge as returned by the rdeps command (source is expanded).
type RdepEdge struct {
	Kind       string `json:"kind"`
	Source     Symbol `json:"source"`
	TargetID   string `json:"target_id"`
	FilePath   string `json:"file_path"`
	LineNumber int    `json:"line_number"`
}

// GrepMatch represents a single grep hit in a file.
type GrepMatch struct {
	FilePath   string `json:"file_path"`
	LineNumber int    `json:"line_number"`
	LineText   string `json:"line_text"`
	MatchStart int    `json:"match_start"`
	MatchEnd   int    `json:"match_end"`
}

// Metadata holds graph.db index metadata.
type Metadata struct {
	RepoRoot       string `json:"repo_root"`
	IndexVersion   string `json:"index_version"`
	LastIndexedAt  string `json:"last_indexed_at"`
	LastIndexedSHA string `json:"last_indexed_sha"`
	LanguageScope  string `json:"language_scope"`
	SchemaVersion  string `json:"schema_version"`
}

// ──── Response Envelopes ─────────────────────────────────────────────────

// Envelope is the base response envelope included in all JSON outputs.
type Envelope struct {
	SchemaVersion string `json:"schema_version"`
	Command       string `json:"command"`
	RepoRoot      string `json:"repo_root"`
	GeneratedAt   string `json:"generated_at"`
}

// NewEnvelope creates a base envelope for the given command and repo root.
func NewEnvelope(command, repoRoot string) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersion,
		Command:       command,
		RepoRoot:      repoRoot,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

// ErrorResponse is the error envelope per spec.
type ErrorResponse struct {
	Envelope
	Error ErrorDetail `json:"error"`
}

// ErrorDetail holds the error code and message.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// IndexResponse is the JSON output of `codero context index`.
type IndexResponse struct {
	Envelope
	Warnings      []string `json:"warnings"`
	IndexState    string   `json:"index_state"`
	DBPresent     bool     `json:"db_present"`
	DBPath        string   `json:"db_path"`
	FileCount     int      `json:"file_count"`
	SymbolCount   int      `json:"symbol_count"`
	EdgeCount     int      `json:"edge_count"`
	LastIndexedAt string   `json:"last_indexed_at"`
}

// StatusResponse is the JSON output of `codero context status`.
type StatusResponse struct {
	Envelope
	IndexState     string `json:"index_state"`
	DBPresent      bool   `json:"db_present"`
	DBPath         string `json:"db_path"`
	DBSizeBytes    int64  `json:"db_size_bytes"`
	FileCount      int    `json:"file_count"`
	SymbolCount    int    `json:"symbol_count"`
	EdgeCount      int    `json:"edge_count"`
	LastIndexedAt  string `json:"last_indexed_at"`
	LastIndexedSHA string `json:"last_indexed_sha"`
	LanguageScope  string `json:"language_scope"`
}

// FindResponse is the JSON output of `codero context find`.
type FindResponse struct {
	Envelope
	Query   string   `json:"query"`
	Count   int      `json:"count"`
	Matches []Symbol `json:"matches"`
}

// GrepResponse is the JSON output of `codero context grep`.
type GrepResponse struct {
	Envelope
	Pattern string      `json:"pattern"`
	Count   int         `json:"count"`
	Matches []GrepMatch `json:"matches"`
}

// SymbolsResponse is the JSON output of `codero context symbols`.
type SymbolsResponse struct {
	Envelope
	FilePath string   `json:"file_path"`
	Count    int      `json:"count"`
	Symbols  []Symbol `json:"symbols"`
}

// DepsResponse is the JSON output of `codero context deps`.
type DepsResponse struct {
	Envelope
	Subject      Symbol    `json:"subject"`
	Count        int       `json:"count"`
	Dependencies []DepEdge `json:"dependencies"`
}

// RdepsResponse is the JSON output of `codero context rdeps`.
type RdepsResponse struct {
	Envelope
	Subject    Symbol     `json:"subject"`
	Count      int        `json:"count"`
	Dependents []RdepEdge `json:"dependents"`
}

// ImpactSummary holds aggregate counts for the impact response.
type ImpactSummary struct {
	Files          int `json:"files"`
	TouchedSymbols int `json:"touched_symbols"`
	Dependents     int `json:"dependents"`
}

// ImpactResponse is the JSON output of `codero context impact`.
type ImpactResponse struct {
	Envelope
	AnalysisState  string        `json:"analysis_state"`
	InputMode      string        `json:"input_mode"`
	Files          []string      `json:"files"`
	TouchedSymbols []Symbol      `json:"touched_symbols"`
	Dependents     []Symbol      `json:"dependents"`
	RiskLevel      string        `json:"risk_level,omitempty"`
	Reasons        []string      `json:"reasons,omitempty"`
	Summary        ImpactSummary `json:"summary"`
}
