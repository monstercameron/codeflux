package coordinator

import (
	"context"
	"fmt"
	"strings"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

// selectedContext is one deterministic context selection and the durable
// manifest recorded for it.
type selectedContext struct {
	Items      []agentloop.RepositoryContextItem
	ManifestID string
	// Degraded records that selection could not run and the worktree listing
	// was used instead. It is reported rather than hidden: a run that planned
	// against a directory listing made a weaker decision than one that planned
	// against selected context, and the difference has to be visible.
	Degraded bool
	Reason   string
}

// selectAgentContext builds the repository map, runs deterministic context
// selection, and records the manifest (M08-043, M08-045, AUDIT-010).
//
// Before this, workspace.SelectContext had no production caller at all: the
// agent's first round was a directory listing plus go.mod and README.md, no
// manifest was ever persisted, and the expandable context card therefore had
// nothing to expand. M08-043 and M08-045 were recorded complete against code
// that ran only in tests.
//
// Selection failing is not fatal. A repository that cannot be mapped is still
// a repository the user asked for work in, so the run continues against the
// listing and says so, rather than refusing to start over context quality.
func selectAgentContext(
	ctx context.Context,
	manifests storage.ContextManifestOperations,
	repositoryID domain.RepositoryID,
	worktree string,
	revision string,
	requirement string,
	redactor *redact.Pipeline,
	approvals workspace.InstructionApprovalResolver,
) selectedContext {
	fallback := func(reason string) selectedContext {
		return selectedContext{
			Items:    worktreeContextItems(worktree),
			Degraded: true,
			Reason:   reason,
		}
	}
	if redactor == nil {
		return fallback("no redaction pipeline is configured")
	}

	runner := workspace.ExecRunner{}
	repositoryMap, err := workspace.BuildRepositoryMap(ctx, workspace.RepositorySnapshot{
		CanonicalRoot: worktree,
		HeadRevision:  revision,
		GitIdentity:   repositoryID.String(),
	}, runner)
	if err != nil {
		return fallback("repository map unavailable: " + boundedReason(err.Error()))
	}

	manifest, err := workspace.SelectContext(ctx, worktree, repositoryMap, workspace.ContextQuery{
		Requirement:          requirement,
		InstructionApprovals: approvals,
		Budget: workspace.ContextBudget{
			// Matched to the loop's own limits, so selection cannot hand the
			// agent more than the round will carry.
			MaxFiles: 20, MaxBytes: 400000, MaxEstimatedTokens: 100000,
		},
	}, runner, redactor)
	if err != nil {
		return fallback("context selection failed: " + boundedReason(err.Error()))
	}

	selected := selectedContext{}
	if manifests != nil {
		recorded, recordErr := recordSelectedContext(ctx, manifests, repositoryID, manifest)
		if recordErr != nil {
			// The selection is still good; only its evidence is missing. That
			// is worth reporting and not worth discarding the selection over.
			selected.Reason = "manifest not recorded: " + boundedReason(recordErr.Error())
		} else {
			selected.ManifestID = recorded
		}
	}

	for _, item := range manifest.Items {
		selected.Items = append(selected.Items, agentloop.RepositoryContextItem{
			Path:            contextItemLabel(item),
			ContentRedacted: item.Content,
			ContentSHA256:   item.ContentSHA256,
		})
	}
	if len(selected.Items) == 0 {
		// Selection ran and chose nothing. The listing is better than an empty
		// first round, but the run is still degraded and says so.
		return selectedContext{
			Items:      worktreeContextItems(worktree),
			ManifestID: selected.ManifestID,
			Degraded:   true,
			Reason:     "context selection matched no file",
		}
	}
	return selected
}

// recordSelectedContext persists the manifest as immutable evidence.
func recordSelectedContext(
	ctx context.Context,
	manifests storage.ContextManifestOperations,
	repositoryID domain.RepositoryID,
	manifest workspace.ContextManifest,
) (string, error) {
	identity, err := domain.NewEventID()
	if err != nil {
		return "", err
	}
	record := storage.RecordContextManifest{
		ID:                  "ctx-" + identity.String(),
		RepositoryID:        repositoryID,
		RepositoryRevision:  manifest.RepositoryRevision,
		MapRevision:         manifest.MapRevision,
		RequirementSHA256:   manifest.RequirementSHA256,
		SelectionPolicy:     manifest.SelectionPolicy,
		MaxFiles:            manifest.Budget.MaxFiles,
		MaxBytes:            manifest.Budget.MaxBytes,
		MaxEstimatedTokens:  manifest.Budget.MaxEstimatedTokens,
		UsedFiles:           manifest.UsedFiles,
		UsedBytes:           manifest.UsedBytes,
		UsedEstimatedTokens: manifest.UsedEstimatedTokens,
	}
	for _, item := range manifest.Items {
		record.Items = append(record.Items, storage.ContextManifestItem{
			Path: item.Path, Kind: item.Kind,
			StartLine: item.StartLine, EndLine: item.EndLine,
			ContentRedacted: item.Content, ContentSHA256: item.ContentSHA256,
			Reasons: item.Reasons, Trust: item.Trust,
			Generated: item.Generated, Binary: item.Binary,
			Minified: item.Minified, Vendor: item.Vendor,
			Dependency: item.Dependency, EstimatedTokens: item.EstimatedTokens,
		})
	}
	for _, exclusion := range manifest.Exclusions {
		record.Exclusions = append(record.Exclusions, storage.ContextManifestExclusion{
			Path: exclusion.Path, Reason: exclusion.Reason,
		})
	}
	stored, err := manifests.RecordContextManifest(ctx, record)
	if err != nil {
		return "", err
	}
	return stored.ID, nil
}

// contextItemLabel names an excerpt by the lines it came from, so the agent
// can tell a whole file from a fragment of one.
func contextItemLabel(item workspace.ContextItem) string {
	if item.StartLine > 0 && item.EndLine >= item.StartLine {
		return fmt.Sprintf("%s:%d-%d", item.Path, item.StartLine, item.EndLine)
	}
	return item.Path
}

// boundedReason keeps a degradation reason short enough to belong in a
// timeline entry.
func boundedReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if len(trimmed) > 200 {
		return trimmed[:200] + "…"
	}
	return trimmed
}
