package transport

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// WorkspaceApplication is the narrow surface the workspace service needs.
//
// It is an interface so the service can be exercised without a database, and
// so it cannot reach past what a workspace picker legitimately reads.
type WorkspaceApplication interface {
	ListRepositories(context.Context, int) ([]RepositoryRecord, error)
	GetRepository(context.Context, domain.RepositoryID) (RepositoryRecord, error)
	GetWorkspaceScope(context.Context, domain.WorkspaceID) (WorkspaceRecord, error)
	// ReadRepositoryState reports what Git says right now. The database records
	// what was true when a repository was opened, and a working tree changes
	// between one glance at the interface and the next, so a top bar drawn from
	// stored fields is a top bar that will eventually lie.
	ReadRepositoryState(context.Context, RepositoryRecord) (RepositoryGitState, error)
}

// RepositoryRecord is what this service needs to know about a repository.
//
// It is declared here rather than borrowed from the storage package because
// this is an adapter: it may depend on ports pointing inward, never on a
// sibling adapter's types. Naming its own inputs also keeps the service
// testable without a database.
type RepositoryRecord struct {
	ID            domain.RepositoryID
	CanonicalPath string
	GitIdentity   string
	Revision      uint64
}

// WorkspaceRecord is what this service needs to know about a workspace.
type WorkspaceRecord struct {
	WorkspaceID  domain.WorkspaceID
	RepositoryID domain.RepositoryID
}

// RepositoryGitState is the working tree as Git currently reports it.
//
// A field Git declined to answer is left zero rather than guessed, so a caller
// renders it as unknown instead of as something false.
type RepositoryGitState struct {
	Branch           string
	HeadRevision     string
	Detached         bool
	Dirty            bool
	ChangedPathCount uint32
}

// MaximumRepositoryPage bounds one page of repositories.
//
// It is declared here as well as in the store because the two bounds answer
// different questions: the store's protects the database, and this one decides
// whether a page told the caller everything. They are checked against each
// other by TestTheRepositoryPageBoundMatchesTheStore.
const MaximumRepositoryPage = 200

// ErrWorkspaceTargetNotFound reports a repository or workspace that is not
// there, so this service does not have to recognize another package's error.
var ErrWorkspaceTargetNotFound = errors.New("workspace target not found")

// WorkspaceService serves the repository and workspace surfaces.
//
// The whole service was declared in the API and never registered, so every
// call returned Unimplemented: the repository picker, the workspace state, and
// the inspection surface were unreachable against a coordinator that had the
// data for all three.
type WorkspaceService struct {
	codefluxv1.UnimplementedWorkspaceServiceServer
	application WorkspaceApplication
}

// NewWorkspaceService builds the service.
func NewWorkspaceService(application WorkspaceApplication) (*WorkspaceService, error) {
	if application == nil {
		return nil, errors.New("workspace application is required")
	}
	return &WorkspaceService{application: application}, nil
}

// ListRepositories returns the repositories a person can choose between.
func (service *WorkspaceService) ListRepositories(
	ctx context.Context,
	request *codefluxv1.ListRepositoriesRequest,
) (*codefluxv1.ListRepositoriesResponse, error) {
	limit := int(request.GetPage().GetLimit())
	listed, err := service.application.ListRepositories(ctx, limit)
	if err != nil {
		return nil, status.Error(codes.Internal, "repositories could not be listed")
	}
	summaries := make([]*codefluxv1.RepositorySummary, 0, len(listed))
	for _, repository := range listed {
		summaries = append(summaries, repositorySummary(repository))
	}
	return &codefluxv1.ListRepositoriesResponse{
		Repositories: summaries,
		// There is no continuation token because the page is bounded and
		// ordered by a stable key; a caller that needs more than the bound is
		// a caller whose picker needs a search rather than a longer list.
		Page: &codefluxv1.PageInfo{HasMore: len(summaries) >= MaximumRepositoryPage},
	}, nil
}

// InspectRepository returns one repository and anything worth warning about.
func (service *WorkspaceService) InspectRepository(
	ctx context.Context,
	request *codefluxv1.InspectRepositoryRequest,
) (*codefluxv1.InspectRepositoryResponse, error) {
	id, err := RepositoryIDFromProto(request.GetRepositoryId())
	if err != nil {
		return nil, err
	}
	repository, err := service.application.GetRepository(ctx, id)
	if err != nil {
		if errors.Is(err, ErrWorkspaceTargetNotFound) {
			return nil, status.Error(codes.NotFound, "no such repository")
		}
		return nil, status.Error(codes.Internal, "the repository could not be read")
	}
	summary := repositorySummary(repository)
	// A working-tree read can fail for ordinary reasons — the directory moved,
	// Git is mid-operation — and none of them are a reason to refuse the whole
	// inspection. The stored identity still answers the question of which
	// repository this is; only the live fields go unanswered.
	if live, liveErr := service.application.ReadRepositoryState(ctx, repository); liveErr == nil {
		summary.Git = &codefluxv1.GitStateView{
			HeadRevision:     live.HeadRevision,
			Branch:           live.Branch,
			Detached:         live.Detached,
			Dirty:            live.Dirty,
			ChangedPathCount: live.ChangedPathCount,
		}
	}
	return &codefluxv1.InspectRepositoryResponse{
		Repository: summary,
		Warnings:   repositoryWarnings(repository),
	}, nil
}

// GetWorkspaceState returns one workspace.
func (service *WorkspaceService) GetWorkspaceState(
	ctx context.Context,
	request *codefluxv1.GetWorkspaceStateRequest,
) (*codefluxv1.GetWorkspaceStateResponse, error) {
	id, err := WorkspaceIDFromProto(request.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	workspace, err := service.application.GetWorkspaceScope(ctx, id)
	if err != nil {
		if errors.Is(err, ErrWorkspaceTargetNotFound) {
			return nil, status.Error(codes.NotFound, "no such workspace")
		}
		return nil, status.Error(codes.Internal, "the workspace could not be read")
	}
	return &codefluxv1.GetWorkspaceStateResponse{
		Workspace: workspaceView(workspace, time.Time{}),
	}, nil
}

// OpenWorkspace is refused rather than half-implemented.
//
// Opening a workspace means granting a coordinator authority to read and
// change a directory a person named. That is an authority decision, and one
// this service cannot make on a caller-supplied path: the durable approval
// identity it would need does not exist yet. Returning a workspace here would
// be the coordinator claiming an authority nobody granted.
func (service *WorkspaceService) OpenWorkspace(
	context.Context,
	*codefluxv1.OpenWorkspaceRequest,
) (*codefluxv1.OpenWorkspaceResponse, error) {
	return nil, status.Error(codes.FailedPrecondition,
		"opening a workspace requires a durable repository approval, which is not yet "+
			"recorded; choose an already-approved repository instead")
}

// repositorySummary renders one repository for a picker.
func repositorySummary(repository RepositoryRecord) *codefluxv1.RepositorySummary {
	return &codefluxv1.RepositorySummary{
		RepositoryId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
			Value: repository.ID.String(),
		},
		// The display name is the directory name rather than the whole path:
		// a picker showing four absolute paths that differ in their last
		// segment is a picker nobody can read.
		DisplayName: &codefluxv1.RedactedText{
			Value: filepath.Base(repository.CanonicalPath),
		},
		// The root is named by its own directory rather than by an absolute
		// path: a summary is chosen from, and a path a client could act on
		// does not belong in one.
		Root:     &codefluxv1.SafePath{WorkspaceRelativeSlashPath: ".", Exists: true, Directory: true},
		Git:      &codefluxv1.GitStateView{HeadRevision: repository.GitIdentity},
		Revision: repository.Revision,
	}
}

// repositoryWarnings states anything a person should know before choosing.
func repositoryWarnings(repository RepositoryRecord) []*codefluxv1.RedactedText {
	var warnings []*codefluxv1.RedactedText
	if strings.TrimSpace(repository.GitIdentity) == "" {
		warnings = append(warnings, &codefluxv1.RedactedText{
			Value: "This repository has no recorded Git identity, so changes cannot be " +
				"bound to a base revision.",
		})
	}
	if !filepath.IsAbs(repository.CanonicalPath) {
		warnings = append(warnings, &codefluxv1.RedactedText{
			Value: "This repository's recorded path is not absolute, so it resolves " +
				"differently depending on where the coordinator was started.",
		})
	}
	return warnings
}

// workspaceView renders one workspace.
//
// The root is reported as the workspace-relative path the API's SafePath
// declares, not as an absolute one. An absolute path in a view is a path a
// client could act on, and nothing outside the coordinator is allowed to.
func workspaceView(workspace WorkspaceRecord, observed time.Time) *codefluxv1.WorkspaceView {
	view := &codefluxv1.WorkspaceView{
		WorkspaceId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE,
			Value: workspace.WorkspaceID.String(),
		},
		RepositoryId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
			Value: workspace.RepositoryID.String(),
		},
		Root: &codefluxv1.SafePath{
			WorkspaceRelativeSlashPath: ".", Exists: true, Directory: true,
		},
	}
	if !observed.IsZero() {
		view.ObservedAt = timestamppb.New(observed)
	}
	return view
}
