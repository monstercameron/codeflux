package gitwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// DiffAttribution links one changed file to durable task and validation facts.
type DiffAttribution struct {
	EventIDs      []domain.EventID
	ValidationIDs []domain.ValidationID
}

// TaskDiffQuery declares approved scope and known durable attribution.
type TaskDiffQuery struct {
	TaskID            domain.TaskID
	ApprovedPaths     []string
	AttributionByPath map[string]DiffAttribution
}

// DiffFile is one repository-relative changed-file summary.
type DiffFile struct {
	Path                 string
	PreviousPath         string
	AddedLines           int
	DeletedLines         int
	Category             string
	Binary               bool
	FormattingChurn      bool
	OutsideApprovedScope bool
	Attribution          DiffAttribution
}

// TaskDiff carries exact review source plus a non-substitutive summary.
type TaskDiff struct {
	Identity     string
	TaskID       domain.TaskID
	BaseRevision string
	HeadRevision string
	UnifiedDiff  string
	Files        []DiffFile
	FilesChanged int
	Insertions   int
	Deletions    int
	Summary      string
}

type environmentCommandRunner interface {
	RunEnvironment(
		context.Context,
		string,
		string,
		map[string]string,
		...string,
	) (CommandResult, error)
}

// GetTaskDiff returns a deterministic repository-relative diff against the
// recorded base, including untracked files.
func (service *Service) GetTaskDiff(
	ctx context.Context,
	query TaskDiffQuery,
) (TaskDiff, error) {
	verification, err := service.VerifyTaskWorktree(ctx, query.TaskID)
	if err != nil {
		return TaskDiff{}, err
	}
	binding := verification.Binding
	environment, cleanup, err := service.alternateDiffIndex(ctx, binding.WorktreePath)
	if err != nil {
		return TaskDiff{}, err
	}
	defer cleanup()
	trackedResult, err := service.runWithEnvironment(
		ctx,
		binding.WorktreePath,
		"git",
		environment,
		"diff",
		"--binary",
		"--no-color",
		"--no-ext-diff",
		"-M",
		binding.BaseRevision,
		"--",
	)
	if err != nil {
		return TaskDiff{}, fmt.Errorf("build tracked task diff: %w", err)
	}
	numstatResult, err := service.runWithEnvironment(
		ctx,
		binding.WorktreePath,
		"git",
		environment,
		"diff",
		"--numstat",
		"-z",
		"-M",
		binding.BaseRevision,
		"--",
	)
	if err != nil {
		return TaskDiff{}, fmt.Errorf("count tracked task diff: %w", err)
	}
	files, err := parseNumstat(numstatResult.Stdout)
	if err != nil {
		return TaskDiff{}, err
	}
	slices.SortFunc(files, func(left, right DiffFile) int {
		return strings.Compare(left.Path, right.Path)
	})
	for index := range files {
		file := &files[index]
		if file.Category == "" {
			content, _ := os.ReadFile(filepath.Join(
				binding.WorktreePath,
				filepath.FromSlash(file.Path),
			))
			if len(content) == 0 && file.DeletedLines != 0 {
				baseContent, showErr := service.runner.Run(
					ctx,
					binding.WorktreePath,
					"git",
					"show",
					binding.BaseRevision+":"+file.Path,
				)
				if showErr == nil {
					content = baseContent.Stdout
				}
			}
			file.Category = classifyDiffPath(file.Path, content)
		}
		file.FormattingChurn = file.AddedLines+file.DeletedLines >= 200
		file.OutsideApprovedScope = outsideApprovedScope(file.Path, query.ApprovedPaths)
		file.Attribution = query.AttributionByPath[file.Path]
	}
	diff := TaskDiff{
		TaskID: query.TaskID, BaseRevision: binding.BaseRevision,
		HeadRevision: binding.HeadRevision, UnifiedDiff: string(trackedResult.Stdout),
		Files: files, FilesChanged: len(files),
	}
	for _, file := range files {
		diff.Insertions += file.AddedLines
		diff.Deletions += file.DeletedLines
	}
	diff.Summary = summarizeDiff(diff)
	diff.Identity = computeDiffIdentity(diff)
	return diff, nil
}

// ComputeDiffIdentity returns the exact revision-and-content binding.
func (service *Service) ComputeDiffIdentity(
	ctx context.Context,
	taskID domain.TaskID,
) (string, error) {
	diff, err := service.GetTaskDiff(ctx, TaskDiffQuery{TaskID: taskID})
	if err != nil {
		return "", err
	}
	return diff.Identity, nil
}

func parseNumstat(value []byte) ([]DiffFile, error) {
	if len(value) == 0 {
		return nil, nil
	}
	records := bytes.Split(value, []byte{0})
	files := make([]DiffFile, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		line := string(record)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return nil, errors.New("git returned malformed diff statistics")
		}
		file := DiffFile{}
		if fields[2] == "" {
			if index+2 >= len(records) {
				return nil, errors.New("git returned malformed rename statistics")
			}
			file.PreviousPath = filepath.ToSlash(string(records[index+1]))
			file.Path = filepath.ToSlash(string(records[index+2]))
			index += 2
		} else {
			file.Path = filepath.ToSlash(fields[2])
		}
		if file.Path == "" {
			return nil, errors.New("git returned an empty diff path")
		}
		if fields[0] == "-" || fields[1] == "-" {
			file.Binary = true
		} else {
			added, addErr := strconv.Atoi(fields[0])
			deleted, deleteErr := strconv.Atoi(fields[1])
			if addErr != nil || deleteErr != nil || added < 0 || deleted < 0 {
				return nil, errors.New("git returned invalid diff counts")
			}
			file.AddedLines = added
			file.DeletedLines = deleted
		}
		files = append(files, file)
	}
	return files, nil
}

func classifyDiffPath(filePath string, content []byte) string {
	base := filepath.Base(filePath)
	lower := strings.ToLower(filepath.ToSlash(filePath))
	switch {
	case strings.Contains(lower, "/vendor/") || strings.HasPrefix(lower, "vendor/"):
		return "dependency"
	case strings.HasSuffix(base, "_test.go"):
		return "test"
	case bytes.Contains(content, []byte("Code generated")):
		return "generated"
	case strings.HasSuffix(base, ".go"):
		return "source"
	case base == "go.mod" || base == "go.sum" || base == "go.work" ||
		base == "Makefile" || strings.HasSuffix(base, ".yml") ||
		strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".toml"):
		return "configuration"
	default:
		return "other"
	}
}

func outsideApprovedScope(filePath string, approved []string) bool {
	if len(approved) == 0 {
		return false
	}
	for _, allowed := range approved {
		allowed = filepath.ToSlash(strings.TrimSpace(allowed))
		if filePath == allowed ||
			(strings.HasSuffix(allowed, "/") && strings.HasPrefix(filePath, allowed)) {
			return false
		}
	}
	return true
}

func summarizeDiff(diff TaskDiff) string {
	categories := make(map[string]int)
	var outside, churn, binary int
	for _, file := range diff.Files {
		categories[file.Category]++
		if file.OutsideApprovedScope {
			outside++
		}
		if file.FormattingChurn {
			churn++
		}
		if file.Binary {
			binary++
		}
	}
	names := make([]string, 0, len(categories))
	for name := range categories {
		names = append(names, name)
	}
	slices.Sort(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%d %s", categories[name], name))
	}
	return fmt.Sprintf(
		"%d files changed (+%d/-%d): %s; %d outside scope, %d formatting-churn, %d binary. Review the unified diff.",
		diff.FilesChanged,
		diff.Insertions,
		diff.Deletions,
		strings.Join(parts, ", "),
		outside,
		churn,
		binary,
	)
}

func computeDiffIdentity(diff TaskDiff) string {
	hash := sha256.New()
	hash.Write([]byte(diff.TaskID.String()))
	hash.Write([]byte{0})
	hash.Write([]byte(diff.BaseRevision))
	hash.Write([]byte{0})
	hash.Write([]byte(diff.HeadRevision))
	hash.Write([]byte{0})
	hash.Write([]byte(diff.UnifiedDiff))
	for _, file := range diff.Files {
		fmt.Fprintf(
			hash,
			"\x00%s\x00%d\x00%d\x00%t\x00%s",
			file.Path,
			file.AddedLines,
			file.DeletedLines,
			file.Binary,
			file.Category,
		)
		hash.Write([]byte{0})
		hash.Write([]byte(file.PreviousPath))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (service *Service) alternateDiffIndex(
	ctx context.Context,
	worktreePath string,
) (map[string]string, func(), error) {
	runner, supported := service.runner.(environmentCommandRunner)
	if !supported {
		return nil, func() {}, errors.New("command runner does not support isolated diff indexes")
	}
	untrackedResult, err := service.runner.Run(
		ctx,
		worktreePath,
		"git",
		"ls-files",
		"--others",
		"--exclude-standard",
		"-z",
	)
	if err != nil {
		return nil, func() {}, fmt.Errorf("list untracked task files: %w", err)
	}
	if len(untrackedResult.Stdout) == 0 {
		return nil, func() {}, nil
	}
	indexPath, err := service.gitText(ctx, worktreePath, "rev-parse", "--git-path", "index")
	if err != nil {
		return nil, func() {}, fmt.Errorf("resolve task Git index: %w", err)
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(worktreePath, indexPath)
	}
	indexContent, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, func() {}, fmt.Errorf("read task Git index: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(worktreePath), ".codeflux-diff-index-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create isolated diff index: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = os.Remove(temporaryPath)
	}
	if _, err := temporary.Write(indexContent); err != nil {
		_ = temporary.Close()
		cleanup()
		return nil, func() {}, fmt.Errorf("copy task Git index: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("close isolated diff index: %w", err)
	}
	environment := map[string]string{"GIT_INDEX_FILE": temporaryPath}
	paths := splitNULPaths(untrackedResult.Stdout)
	arguments := append([]string{"add", "-N", "--"}, paths...)
	if _, err := runner.RunEnvironment(
		ctx,
		worktreePath,
		"git",
		environment,
		arguments...,
	); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("prepare isolated diff index: %w", err)
	}
	return environment, cleanup, nil
}

func (service *Service) runWithEnvironment(
	ctx context.Context,
	directory string,
	executable string,
	environment map[string]string,
	arguments ...string,
) (CommandResult, error) {
	if len(environment) == 0 {
		return service.runner.Run(ctx, directory, executable, arguments...)
	}
	runner, ok := service.runner.(environmentCommandRunner)
	if !ok {
		return CommandResult{}, errors.New("command runner does not support an environment overlay")
	}
	return runner.RunEnvironment(ctx, directory, executable, environment, arguments...)
}

func splitNULPaths(value []byte) []string {
	fields := bytes.Split(value, []byte{0})
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) != 0 {
			paths = append(paths, string(field))
		}
	}
	return paths
}
