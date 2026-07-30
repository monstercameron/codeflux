package gitwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

const (
	maximumEditBatchMutations = 100
	broadDeleteFileCount      = 10
	largeDeleteBytes          = 256 << 10
)

// MutationOperation identifies one explicit filesystem mutation.
type MutationOperation string

const (
	MutationCreate MutationOperation = "create"
	MutationUpdate MutationOperation = "update"
	MutationRename MutationOperation = "rename"
	MutationDelete MutationOperation = "delete"
)

// FileMutation carries an exact existence or content-hash precondition.
type FileMutation struct {
	Operation        MutationOperation
	Path             string
	NewPath          string
	Content          []byte
	ExpectedSHA256   string
	ExpectAbsent     bool
	FormatterChanged bool
}

// ApplyEditBatchInput is one bounded, rollback-on-failure mutation set.
type ApplyEditBatchInput struct {
	TaskID              domain.TaskID
	Mutations           []FileMutation
	LargeDeleteApproved bool
}

// FileMutationResult records exact before/after identities.
type FileMutationResult struct {
	Operation    MutationOperation
	Path         string
	NewPath      string
	BeforeSHA256 string
	AfterSHA256  string
}

// EditBatchResult is the inspectable result of one complete batch.
type EditBatchResult struct {
	Results      []FileMutationResult
	Summary      RedactedEditSummary
	ChangedPaths []string
}

// RedactedEditSummary intentionally contains counts and a digest, not source
// content or repository-provided path text.
type RedactedEditSummary struct {
	TaskID      domain.TaskID
	BatchSHA256 string
	Created     int
	Updated     int
	Renamed     int
	Deleted     int
	FileCount   int
}

// EditEventRecorder persists the redacted summary after filesystem success.
type EditEventRecorder interface {
	RecordEditSummary(context.Context, RedactedEditSummary) error
}

// SetEditEventRecorder binds the durable event sink required by edit batches.
func (service *Service) SetEditEventRecorder(recorder EditEventRecorder) {
	service.events = recorder
}

// ApplyEditBatch performs complete preflight, rechecks each expected snapshot
// immediately before mutation, and restores every touched path on failure.
func (service *Service) ApplyEditBatch(
	ctx context.Context,
	input ApplyEditBatchInput,
) (EditBatchResult, error) {
	if input.TaskID.IsZero() {
		return EditBatchResult{}, errors.New("task ID must not be empty")
	}
	if len(input.Mutations) == 0 ||
		len(input.Mutations) > maximumEditBatchMutations {
		return EditBatchResult{}, errors.New("edit batch size is outside supported bounds")
	}
	if service.events == nil {
		return EditBatchResult{}, errors.New("edit event recorder is required")
	}
	verification, err := service.VerifyTaskWorktree(ctx, input.TaskID)
	if err != nil {
		return EditBatchResult{}, err
	}
	binding := verification.Binding
	originals := make(map[string]FileSnapshot)
	seenPaths := make(map[string]struct{})
	deleteFiles := 0
	deleteBytes := 0
	for index, mutation := range input.Mutations {
		if err := validateMutation(mutation); err != nil {
			return EditBatchResult{}, fmt.Errorf("mutation %d: %w", index, err)
		}
		for _, mutationPath := range mutationPaths(mutation) {
			if _, duplicate := seenPaths[mutationPath]; duplicate {
				return EditBatchResult{}, errors.New("edit batch repeats a path")
			}
			seenPaths[mutationPath] = struct{}{}
			snapshot, err := ReadFileAtRevision(ctx, binding, mutationPath)
			if err != nil {
				return EditBatchResult{}, err
			}
			originals[mutationPath] = snapshot
		}
		if err := verifyMutationPrecondition(mutation, originals[mutation.Path]); err != nil {
			return EditBatchResult{}, err
		}
		if mutation.Operation == MutationRename && originals[mutation.NewPath].Exists {
			return EditBatchResult{}, ErrEditConflict
		}
		if mutation.Operation == MutationDelete {
			deleteFiles++
			deleteBytes += len(originals[mutation.Path].Content)
		}
	}
	if !input.LargeDeleteApproved &&
		(deleteFiles >= broadDeleteFileCount || deleteBytes >= largeDeleteBytes) {
		return EditBatchResult{}, ErrApprovalRequired
	}

	var result EditBatchResult
	for _, mutation := range input.Mutations {
		current, err := ReadFileAtRevision(ctx, binding, mutation.Path)
		if err != nil {
			return EditBatchResult{}, errors.Join(err, restoreSnapshots(binding, originals))
		}
		if DetectConcurrentUserChanges(originals[mutation.Path], current) {
			return EditBatchResult{}, errors.Join(
				ErrEditConflict,
				restoreSnapshots(binding, originals),
			)
		}
		mutationResult, err := applyFileMutation(binding, mutation, current)
		if err != nil {
			return EditBatchResult{}, errors.Join(err, restoreSnapshots(binding, originals))
		}
		result.Results = append(result.Results, mutationResult)
	}
	result.ChangedPaths = make([]string, 0, len(seenPaths))
	for changedPath := range seenPaths {
		result.ChangedPaths = append(result.ChangedPaths, changedPath)
	}
	slices.Sort(result.ChangedPaths)
	result.Summary = summarizeEditBatch(input.TaskID, result.Results)
	if err := service.events.RecordEditSummary(ctx, result.Summary); err != nil {
		return EditBatchResult{}, errors.Join(
			fmt.Errorf("record edit summary: %w", err),
			restoreSnapshots(binding, originals),
		)
	}
	return result, nil
}

func validateMutation(mutation FileMutation) error {
	switch mutation.Operation {
	case MutationCreate:
		if !mutation.ExpectAbsent || mutation.ExpectedSHA256 != "" ||
			mutation.NewPath != "" {
			return errors.New("create requires only an absence precondition")
		}
	case MutationUpdate:
		if mutation.ExpectAbsent || mutation.NewPath != "" {
			return errors.New("update requires one expected content hash")
		}
	case MutationRename:
		if mutation.ExpectAbsent || mutation.NewPath == "" || len(mutation.Content) != 0 {
			return errors.New("rename requires source hash and absent destination")
		}
	case MutationDelete:
		if mutation.ExpectAbsent || mutation.NewPath != "" || len(mutation.Content) != 0 {
			return errors.New("delete requires one expected content hash")
		}
	default:
		return errors.New("mutation operation is invalid")
	}
	if mutation.Operation != MutationCreate {
		if err := validateObjectID(mutation.ExpectedSHA256); err != nil ||
			len(mutation.ExpectedSHA256) != 64 {
			return errors.New("expected content hash must be a lowercase SHA-256")
		}
	}
	if mutation.Operation == MutationCreate || mutation.Operation == MutationUpdate {
		if len(mutation.Content) > maximumEditableFileBytes ||
			!utf8.Valid(mutation.Content) ||
			strings.IndexByte(string(mutation.Content), 0) >= 0 {
			return ErrUnsupportedBinary
		}
	}
	return nil
}

func verifyMutationPrecondition(
	mutation FileMutation,
	snapshot FileSnapshot,
) error {
	if mutation.Operation == MutationCreate {
		if snapshot.Exists {
			return ErrEditConflict
		}
		return nil
	}
	if !snapshot.Exists || snapshot.SHA256 != mutation.ExpectedSHA256 {
		return ErrEditConflict
	}
	return nil
}

func mutationPaths(mutation FileMutation) []string {
	if mutation.Operation == MutationRename {
		return []string{mutation.Path, mutation.NewPath}
	}
	return []string{mutation.Path}
}

func applyFileMutation(
	binding storage.WorktreeBinding,
	mutation FileMutation,
	before FileSnapshot,
) (FileMutationResult, error) {
	source, err := ResolveTaskPath(binding, mutation.Path)
	if err != nil {
		return FileMutationResult{}, err
	}
	result := FileMutationResult{
		Operation: mutation.Operation,
		Path:      mutation.Path, NewPath: mutation.NewPath,
		BeforeSHA256: before.SHA256,
	}
	switch mutation.Operation {
	case MutationCreate, MutationUpdate:
		content := append([]byte(nil), mutation.Content...)
		mode := os.FileMode(0o644)
		if before.Exists {
			mode = before.Mode
			if before.NewlineStyle == "crlf" && !mutation.FormatterChanged {
				content = normalizeCRLF(content)
			}
		}
		if err := writeAtomic(source.Absolute, content, mode); err != nil {
			return FileMutationResult{}, err
		}
		digest := sha256.Sum256(content)
		result.AfterSHA256 = hex.EncodeToString(digest[:])
	case MutationRename:
		destination, err := ResolveTaskPath(binding, mutation.NewPath)
		if err != nil {
			return FileMutationResult{}, err
		}
		if err := os.Rename(source.Absolute, destination.Absolute); err != nil {
			return FileMutationResult{}, fmt.Errorf("rename task file: %w", err)
		}
		result.AfterSHA256 = before.SHA256
	case MutationDelete:
		if err := os.Remove(source.Absolute); err != nil {
			return FileMutationResult{}, fmt.Errorf("delete task file: %w", err)
		}
	}
	return result, nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".codeflux-edit-*")
	if err != nil {
		return fmt.Errorf("create task edit temporary file: %w", err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if err := file.Chmod(mode.Perm()); err != nil {
		_ = file.Close()
		return fmt.Errorf("preserve task file permissions: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write task edit: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync task edit: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close task edit: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace task file: %w", err)
	}
	return nil
}

func normalizeCRLF(content []byte) []byte {
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	return []byte(strings.ReplaceAll(normalized, "\n", "\r\n"))
}

func summarizeEditBatch(
	taskID domain.TaskID,
	results []FileMutationResult,
) RedactedEditSummary {
	var builder strings.Builder
	summary := RedactedEditSummary{TaskID: taskID, FileCount: len(results)}
	for _, result := range results {
		builder.WriteString(string(result.Operation))
		builder.WriteByte(0)
		builder.WriteString(result.Path)
		builder.WriteByte(0)
		builder.WriteString(result.NewPath)
		builder.WriteByte(0)
		builder.WriteString(result.BeforeSHA256)
		builder.WriteByte(0)
		builder.WriteString(result.AfterSHA256)
		builder.WriteByte(0)
		switch result.Operation {
		case MutationCreate:
			summary.Created++
		case MutationUpdate:
			summary.Updated++
		case MutationRename:
			summary.Renamed++
		case MutationDelete:
			summary.Deleted++
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	summary.BatchSHA256 = hex.EncodeToString(digest[:])
	return summary
}

func restoreSnapshots(
	binding storage.WorktreeBinding,
	snapshots map[string]FileSnapshot,
) error {
	paths := make([]string, 0, len(snapshots))
	for snapshotPath := range snapshots {
		paths = append(paths, snapshotPath)
	}
	slices.Sort(paths)
	var restoreErrors []error
	for _, snapshotPath := range paths {
		snapshot := snapshots[snapshotPath]
		if snapshot.Exists {
			continue
		}
		resolved, err := ResolveTaskPath(binding, snapshotPath)
		if err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if err := os.Remove(resolved.Absolute); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			restoreErrors = append(restoreErrors, fmt.Errorf("remove rolled-back task file: %w", err))
		}
	}
	for _, snapshotPath := range paths {
		snapshot := snapshots[snapshotPath]
		if !snapshot.Exists {
			continue
		}
		resolved, err := ResolveTaskPath(binding, snapshotPath)
		if err != nil {
			restoreErrors = append(restoreErrors, err)
			continue
		}
		if err := writeAtomic(resolved.Absolute, snapshot.Content, snapshot.Mode); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore task file: %w", err))
		}
	}
	return errors.Join(restoreErrors...)
}
