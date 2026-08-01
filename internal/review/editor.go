package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	maximumEditorRelativePathBytes = 4096
	maximumEditorSourceBytes       = 8 << 20
)

var (
	// ErrInvalidEditorSource identifies a malformed path or source coordinate.
	ErrInvalidEditorSource = errors.New("invalid editor source location")
	// ErrEditorSourceOutsideRepository identifies a path that is absolute,
	// traverses, or resolves through a link outside the bound repository.
	ErrEditorSourceOutsideRepository = errors.New("editor source is outside the bound repository")
	// ErrEditorSourceUnavailable identifies a target that is absent, not a
	// regular UTF-8 source file, or too large for bounded validation.
	ErrEditorSourceUnavailable = errors.New("editor source is unavailable")
)

// EditorWorkspace is the exact repository authority resolved for one active
// workspace. RepositoryRoot is server-owned configuration, never model input.
type EditorWorkspace struct {
	WorkspaceID    domain.WorkspaceID
	RepositoryRoot string
}

// EditorWorkspaceResolver resolves the active repository authority for an
// editor-open request.
type EditorWorkspaceResolver interface {
	ResolveEditorWorkspace(context.Context, domain.WorkspaceID) (EditorWorkspace, error)
}

// EditorLauncher performs the external editor effect for an already validated
// source target. Implementations must pass Path, Line, and Column as process
// arguments and must not interpolate them into a shell command.
type EditorLauncher interface {
	OpenEditor(context.Context, EditorTarget) error
}

// OpenEditorCommand is one explicit local request to open a source location.
type OpenEditorCommand struct {
	WorkspaceID    domain.WorkspaceID
	RelativePath   string
	Line           uint32
	Column         uint32
	IdempotencyKey string
}

// EditorTarget is an immutable, repository-confined source location. Private
// fields ensure only ResolveEditorTarget can construct a launchable value.
type EditorTarget struct {
	relativePath string
	absolutePath string
	line         uint32
	column       uint32
}

func (target EditorTarget) RelativePath() string { return target.relativePath }
func (target EditorTarget) AbsolutePath() string { return target.absolutePath }
func (target EditorTarget) Line() uint32         { return target.line }
func (target EditorTarget) Column() uint32       { return target.column }

// EditorOpenService resolves workspace authority before permitting the
// external editor effect.
type EditorOpenService struct {
	workspaces EditorWorkspaceResolver
	launcher   EditorLauncher
}

// NewEditorOpenService validates the complete editor-open dependency boundary.
func NewEditorOpenService(
	workspaces EditorWorkspaceResolver,
	launcher EditorLauncher,
) (*EditorOpenService, error) {
	if workspaces == nil {
		return nil, errors.New("editor workspace resolver is required")
	}
	if launcher == nil {
		return nil, errors.New("editor launcher is required")
	}
	return &EditorOpenService{workspaces: workspaces, launcher: launcher}, nil
}

// OpenInEditor resolves and validates a source location before performing one
// explicit editor launch. Validation failure performs no external effect.
func (service *EditorOpenService) OpenInEditor(
	ctx context.Context,
	command OpenEditorCommand,
) (EditorTarget, error) {
	if command.WorkspaceID.IsZero() {
		return EditorTarget{}, fmt.Errorf("%w: workspace ID is required", ErrInvalidEditorSource)
	}
	workspace, err := service.workspaces.ResolveEditorWorkspace(ctx, command.WorkspaceID)
	if err != nil {
		return EditorTarget{}, fmt.Errorf("resolve editor workspace: %w", err)
	}
	if workspace.WorkspaceID != command.WorkspaceID {
		return EditorTarget{}, fmt.Errorf("%w: resolved workspace identity mismatch", ErrEditorSourceOutsideRepository)
	}
	target, err := ResolveEditorTarget(
		workspace.RepositoryRoot,
		command.RelativePath,
		command.Line,
		command.Column,
	)
	if err != nil {
		return EditorTarget{}, err
	}
	if err := service.launcher.OpenEditor(ctx, target); err != nil {
		return EditorTarget{}, fmt.Errorf("open external editor: %w", err)
	}
	return target, nil
}

// ResolveEditorTarget validates a canonical slash-separated repository path,
// rejects link escape, and verifies that the requested coordinate exists in a
// bounded regular UTF-8 source file.
func ResolveEditorTarget(
	repositoryRoot string,
	relativePath string,
	line uint32,
	column uint32,
) (EditorTarget, error) {
	if line == 0 || column == 0 {
		return EditorTarget{}, fmt.Errorf("%w: line and column must be positive", ErrInvalidEditorSource)
	}
	cleaned, err := validateEditorRelativePath(relativePath)
	if err != nil {
		return EditorTarget{}, err
	}
	root, err := validatedEditorRepositoryRoot(repositoryRoot)
	if err != nil {
		return EditorTarget{}, err
	}
	absolute := filepath.Join(root, filepath.FromSlash(cleaned))
	if !editorPathWithin(root, absolute) {
		return EditorTarget{}, ErrEditorSourceOutsideRepository
	}
	if err := validateEditorPathComponents(root, cleaned); err != nil {
		return EditorTarget{}, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil || !sameEditorPath(resolved, absolute) || !editorPathWithin(root, resolved) {
		return EditorTarget{}, ErrEditorSourceOutsideRepository
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumEditorSourceBytes {
		return EditorTarget{}, ErrEditorSourceUnavailable
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return EditorTarget{}, fmt.Errorf("%w: read source", ErrEditorSourceUnavailable)
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return EditorTarget{}, ErrEditorSourceUnavailable
	}
	if !editorCoordinateExists(content, line, column) {
		return EditorTarget{}, fmt.Errorf("%w: source coordinate is outside the file", ErrInvalidEditorSource)
	}
	return EditorTarget{
		relativePath: cleaned,
		absolutePath: absolute,
		line:         line,
		column:       column,
	}, nil
}

func validateEditorRelativePath(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || len(value) > maximumEditorRelativePathBytes ||
		!utf8.ValidString(value) || strings.Contains(value, `\`) || strings.Contains(value, ":") ||
		path.IsAbs(value) || strings.HasPrefix(value, "/") {
		return "", ErrEditorSourceOutsideRepository
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return "", ErrInvalidEditorSource
		}
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrEditorSourceOutsideRepository
	}
	return cleaned, nil
}

func validatedEditorRepositoryRoot(value string) (string, error) {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return "", fmt.Errorf("%w: repository root is not canonical", ErrEditorSourceOutsideRepository)
	}
	root, err := filepath.EvalSymlinks(value)
	if err != nil || !sameEditorPath(root, value) {
		return "", fmt.Errorf("%w: repository root is not resolved", ErrEditorSourceOutsideRepository)
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return "", ErrEditorSourceUnavailable
	}
	return root, nil
}

func validateEditorPathComponents(root, relative string) error {
	current := root
	for _, part := range strings.Split(relative, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrEditorSourceUnavailable
			}
			return fmt.Errorf("%w: inspect source path", ErrEditorSourceUnavailable)
		}
		// ModeIrregular covers Windows directory junctions, which Lstat does
		// not report as symlinks and filepath.EvalSymlinks does not resolve.
		// Creating one needs no privilege, so it is the reparse point most
		// available to an attacker aiming an editor launch outside the
		// repository.
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return ErrEditorSourceOutsideRepository
		}
		resolved, err := filepath.EvalSymlinks(current)
		if err != nil || !sameEditorPath(resolved, current) || !editorPathWithin(root, resolved) {
			return ErrEditorSourceOutsideRepository
		}
	}
	return nil
}

func editorCoordinateExists(content []byte, line, column uint32) bool {
	lines := bytes.Split(content, []byte("\n"))
	if uint64(line) > uint64(len(lines)) {
		return false
	}
	selected := bytes.TrimSuffix(lines[line-1], []byte("\r"))
	return uint64(column) <= uint64(utf8.RuneCount(selected))+1
}

func editorPathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func sameEditorPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
