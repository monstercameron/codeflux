package retrieval

import (
	"bufio"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestM21_089_NoSeparateVectorDatabaseIsImported is the M21-089 (DEFER)
// verification: "Do not add a separate vector database." docs/plan.md §1
// requires "store all Codeflux-managed atoms, graphs, vectors, evidence, and
// memory in one local SQLite database rather than artifact files." This test
// makes that a checked, automated fact rather than an intention: it fails if
// a known vector-database/ANN-search client library is ever added to go.mod,
// fails if one is ever imported by any tracked .go file even without a
// go.mod change (for example a vendored copy or a replace directive), and
// fails if a SQLite vector-search virtual-table extension (sqlite-vec,
// sqlite-vss, or equivalent) is ever loaded from a migration, which would
// bolt a separate vector index onto the one SQLite database rather than
// storing vectors as the plain BLOB columns migrations 000025/000026 already
// use (memory_artifact_embeddings.vector, atom_documentation_embeddings.vector).
//
// The deny-list below is necessarily a named, reviewable set rather than a
// provably exhaustive one (a sufficiently obscure or renamed package could
// in principle evade a substring match); it is the concrete, automated
// backstop M21-089 asks for, not a claim that no other check is ever
// needed. Today (2026-08-01) it finds nothing, which this test also
// verifies by confirming the go.mod module list is non-empty (so an empty
// deny-list match is not merely because the parser silently found no
// requires at all).
func TestM21_089_NoSeparateVectorDatabaseIsImported(t *testing.T) {
	root := repositoryRootForTest(t)

	modulePaths := parseGoModRequirePaths(t, filepath.Join(root, "go.mod"))
	if len(modulePaths) == 0 {
		t.Fatal("parsed zero module paths from go.mod; the parser itself is broken, this is not evidence of absence")
	}
	for _, path := range modulePaths {
		if deniedVectorDatabaseModule(path) {
			t.Fatalf("go.mod requires %q, which is a known vector-database/ANN-search client library; M21-089 forbids adding a separate vector database", path)
		}
	}

	importedPaths := map[string]string{} // import path -> the file that imports it
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative := filepath.ToSlash(mustRel(t, root, path))
		if info.IsDir() {
			switch info.Name() {
			case ".git", ".artifacts", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(relative, ".go") || strings.HasPrefix(relative, "api/gen/") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), relative, source, parser.ImportsOnly)
		if err != nil {
			// A file this test cannot parse is not this test's concern
			// (build/vet already enforce that every tracked .go file
			// parses); skip rather than fail the boundary check on an
			// unrelated syntax issue.
			return nil
		}
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				continue
			}
			if _, already := importedPaths[value]; !already {
				importedPaths[value] = relative
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	for path, file := range importedPaths {
		if deniedVectorDatabaseModule(path) {
			t.Fatalf("%s imports %q, a known vector-database/ANN-search client library; M21-089 forbids adding a separate vector database", file, path)
		}
	}

	assertNoVectorSearchVirtualTableExtension(t, root)
}

// deniedVectorDatabaseModule reports whether path names a known, purpose-
// built vector-database or approximate-nearest-neighbor search client
// library. Ordinary SQL/storage clients (including modernc.org/sqlite, the
// one local database this prototype already uses) are never denied.
func deniedVectorDatabaseModule(path string) bool {
	denied := []string{
		"github.com/qdrant/",
		"github.com/weaviate/",
		"github.com/pinecone-io/",
		"github.com/milvus-io/",
		"github.com/zilliztech/",
		"github.com/chroma-core/",
		"github.com/philippgille/chromem-go",
		"github.com/amikos-tech/chroma-go",
		"github.com/pgvector/pgvector-go",
		"github.com/asg017/sqlite-vec",
		"github.com/asg017/sqlite-vss",
		"github.com/nmslib/hnswlib",
		"github.com/spotify/annoy",
		"github.com/facebookresearch/faiss",
		"github.com/DataIntelligenceCrew/go-faiss",
		"github.com/RediSearch/redisearch-go",
		"github.com/typesense/typesense-go",
		"github.com/vdaas/vald",
		"github.com/marqo-ai/",
		"go.qdrant.tech/",
	}
	for _, prefix := range denied {
		if strings.HasPrefix(path, prefix) || path == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	return false
}

// parseGoModRequirePaths extracts every module path named in go.mod's
// require block(s) (both grouped `require (...)` and single-line `require
// x y` forms), ignoring the module's own declared identity and version
// numbers/comments.
func parseGoModRequirePaths(t *testing.T, goModPath string) []string {
	t.Helper()
	file, err := os.Open(goModPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var paths []string
	inRequireBlock := false
	requireLine := regexp.MustCompile(`^\s*require\s+(\S+)\s+v\S+`)
	groupedLine := regexp.MustCompile(`^\s*(\S+)\s+v\S+`)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "require ("):
			inRequireBlock = true
			continue
		case inRequireBlock && trimmed == ")":
			inRequireBlock = false
			continue
		case inRequireBlock:
			if match := groupedLine.FindStringSubmatch(line); match != nil {
				paths = append(paths, match[1])
			}
		default:
			if match := requireLine.FindStringSubmatch(line); match != nil {
				paths = append(paths, match[1])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return paths
}

// vectorSearchVirtualTableModules names known SQLite virtual-table modules
// that implement vector/ANN search as a loadable SQLite extension. Using one
// would bolt a separate vector index engine onto the database rather than
// storing vectors as plain BLOB columns, which is what M21-089 forbids even
// though it would technically remain "one SQLite file."
var vectorSearchVirtualTableModules = []string{"vec0", "vss0", "USING vec", "USING vss"}

// assertNoVectorSearchVirtualTableExtension scans every tracked migration
// source for a CREATE VIRTUAL TABLE statement naming a known vector-search
// extension module.
func assertNoVectorSearchVirtualTableExtension(t *testing.T, root string) {
	t.Helper()
	migrationsDir := filepath.Join(root, "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		upper := strings.ToUpper(string(content))
		for _, module := range vectorSearchVirtualTableModules {
			if strings.Contains(upper, strings.ToUpper(module)) {
				t.Fatalf("migration %s appears to load a vector-search virtual-table extension (%q); M21-089 requires vectors stored as plain columns in the one SQLite database, not a separate vector index engine", entry.Name(), module)
			}
		}
	}
}

func mustRel(t *testing.T, base, target string) string {
	t.Helper()
	relative, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatal(err)
	}
	return relative
}

// repositoryRootForTest resolves the repository root from this test file's
// own source location (internal/retrieval/<file>.go is two directories
// below the root), mirroring the runtime.Caller(0) pattern
// internal/domain/policy_test.go already uses for a source-relative check.
func repositoryRootForTest(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved repository root %q does not contain go.mod: %v", root, err)
	}
	return root
}
