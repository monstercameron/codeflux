package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	migrationNamePattern = regexp.MustCompile(`^([0-9]{6})_[a-z0-9]+(?:_[a-z0-9]+)*\.sql$`)
	secretPatterns       = []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{name: "OpenAI API key", pattern: regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`)},
		{name: "Anthropic API key", pattern: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
		{name: "GitHub token", pattern: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)},
	}
)

func runRepositoryChecks(ctx context.Context, root string) error {
	tracked, err := trackedFiles(ctx, root)
	if err != nil {
		return err
	}
	if err := checkGeneratedHeaders(root, tracked); err != nil {
		return err
	}
	if err := checkMigrationNames(root); err != nil {
		return err
	}
	if err := checkNoTrackedDatabases(root, tracked); err != nil {
		return err
	}
	if err := checkNoTrackedSecrets(root, tracked); err != nil {
		return err
	}
	if err := checkNoTrackedBrowserSource(tracked); err != nil {
		return err
	}
	if err := checkTaskStateOwnership(root, tracked); err != nil {
		return err
	}
	if err := checkInstructionFiles(root, tracked); err != nil {
		return err
	}
	if err := checkAtomDeclarations(root, tracked); err != nil {
		return err
	}
	if err := checkPackageDependencies(root, tracked); err != nil {
		return err
	}
	if err := checkArtifactPolicy(ctx, root, tracked); err != nil {
		return err
	}
	return nil
}

func trackedFiles(ctx context.Context, root string) ([]string, error) {
	command := exec.CommandContext(ctx, "git", "ls-files", "-z")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}
	parts := bytes.Split(output, []byte{0})
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			files = append(files, filepath.FromSlash(string(part)))
		}
	}
	return files, nil
}

func checkGeneratedHeaders(root string, tracked []string) error {
	for _, relative := range tracked {
		base := filepath.Base(relative)
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return err
		}
		firstLine, _, _ := bytes.Cut(content, []byte{'\n'})
		hasGeneratedHeader := bytes.HasPrefix(firstLine, []byte("// Code generated ")) &&
			bytes.Contains(firstLine, []byte(" DO NOT EDIT."))
		hasGeneratedSuffix := strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_gen.go")
		if !hasGeneratedHeader && !hasGeneratedSuffix {
			continue
		}
		if !isDeclaredGeneratedPath(filepath.ToSlash(relative)) {
			return fmt.Errorf("generated file is outside a declared path: %s", filepath.ToSlash(relative))
		}
		if !hasGeneratedHeader {
			return fmt.Errorf("generated file %s lacks the required first-line header", filepath.ToSlash(relative))
		}
	}
	return nil
}

func isDeclaredGeneratedPath(path string) bool {
	if strings.HasPrefix(path, "api/gen/") && strings.HasSuffix(path, ".pb.go") {
		return true
	}
	switch path {
	case "internal/buildinfo/versions_gen.go",
		"internal/events/registry_gen.go",
		"migrations/catalog_gen.go",
		"web/assets/manifest_gen.go":
		return true
	default:
		return false
	}
}

func checkMigrationNames(root string) error {
	entries, err := os.ReadDir(filepath.Join(root, "migrations"))
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	type migration struct {
		name   string
		number int
	}
	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		match := migrationNamePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			return fmt.Errorf("migration %s must match NNNNNN_lower_snake_case.sql", entry.Name())
		}
		number, err := strconv.Atoi(match[1])
		if err != nil {
			return fmt.Errorf("parse migration %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{name: entry.Name(), number: number})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].number < migrations[j].number
	})
	for index, migration := range migrations {
		if index > 0 && migration.number == migrations[index-1].number {
			return fmt.Errorf(
				"migration number %06d is duplicated by %s and %s",
				migration.number,
				migrations[index-1].name,
				migration.name,
			)
		}
		if migration.number != index {
			return fmt.Errorf(
				"migration %s has number %06d; want contiguous number %06d",
				migration.name,
				migration.number,
				index,
			)
		}
	}
	return nil
}

func checkNoTrackedDatabases(root string, tracked []string) error {
	for _, relative := range tracked {
		lower := strings.ToLower(relative)
		switch {
		case strings.HasSuffix(lower, ".db"),
			strings.HasSuffix(lower, ".db-shm"),
			strings.HasSuffix(lower, ".db-wal"),
			strings.HasSuffix(lower, ".sqlite"),
			strings.HasSuffix(lower, ".sqlite3"):
			return fmt.Errorf("tracked local database is forbidden: %s", filepath.ToSlash(relative))
		}
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return err
		}
		if bytes.HasPrefix(content, []byte("SQLite format 3\x00")) {
			return fmt.Errorf("tracked SQLite content is forbidden: %s", filepath.ToSlash(relative))
		}
	}
	return nil
}

func checkNoTrackedSecrets(root string, tracked []string) error {
	for _, relative := range tracked {
		content, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return err
		}
		if bytes.IndexByte(content, 0) >= 0 {
			continue
		}
		for _, secret := range secretPatterns {
			if location := secret.pattern.FindIndex(content); location != nil {
				line := 1 + bytes.Count(content[:location[0]], []byte{'\n'})
				return fmt.Errorf(
					"%s resembles %s at line %d",
					filepath.ToSlash(relative),
					secret.name,
					line,
				)
			}
		}
	}
	return nil
}

func checkNoTrackedBrowserSource(tracked []string) error {
	forbidden := map[string]struct{}{
		".cjs":  {},
		".css":  {},
		".htm":  {},
		".html": {},
		".js":   {},
		".jsx":  {},
		".mjs":  {},
		".ts":   {},
		".tsx":  {},
	}
	for _, relative := range tracked {
		extension := strings.ToLower(filepath.Ext(relative))
		if _, found := forbidden[extension]; found {
			return fmt.Errorf(
				"tracked browser-language source is forbidden; use GoWebComponents v5 from Go: %s",
				filepath.ToSlash(relative),
			)
		}
	}
	return nil
}

func checkTaskStateOwnership(root string, tracked []string) error {
	const canonicalPath = "internal/domain/states.go"
	definitions := make([]string, 0, 1)
	files := token.NewFileSet()
	for _, relative := range tracked {
		if strings.ToLower(filepath.Ext(relative)) != ".go" {
			continue
		}
		sourcePath := filepath.Join(root, relative)
		file, err := parser.ParseFile(files, sourcePath, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s for task-state ownership: %w", filepath.ToSlash(relative), err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if ok && spec.Name.Name == "TaskState" {
				definitions = append(definitions, filepath.ToSlash(relative))
			}
			return true
		})
	}
	if len(definitions) != 1 || definitions[0] != canonicalPath {
		return fmt.Errorf(
			"TaskState must have exactly one definition in %s; found %s",
			canonicalPath,
			strings.Join(definitions, ", "),
		)
	}
	return nil
}

func createArtifactTemp(root, pattern string) (string, error) {
	tempRoot := filepath.Join(root, ".artifacts", "tmp")
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		return "", fmt.Errorf("create artifact temp root: %w", err)
	}
	path, err := os.MkdirTemp(tempRoot, pattern)
	if err != nil {
		return "", fmt.Errorf("create artifact temp child: %w", err)
	}
	return path, nil
}

func removeArtifactChild(root, target string) error {
	artifactRoot, err := filepath.Abs(filepath.Join(root, ".artifacts"))
	if err != nil {
		return err
	}
	resolved, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(artifactRoot, resolved)
	if err != nil {
		return err
	}
	if relative == "." || relative == "" || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refuse cleanup outside a child of .artifacts: %s", resolved)
	}
	return os.RemoveAll(resolved)
}

func compareDirectoryTrees(expectedRoot, actualRoot string) error {
	expected, err := readFileTree(expectedRoot)
	if err != nil {
		return fmt.Errorf("read committed generated tree: %w", err)
	}
	actual, err := readFileTree(actualRoot)
	if err != nil {
		return fmt.Errorf("read regenerated tree: %w", err)
	}
	for path, expectedContent := range expected {
		actualContent, ok := actual[path]
		if !ok {
			return fmt.Errorf("regeneration omitted %s", path)
		}
		if !bytes.Equal(expectedContent, actualContent) {
			return fmt.Errorf("generated content differs for %s", path)
		}
		delete(actual, path)
	}
	if len(actual) != 0 {
		paths := make([]string, 0, len(actual))
		for path := range actual {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		return fmt.Errorf("regeneration added unexpected files: %s", strings.Join(paths, ", "))
	}
	return nil
}

func readFileTree(root string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = content
		return nil
	})
	return files, err
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
