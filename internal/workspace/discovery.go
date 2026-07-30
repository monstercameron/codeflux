package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	defaultCommandOutputLimit = 4 << 20
	maxDiscoveryFiles         = 100_000
	maxLFSFiles               = 10_000
)

var (
	ErrPathNotFound   = errors.New("repository path does not exist")
	ErrPathNotDir     = errors.New("repository path is not a directory")
	ErrNotGit         = errors.New("path is not inside a Git repository")
	ErrInvalidGitRoot = errors.New("git returned an unsafe repository root")
	ErrNoRevision     = errors.New("repository has no current revision")
	ErrOutputLimit    = errors.New("command output exceeded its limit")
)

// CommandRunner is the inward, bounded process port used for read-only
// repository discovery. Callers choose the executable and argument array;
// repository content is never interpreted as a command.
type CommandRunner interface {
	Run(context.Context, string, string, ...string) (CommandResult, error)
}

type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ExecRunner executes bounded argument-array commands without a shell.
type ExecRunner struct {
	MaxOutputBytes int
}

func (runner ExecRunner) Run(
	ctx context.Context,
	directory string,
	executable string,
	arguments ...string,
) (CommandResult, error) {
	limit := runner.MaxOutputBytes
	if limit <= 0 {
		limit = defaultCommandOutputLimit
	}
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	command.Env = networkDisabledEnvironment()
	stdout := &limitedBuffer{remaining: limit}
	stderr := &limitedBuffer{remaining: limit}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	result := CommandResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
	}
	if stdout.exceeded || stderr.exceeded {
		return result, ErrOutputLimit
	}
	return result, err
}

type Remote struct {
	Name string
	URL  string
}

type PathState struct {
	Path     string
	Index    byte
	Worktree byte
}

type RepositorySnapshot struct {
	CanonicalRoot       string
	SelectedPath        string
	GitIdentity         string
	HeadRevision        string
	Branch              string
	Detached            bool
	Dirty               bool
	Conflicted          bool
	OperationStates     []string
	Remotes             []Remote
	ChangedPaths        []PathState
	UntrackedPaths      []string
	IgnoredPaths        []string
	Submodules          []string
	SubmodulesSupported bool
	NestedRepositories  []string
	LFSPointers         []string
	Warnings            []string
}

// DiscoverRepository resolves, validates, and inspects one user-selected path
// without modifying the repository or fetching external content.
func DiscoverRepository(
	ctx context.Context,
	selectedPath string,
	runner CommandRunner,
) (RepositorySnapshot, error) {
	if runner == nil {
		return RepositorySnapshot{}, errors.New("command runner must not be nil")
	}
	selected, err := canonicalizeDirectory(selectedPath)
	if err != nil {
		return RepositorySnapshot{}, err
	}
	root, err := resolveGitRoot(ctx, selected, runner)
	if err != nil {
		return RepositorySnapshot{}, err
	}
	snapshot := RepositorySnapshot{
		CanonicalRoot: root,
		SelectedPath:  selected,
	}
	if err := inspectGit(ctx, runner, &snapshot); err != nil {
		return RepositorySnapshot{}, err
	}
	if err := inspectRepositoryFiles(ctx, runner, &snapshot); err != nil {
		return RepositorySnapshot{}, err
	}
	snapshot.Warnings = buildWarnings(snapshot)
	return snapshot, nil
}

func canonicalizeDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", ErrPathNotFound
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	info, err := os.Stat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrPathNotFound
	}
	if err != nil {
		return "", fmt.Errorf("inspect repository path: %w", err)
	}
	if !info.IsDir() {
		return "", ErrPathNotDir
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve repository path links: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("canonicalize repository path: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func resolveGitRoot(ctx context.Context, selected string, runner CommandRunner) (string, error) {
	result, err := runner.Run(ctx, selected, "git", "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotGit, boundedDiagnostic(result.Stderr))
	}
	rootText := strings.TrimSpace(string(result.Stdout))
	root, err := canonicalizeDirectory(rootText)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidGitRoot, err)
	}
	relative, err := filepath.Rel(root, selected)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidGitRoot
	}
	return root, nil
}

func inspectGit(ctx context.Context, runner CommandRunner, snapshot *RepositorySnapshot) error {
	head, err := runGitText(ctx, runner, snapshot.CanonicalRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return ErrNoRevision
	}
	if !validRevision(head) {
		return fmt.Errorf("%w: invalid HEAD", ErrNoRevision)
	}
	snapshot.HeadRevision = head

	branch, err := runGitText(ctx, runner, snapshot.CanonicalRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		snapshot.Detached = true
		snapshot.Branch = ""
	} else {
		snapshot.Branch = branch
	}
	rootCommit, err := runGitText(
		ctx,
		runner,
		snapshot.CanonicalRoot,
		"rev-list",
		"--max-parents=0",
		"--reverse",
		"HEAD",
	)
	if err != nil || rootCommit == "" {
		return ErrNoRevision
	}
	firstRoot, _, _ := strings.Cut(rootCommit, "\n")
	digest := sha256.Sum256([]byte(firstRoot))
	snapshot.GitIdentity = "git-root-sha256:" + hex.EncodeToString(digest[:])

	remotes, err := readRemotes(ctx, runner, snapshot.CanonicalRoot)
	if err != nil {
		return err
	}
	snapshot.Remotes = remotes
	status, err := runner.Run(
		ctx,
		snapshot.CanonicalRoot,
		"git",
		"status",
		"--porcelain=v2",
		"--untracked-files=all",
		"--ignored=matching",
		"-z",
	)
	if err != nil {
		return fmt.Errorf("inspect Git status: %w", err)
	}
	parseStatus(status.Stdout, snapshot)
	operations, err := operationStates(ctx, runner, snapshot.CanonicalRoot)
	if err != nil {
		return err
	}
	snapshot.OperationStates = operations
	return nil
}

func inspectRepositoryFiles(
	ctx context.Context,
	runner CommandRunner,
	snapshot *RepositorySnapshot,
) error {
	submodules, err := readSubmodules(ctx, runner, snapshot.CanonicalRoot)
	if err != nil {
		return err
	}
	snapshot.Submodules = submodules
	snapshot.SubmodulesSupported = false

	nested, err := findNestedRepositories(snapshot.CanonicalRoot)
	if err != nil {
		return err
	}
	snapshot.NestedRepositories = nested

	tracked, err := runner.Run(ctx, snapshot.CanonicalRoot, "git", "ls-files", "-z")
	if err != nil {
		return fmt.Errorf("list tracked paths: %w", err)
	}
	snapshot.LFSPointers = findLFSPointers(snapshot.CanonicalRoot, splitNUL(tracked.Stdout))
	return nil
}

func readRemotes(ctx context.Context, runner CommandRunner, root string) ([]Remote, error) {
	namesResult, err := runner.Run(ctx, root, "git", "remote")
	if err != nil {
		return nil, fmt.Errorf("list Git remotes: %w", err)
	}
	var remotes []Remote
	for _, name := range strings.Fields(string(namesResult.Stdout)) {
		urlsResult, runErr := runner.Run(ctx, root, "git", "remote", "get-url", "--all", name)
		if runErr != nil {
			return nil, fmt.Errorf("read Git remote %q: %w", name, runErr)
		}
		for _, remoteURL := range strings.Split(strings.TrimSpace(string(urlsResult.Stdout)), "\n") {
			if remoteURL != "" {
				remotes = append(remotes, Remote{Name: name, URL: sanitizeRemoteURL(remoteURL)})
			}
		}
	}
	slices.SortFunc(remotes, func(left, right Remote) int {
		if compared := strings.Compare(left.Name, right.Name); compared != 0 {
			return compared
		}
		return strings.Compare(left.URL, right.URL)
	})
	return remotes, nil
}

func sanitizeRemoteURL(value string) string {
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme != "" && parsed.User != nil {
		parsed.User = url.User("redacted")
		return parsed.String()
	}
	return value
}

func parseStatus(output []byte, snapshot *RepositorySnapshot) {
	records := splitNUL(output)
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" || record[0] == '#' {
			continue
		}
		switch record[0] {
		case '?':
			snapshot.UntrackedPaths = append(snapshot.UntrackedPaths, record[2:])
			snapshot.Dirty = true
		case '!':
			snapshot.IgnoredPaths = append(snapshot.IgnoredPaths, record[2:])
		case '1':
			fields := strings.SplitN(record, " ", 9)
			appendChangedStatus(fields, 8, snapshot)
		case '2':
			fields := strings.SplitN(record, " ", 10)
			appendChangedStatus(fields, 9, snapshot)
			if index+1 < len(records) {
				index++
			}
		case 'u':
			fields := strings.SplitN(record, " ", 11)
			appendChangedStatus(fields, 10, snapshot)
			snapshot.Conflicted = true
		}
	}
	slices.Sort(snapshot.UntrackedPaths)
	slices.Sort(snapshot.IgnoredPaths)
	slices.SortFunc(snapshot.ChangedPaths, func(left, right PathState) int {
		return strings.Compare(left.Path, right.Path)
	})
}

func appendChangedStatus(fields []string, pathIndex int, snapshot *RepositorySnapshot) {
	if len(fields) <= pathIndex || len(fields[1]) != 2 {
		return
	}
	state := PathState{Path: fields[pathIndex], Index: fields[1][0], Worktree: fields[1][1]}
	snapshot.ChangedPaths = append(snapshot.ChangedPaths, state)
	snapshot.Dirty = true
	if state.Index == 'U' || state.Worktree == 'U' {
		snapshot.Conflicted = true
	}
}

func operationStates(ctx context.Context, runner CommandRunner, root string) ([]string, error) {
	gitDirectory, err := runGitText(ctx, runner, root, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return nil, fmt.Errorf("resolve Git metadata: %w", err)
	}
	gitDirectory, err = filepath.Abs(gitDirectory)
	if err != nil {
		return nil, fmt.Errorf("canonicalize Git metadata: %w", err)
	}
	candidates := []struct {
		name string
		path string
	}{
		{name: "merge", path: "MERGE_HEAD"},
		{name: "rebase", path: "rebase-merge"},
		{name: "rebase", path: "rebase-apply"},
		{name: "cherry-pick", path: "CHERRY_PICK_HEAD"},
		{name: "bisect", path: "BISECT_LOG"},
	}
	var states []string
	for _, candidate := range candidates {
		if _, statErr := os.Lstat(filepath.Join(gitDirectory, candidate.path)); statErr == nil &&
			!slices.Contains(states, candidate.name) {
			states = append(states, candidate.name)
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect Git operation state: %w", statErr)
		}
	}
	return states, nil
}

func readSubmodules(ctx context.Context, runner CommandRunner, root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, ".gitmodules")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect .gitmodules: %w", err)
	}
	result, err := runner.Run(
		ctx,
		root,
		"git",
		"config",
		"--file",
		".gitmodules",
		"--get-regexp",
		`^submodule\..*\.path$`,
	)
	if err != nil && result.ExitCode != 1 {
		return nil, fmt.Errorf("inspect submodules: %w", err)
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		_, path, found := strings.Cut(line, " ")
		if found && safeRelativePath(path) {
			paths = append(paths, filepath.ToSlash(filepath.Clean(path)))
		}
	}
	slices.Sort(paths)
	return paths, nil
}

func findNestedRepositories(root string) ([]string, error) {
	var (
		found   []string
		visited int
	)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > maxDiscoveryFiles {
			return ErrOutputLimit
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == ".git" {
			return filepath.SkipDir
		}
		if relative != "." && entry.Name() == ".git" {
			parent := filepath.ToSlash(filepath.Dir(relative))
			found = append(found, parent)
			if entry.IsDir() {
				return filepath.SkipDir
			}
		}
		if entry.Type()&os.ModeSymlink != 0 && entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan nested repositories: %w", err)
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}

func findLFSPointers(root string, paths []string) []string {
	var found []string
	for index, relative := range paths {
		if index >= maxLFSFiles || !safeRelativePath(relative) {
			break
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		buffer := make([]byte, 256)
		count, _ := io.ReadFull(file, buffer)
		_ = file.Close()
		content := buffer[:count]
		if bytes.HasPrefix(content, []byte("version https://git-lfs.github.com/spec/v1\n")) &&
			bytes.Contains(content, []byte("\noid sha256:")) {
			found = append(found, filepath.ToSlash(relative))
		}
	}
	slices.Sort(found)
	return found
}

func buildWarnings(snapshot RepositorySnapshot) []string {
	var warnings []string
	if snapshot.Dirty {
		warnings = append(warnings, "dirty-worktree")
	}
	if snapshot.Detached {
		warnings = append(warnings, "detached-head")
	}
	if snapshot.Conflicted {
		warnings = append(warnings, "unresolved-conflicts")
	}
	for _, state := range snapshot.OperationStates {
		warnings = append(warnings, "git-operation:"+state)
	}
	if len(snapshot.Submodules) != 0 {
		warnings = append(warnings, "submodules-unsupported")
	}
	if len(snapshot.NestedRepositories) != 0 {
		warnings = append(warnings, "nested-repositories")
	}
	if len(snapshot.LFSPointers) != 0 {
		warnings = append(warnings, "git-lfs-content-not-fetched")
	}
	if len(snapshot.IgnoredPaths) != 0 {
		warnings = append(warnings, "ignored-files-observed")
	}
	return warnings
}

func runGitText(
	ctx context.Context,
	runner CommandRunner,
	root string,
	arguments ...string,
) (string, error) {
	result, err := runner.Run(ctx, root, "git", arguments...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func validRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func safeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	return cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func splitNUL(value []byte) []string {
	parts := bytes.Split(value, []byte{0})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			result = append(result, string(part))
		}
	}
	return result
}

func boundedDiagnostic(value []byte) string {
	const limit = 512
	value = bytes.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit]
	}
	return string(value)
}

func networkDisabledEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "GOPROXY") || strings.EqualFold(key, "GOSUMDB") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "GOPROXY=off", "GOSUMDB=off")
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	exceeded  bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	if len(value) > buffer.remaining {
		value = value[:max(0, buffer.remaining)]
		buffer.exceeded = true
	}
	buffer.remaining -= len(value)
	_, _ = buffer.buffer.Write(value)
	return original, nil
}

func (buffer *limitedBuffer) Bytes() []byte {
	return buffer.buffer.Bytes()
}
