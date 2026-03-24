package context

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IndexResult holds the outcome of an indexing run.
type IndexResult struct {
	Warnings []string
	Files    int
	Symbols  int
	Edges    int
}

// IndexOptions controls indexing behavior.
type IndexOptions struct {
	Full bool // force full rebuild even if incremental is possible
}

// Index performs a full or incremental index of Go source files in repoRoot.
func Index(store *Store, repoRoot string, opts IndexOptions) (*IndexResult, error) {
	meta := store.GetMetadata()
	currentSHA := gitHEAD(repoRoot)

	needFull := opts.Full ||
		meta.SchemaVersion != SchemaVersion ||
		meta.LastIndexedSHA == "" ||
		meta.LastIndexedAt == ""

	var changedFiles []string
	if !needFull && currentSHA != "" && meta.LastIndexedSHA != "" {
		var err error
		changedFiles, err = gitChangedFiles(repoRoot, meta.LastIndexedSHA)
		if err != nil {
			needFull = true // fallback to full rebuild
		}
	} else {
		needFull = true
	}

	if needFull {
		return fullIndex(store, repoRoot, currentSHA)
	}
	return incrementalIndex(store, repoRoot, currentSHA, changedFiles)
}

func fullIndex(store *Store, repoRoot, sha string) (*IndexResult, error) {
	// Clear all existing data.
	if _, err := store.db.Exec(`DELETE FROM edges`); err != nil {
		return nil, err
	}
	if _, err := store.db.Exec(`DELETE FROM symbols`); err != nil {
		return nil, err
	}

	goFiles, warnings := collectGoFiles(repoRoot)
	result := &IndexResult{Warnings: warnings}

	tx, err := store.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	fset := token.NewFileSet()
	for _, relPath := range goFiles {
		absPath := filepath.Join(repoRoot, relPath)
		if err := indexFile(tx, fset, repoRoot, relPath, absPath); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("parse error: %s: %v", relPath, err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = store.SetMeta("repo.root", repoRoot)
	_ = store.SetMeta("index.version", SchemaVersion)
	_ = store.SetMeta("schema_version", SchemaVersion)
	_ = store.SetMeta("last_indexed_at", now)
	_ = store.SetMeta("last_indexed_sha", sha)
	_ = store.SetMeta("language_scope", "go")

	result.Files = store.FileCount()
	result.Symbols = store.SymbolCount()
	result.Edges = store.EdgeCount()
	return result, nil
}

func incrementalIndex(store *Store, repoRoot, sha string, changedFiles []string) (*IndexResult, error) {
	// Filter to .go files only.
	var goChanged []string
	for _, f := range changedFiles {
		if strings.HasSuffix(f, ".go") && !strings.Contains(f, "/vendor/") {
			goChanged = append(goChanged, f)
		}
	}

	result := &IndexResult{}

	if len(goChanged) > 0 {
		tx, err := store.db.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback() //nolint:errcheck

		// Remove stale symbols for changed files.
		for _, relPath := range goChanged {
			if _, err := tx.Exec(`DELETE FROM edges WHERE source_id IN (SELECT id FROM symbols WHERE file_path = ?)`, relPath); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(`DELETE FROM edges WHERE target_id IN (SELECT id FROM symbols WHERE file_path = ?)`, relPath); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(`DELETE FROM symbols WHERE file_path = ?`, relPath); err != nil {
				return nil, err
			}
		}

		fset := token.NewFileSet()
		for _, relPath := range goChanged {
			absPath := filepath.Join(repoRoot, relPath)
			if _, err := os.Stat(absPath); os.IsNotExist(err) {
				continue // file was deleted
			}
			if err := indexFile(tx, fset, repoRoot, relPath, absPath); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("parse error: %s: %v", relPath, err))
			}
		}

		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = store.SetMeta("last_indexed_at", now)
	_ = store.SetMeta("last_indexed_sha", sha)

	result.Files = store.FileCount()
	result.Symbols = store.SymbolCount()
	result.Edges = store.EdgeCount()
	return result, nil
}

// indexFile parses a single Go file and inserts symbols + edges into the tx.
func indexFile(tx *sql.Tx, fset *token.FileSet, repoRoot, relPath, absPath string) error {
	src, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(src))

	f, err := parser.ParseFile(fset, absPath, src, parser.ParseComments)
	if err != nil {
		return err
	}

	pkgName := f.Name.Name
	pkgDir := filepath.Dir(relPath)
	pkgID := "sym:" + pkgDir
	fileID := "sym:" + relPath

	// Insert package node (idempotent via OR REPLACE).
	insertSym(tx, Symbol{
		ID: pkgID, Name: pkgName, Kind: KindPackage,
		FilePath: pkgDir, Exported: true, SourceHash: hash,
	})

	// Insert file node.
	endLine := fset.Position(f.End()).Line
	insertSym(tx, Symbol{
		ID: fileID, Kind: KindFile, FilePath: relPath,
		LineStart: 1, LineEnd: endLine, SourceHash: hash,
	})

	// File belongs to package.
	insertEdge(tx, Edge{SourceID: pkgID, TargetID: fileID, Kind: EdgeDefines, FilePath: relPath, LineNumber: 1})

	// Process imports.
	for _, imp := range f.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		targetID := "sym:" + impPath
		line := fset.Position(imp.Pos()).Line
		// Create a lightweight target symbol for the import.
		insertSym(tx, Symbol{
			ID: targetID, Name: filepath.Base(impPath), Kind: KindPackage, FilePath: impPath,
		})
		insertEdge(tx, Edge{SourceID: fileID, TargetID: targetID, Kind: EdgeImports, FilePath: relPath, LineNumber: line})
	}

	// Process top-level declarations.
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			indexFuncDecl(tx, fset, relPath, fileID, d, hash)
		case *ast.GenDecl:
			indexGenDecl(tx, fset, relPath, fileID, d, hash)
		}
	}

	return nil
}

func indexFuncDecl(tx *sql.Tx, fset *token.FileSet, filePath, fileID string, fn *ast.FuncDecl, hash string) {
	name := fn.Name.Name
	kind := KindFunction
	sig := "func " + name

	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		kind = KindMethod
		recv := exprString(fn.Recv.List[0].Type)
		sig = fmt.Sprintf("func (%s) %s", recv, name)
	}

	if fn.Type.Params != nil {
		sig += "(" + fieldListString(fn.Type.Params) + ")"
	} else {
		sig += "()"
	}
	if fn.Type.Results != nil {
		results := fieldListString(fn.Type.Results)
		if strings.Contains(results, ",") {
			sig += " (" + results + ")"
		} else {
			sig += " " + results
		}
	}

	startLine := fset.Position(fn.Pos()).Line
	endLine := fset.Position(fn.End()).Line
	symID := fmt.Sprintf("sym:%s:%s", filePath, name)

	insertSym(tx, Symbol{
		ID: symID, Name: name, Kind: kind, FilePath: filePath,
		LineStart: startLine, LineEnd: endLine, Signature: sig,
		Exported: ast.IsExported(name), SourceHash: hash,
	})
	insertEdge(tx, Edge{SourceID: fileID, TargetID: symID, Kind: EdgeDefines, FilePath: filePath, LineNumber: startLine})

	// Walk function body for call references.
	if fn.Body != nil {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				// pkg.Func() or receiver.Method()
				callName := fun.Sel.Name
				line := fset.Position(fun.Pos()).Line
				targetID := fmt.Sprintf("sym:%s:%s", filePath, callName)
				insertEdge(tx, Edge{SourceID: symID, TargetID: targetID, Kind: EdgeCalls, FilePath: filePath, LineNumber: line})
			case *ast.Ident:
				callName := fun.Name
				if callName == "make" || callName == "new" || callName == "len" || callName == "cap" ||
					callName == "append" || callName == "copy" || callName == "delete" || callName == "close" ||
					callName == "panic" || callName == "recover" || callName == "print" || callName == "println" {
					return true
				}
				line := fset.Position(fun.Pos()).Line
				targetID := fmt.Sprintf("sym:%s:%s", filePath, callName)
				insertEdge(tx, Edge{SourceID: symID, TargetID: targetID, Kind: EdgeCalls, FilePath: filePath, LineNumber: line})
			}
			return true
		})
	}
}

func indexGenDecl(tx *sql.Tx, fset *token.FileSet, filePath, fileID string, gd *ast.GenDecl, hash string) {
	for _, spec := range gd.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			indexTypeSpec(tx, fset, filePath, fileID, s, hash)
		case *ast.ValueSpec:
			kind := KindVar
			if gd.Tok == token.CONST {
				kind = KindConst
			}
			for _, name := range s.Names {
				if name.Name == "_" {
					continue
				}
				startLine := fset.Position(s.Pos()).Line
				endLine := fset.Position(s.End()).Line
				symID := fmt.Sprintf("sym:%s:%s", filePath, name.Name)
				sig := kind + " " + name.Name
				if s.Type != nil {
					sig += " " + exprString(s.Type)
				}
				insertSym(tx, Symbol{
					ID: symID, Name: name.Name, Kind: kind, FilePath: filePath,
					LineStart: startLine, LineEnd: endLine, Signature: sig,
					Exported: ast.IsExported(name.Name), SourceHash: hash,
				})
				insertEdge(tx, Edge{SourceID: fileID, TargetID: symID, Kind: EdgeDefines, FilePath: filePath, LineNumber: startLine})
			}
		}
	}
}

func indexTypeSpec(tx *sql.Tx, fset *token.FileSet, filePath, fileID string, ts *ast.TypeSpec, hash string) {
	name := ts.Name.Name
	startLine := fset.Position(ts.Pos()).Line
	endLine := fset.Position(ts.End()).Line
	symID := fmt.Sprintf("sym:%s:%s", filePath, name)

	kind := KindType
	sig := "type " + name
	switch t := ts.Type.(type) {
	case *ast.StructType:
		kind = KindStruct
		sig = "type " + name + " struct"
		// Check for embedded fields.
		if t.Fields != nil {
			for _, field := range t.Fields.List {
				if len(field.Names) == 0 {
					// Embedded field.
					embName := exprString(field.Type)
					line := fset.Position(field.Pos()).Line
					targetID := fmt.Sprintf("sym:%s:%s", filePath, embName)
					insertEdge(tx, Edge{SourceID: symID, TargetID: targetID, Kind: EdgeEmbeds, FilePath: filePath, LineNumber: line})
				}
			}
		}
	case *ast.InterfaceType:
		kind = KindInterface
		sig = "type " + name + " interface"
		// Check for embedded interfaces.
		if t.Methods != nil {
			for _, m := range t.Methods.List {
				if len(m.Names) == 0 {
					embName := exprString(m.Type)
					line := fset.Position(m.Pos()).Line
					targetID := fmt.Sprintf("sym:%s:%s", filePath, embName)
					insertEdge(tx, Edge{SourceID: symID, TargetID: targetID, Kind: EdgeEmbeds, FilePath: filePath, LineNumber: line})
				}
			}
		}
	}

	insertSym(tx, Symbol{
		ID: symID, Name: name, Kind: kind, FilePath: filePath,
		LineStart: startLine, LineEnd: endLine, Signature: sig,
		Exported: ast.IsExported(name), SourceHash: hash,
	})
	insertEdge(tx, Edge{SourceID: fileID, TargetID: symID, Kind: EdgeDefines, FilePath: filePath, LineNumber: startLine})
}

// ──── Helpers ────────────────────────────────────────────────────────────

func insertSym(tx *sql.Tx, sym Symbol) {
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, _ = tx.Exec(`INSERT OR REPLACE INTO symbols
		(id, name, kind, file_path, line_start, line_end, signature, exported, source_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sym.ID, sym.Name, sym.Kind, sym.FilePath,
		sym.LineStart, sym.LineEnd, sym.Signature,
		sym.Exported, sym.SourceHash)
}

func insertEdge(tx *sql.Tx, e Edge) {
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, _ = tx.Exec(`INSERT OR IGNORE INTO edges
		(source_id, target_id, kind, file_path, line_number)
		VALUES (?, ?, ?, ?, ?)`,
		e.SourceID, e.TargetID, e.Kind, e.FilePath, e.LineNumber)
}

// collectGoFiles walks the repo and returns repo-relative paths to .go files.
func collectGoFiles(repoRoot string) (goFiles []string, warnings []string) {
	_ = filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip hidden dirs, vendor, testdata, .git.
		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "vendor" || name == "testdata" || name == "node_modules" ||
				(strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		goFiles = append(goFiles, rel)
		return nil
	})
	return
}

func gitHEAD(repoRoot string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitChangedFiles(repoRoot, baseSHA string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", baseSHA, "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// gitStagedFiles returns files staged in the index.
func gitStagedFiles(repoRoot string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprString(e.X)
	case *ast.SelectorExpr:
		return exprString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprString(e.Elt)
	case *ast.MapType:
		return "map[" + exprString(e.Key) + "]" + exprString(e.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	case *ast.Ellipsis:
		return "..." + exprString(e.Elt)
	case *ast.ChanType:
		return "chan " + exprString(e.Value)
	default:
		return "?"
	}
}

func fieldListString(fl *ast.FieldList) string {
	if fl == nil {
		return ""
	}
	var parts []string
	for _, f := range fl.List {
		typ := exprString(f.Type)
		if len(f.Names) == 0 {
			parts = append(parts, typ)
		} else {
			for _, n := range f.Names {
				parts = append(parts, n.Name+" "+typ)
			}
		}
	}
	return strings.Join(parts, ", ")
}
