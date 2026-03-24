package context

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// DefaultDBRelPath is the repo-relative path to the graph store.
const DefaultDBRelPath = ".codero/context/graph.db"

// Store is the SQLite-backed graph store for repo-context.
type Store struct {
	db   *sql.DB
	path string
}

// OpenStore opens or creates the graph.db at the given path.
// Parent directories are created automatically. If the file does not exist,
// a fresh schema is applied.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("context: create directory: %w", err)
	}

	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("context: open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("context: apply schema: %w", err)
	}

	return &Store{db: db, path: path}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the filesystem path to the graph.db file.
func (s *Store) Path() string { return s.path }

// DB returns the underlying *sql.DB for direct queries.
func (s *Store) DB() *sql.DB { return s.db }

// applySchema creates the required tables if they don't exist.
func applySchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS symbols (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL DEFAULT '',
	kind        TEXT NOT NULL,
	file_path   TEXT NOT NULL DEFAULT '',
	line_start  INTEGER NOT NULL DEFAULT 0,
	line_end    INTEGER NOT NULL DEFAULT 0,
	signature   TEXT NOT NULL DEFAULT '',
	exported    BOOLEAN NOT NULL DEFAULT 0,
	source_hash TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file_path);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(kind);

CREATE TABLE IF NOT EXISTS edges (
	source_id   TEXT NOT NULL,
	target_id   TEXT NOT NULL,
	kind        TEXT NOT NULL,
	file_path   TEXT NOT NULL DEFAULT '',
	line_number INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source_id, target_id, kind, line_number),
	FOREIGN KEY (source_id) REFERENCES symbols(id) ON DELETE CASCADE,
	FOREIGN KEY (target_id) REFERENCES symbols(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);

CREATE TABLE IF NOT EXISTS metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`
	_, err := db.Exec(schema)
	return err
}

// ──── Symbol CRUD ────────────────────────────────────────────────────────

// InsertSymbol inserts or replaces a symbol row.
func (s *Store) InsertSymbol(sym Symbol) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO symbols
		(id, name, kind, file_path, line_start, line_end, signature, exported, source_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sym.ID, sym.Name, sym.Kind, sym.FilePath,
		sym.LineStart, sym.LineEnd, sym.Signature,
		sym.Exported, sym.SourceHash)
	return err
}

// InsertEdge inserts or ignores an edge row.
func (s *Store) InsertEdge(e Edge) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO edges
		(source_id, target_id, kind, file_path, line_number)
		VALUES (?, ?, ?, ?, ?)`,
		e.SourceID, e.TargetID, e.Kind, e.FilePath, e.LineNumber)
	return err
}

// DeleteFileSymbols removes all symbols and their edges for a given file.
func (s *Store) DeleteFileSymbols(filePath string) error {
	// Edges cascade via FK; delete symbols for this file.
	_, err := s.db.Exec(`DELETE FROM symbols WHERE file_path = ?`, filePath)
	return err
}

// ──── Metadata ───────────────────────────────────────────────────────────

// SetMeta sets a metadata key-value pair.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)`, key, value)
	return err
}

// GetMeta retrieves a metadata value. Returns "" if not found.
func (s *Store) GetMeta(key string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&v)
	return v
}

// GetMetadata returns the full Metadata struct.
func (s *Store) GetMetadata() Metadata {
	return Metadata{
		RepoRoot:       s.GetMeta("repo.root"),
		IndexVersion:   s.GetMeta("index.version"),
		LastIndexedAt:  s.GetMeta("last_indexed_at"),
		LastIndexedSHA: s.GetMeta("last_indexed_sha"),
		LanguageScope:  s.GetMeta("language_scope"),
		SchemaVersion:  s.GetMeta("schema_version"),
	}
}

// ──── Counts ─────────────────────────────────────────────────────────────

// FileCount returns the number of distinct file_path values in symbols.
func (s *Store) FileCount() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT file_path) FROM symbols WHERE kind = 'file'`).Scan(&n)
	return n
}

// SymbolCount returns the total number of symbols.
func (s *Store) SymbolCount() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&n)
	return n
}

// EdgeCount returns the total number of edges.
func (s *Store) EdgeCount() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&n)
	return n
}

// ──── Query Helpers ──────────────────────────────────────────────────────

// FindSymbols searches symbols by name (case-insensitive LIKE match).
func (s *Store) FindSymbols(query string) ([]Symbol, error) {
	rows, err := s.db.Query(`
		SELECT id, name, kind, file_path, line_start, line_end, signature, exported, source_hash
		FROM symbols
		WHERE name LIKE ? AND kind NOT IN ('file', 'package')
		ORDER BY name, file_path, line_start`,
		"%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// SymbolsByFile returns all symbols declared in the given file, sorted by line.
func (s *Store) SymbolsByFile(filePath string) ([]Symbol, error) {
	rows, err := s.db.Query(`
		SELECT id, name, kind, file_path, line_start, line_end, signature, exported, source_hash
		FROM symbols
		WHERE file_path = ? AND kind NOT IN ('file', 'package')
		ORDER BY line_start, name`,
		filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// GetSymbol returns a single symbol by ID, or nil if not found.
func (s *Store) GetSymbol(id string) *Symbol {
	row := s.db.QueryRow(`
		SELECT id, name, kind, file_path, line_start, line_end, signature, exported, source_hash
		FROM symbols WHERE id = ?`, id)
	var sym Symbol
	if err := row.Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.FilePath,
		&sym.LineStart, &sym.LineEnd, &sym.Signature, &sym.Exported, &sym.SourceHash); err != nil {
		return nil
	}
	return &sym
}

// ResolveSubject tries to find a symbol by exact name, file path, or symbol ID.
func (s *Store) ResolveSubject(query string) *Symbol {
	// Try exact symbol ID first.
	if sym := s.GetSymbol(query); sym != nil {
		return sym
	}
	// Try as file path → return the file symbol.
	if sym := s.GetSymbol("sym:" + query); sym != nil {
		return sym
	}
	// Try exact name match (first result).
	row := s.db.QueryRow(`
		SELECT id, name, kind, file_path, line_start, line_end, signature, exported, source_hash
		FROM symbols WHERE name = ? ORDER BY file_path, line_start LIMIT 1`, query)
	var sym Symbol
	if err := row.Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.FilePath,
		&sym.LineStart, &sym.LineEnd, &sym.Signature, &sym.Exported, &sym.SourceHash); err != nil {
		return nil
	}
	return &sym
}

// Deps returns outgoing edges from a symbol (what it depends on).
func (s *Store) Deps(symbolID string) ([]DepEdge, error) {
	rows, err := s.db.Query(`
		SELECT e.kind, e.source_id, e.file_path, e.line_number,
		       s.id, s.name, s.kind, s.file_path, s.line_start, s.line_end, s.signature, s.exported, s.source_hash
		FROM edges e
		JOIN symbols s ON e.target_id = s.id
		WHERE e.source_id = ?
		ORDER BY e.kind, s.file_path, s.line_start`,
		symbolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DepEdge
	for rows.Next() {
		var d DepEdge
		if err := rows.Scan(&d.Kind, &d.SourceID, &d.FilePath, &d.LineNumber,
			&d.Target.ID, &d.Target.Name, &d.Target.Kind, &d.Target.FilePath,
			&d.Target.LineStart, &d.Target.LineEnd, &d.Target.Signature,
			&d.Target.Exported, &d.Target.SourceHash); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// Rdeps returns incoming edges to a symbol (what depends on it).
func (s *Store) Rdeps(symbolID string) ([]RdepEdge, error) {
	rows, err := s.db.Query(`
		SELECT e.kind, e.target_id, e.file_path, e.line_number,
		       s.id, s.name, s.kind, s.file_path, s.line_start, s.line_end, s.signature, s.exported, s.source_hash
		FROM edges e
		JOIN symbols s ON e.source_id = s.id
		WHERE e.target_id = ?
		ORDER BY e.kind, s.file_path, s.line_start`,
		symbolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []RdepEdge
	for rows.Next() {
		var r RdepEdge
		if err := rows.Scan(&r.Kind, &r.TargetID, &r.FilePath, &r.LineNumber,
			&r.Source.ID, &r.Source.Name, &r.Source.Kind, &r.Source.FilePath,
			&r.Source.LineStart, &r.Source.LineEnd, &r.Source.Signature,
			&r.Source.Exported, &r.Source.SourceHash); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// SymbolsInFiles returns all non-file/non-package symbols in the given file paths.
func (s *Store) SymbolsInFiles(files []string) ([]Symbol, error) {
	if len(files) == 0 {
		return nil, nil
	}
	query := `SELECT id, name, kind, file_path, line_start, line_end, signature, exported, source_hash
		FROM symbols WHERE file_path IN (`
	args := make([]interface{}, len(files))
	for i, f := range files {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = f
	}
	query += `) AND kind NOT IN ('file', 'package') ORDER BY file_path, line_start, name`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// RdepsOfSymbols returns unique dependent symbols for a set of symbol IDs.
func (s *Store) RdepsOfSymbols(ids []string) ([]Symbol, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	query := `SELECT DISTINCT s.id, s.name, s.kind, s.file_path, s.line_start, s.line_end, s.signature, s.exported, s.source_hash
		FROM edges e
		JOIN symbols s ON e.source_id = s.id
		WHERE e.target_id IN (`
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += `) ORDER BY s.file_path, s.line_start, s.name`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

func scanSymbols(rows *sql.Rows) ([]Symbol, error) {
	var result []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.ID, &sym.Name, &sym.Kind, &sym.FilePath,
			&sym.LineStart, &sym.LineEnd, &sym.Signature, &sym.Exported, &sym.SourceHash); err != nil {
			return nil, err
		}
		result = append(result, sym)
	}
	return result, rows.Err()
}
