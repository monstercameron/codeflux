package main

import (
	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
)

// projectRepositoryRows turns a listing into the rows the repositories page
// draws.
//
// A row whose identity cannot be parsed is dropped rather than shown: every
// action on this page is scoped by that identity, and a row that cannot name
// its repository would offer controls that act on nothing.
func projectRepositoryRows(
	response *codefluxv1.ListRepositoriesResponse,
	openThreads map[string]routes.Route,
	accessible map[string]bool,
	currentRepositoryID string,
) []shell.RepositoryRow {
	summaries := response.GetRepositories()
	rows := make([]shell.RepositoryRow, 0, len(summaries))
	for _, summary := range summaries {
		repositoryID, err := domain.ParseRepositoryID(summary.GetRepositoryId().GetValue())
		if err != nil {
			continue
		}
		identity := repositoryID.String()
		row := shell.RepositoryRow{
			RepositoryID:     identity,
			Name:             summary.GetDisplayName().GetValue(),
			RecordedRevision: shortRevision(summary.GetGit().GetHeadRevision()),
			Revision:         summary.GetRevision(),
			Current:          identity == currentRepositoryID,
			// An empty access map means the coordinator published no
			// restriction, which is not the same as restricting everything.
			Accessible: len(accessible) == 0 || accessible[identity],
		}
		if route, present := openThreads[identity]; present {
			row.ThreadPath = pathOrEmptyRoute(route)
		}
		if row.Name == "" {
			row.Name = identity
		}
		rows = append(rows, row)
	}
	return rows
}

// projectRepositoryInspection reads one repository's live working tree.
//
// WorkingTreeKnown is false when the coordinator answered with no Git state at
// all, so the page can say the tree is unknown instead of drawing it clean.
// "Clean" is the claim somebody starts an agent on the strength of.
func projectRepositoryInspection(
	response *codefluxv1.InspectRepositoryResponse,
) shell.RepositoryInspection {
	git := response.GetRepository().GetGit()
	inspection := shell.RepositoryInspection{
		Branch:           git.GetBranch(),
		HeadRevision:     git.GetHeadRevision(),
		Detached:         git.GetDetached(),
		Dirty:            git.GetDirty(),
		ChangedPathCount: git.GetChangedPathCount(),
		// The coordinator says outright whether Git answered. Inferring it from
		// a non-nil message would report the stored fallback as a clean tree.
		WorkingTreeKnown: response.GetWorkingTreeRead(),
	}
	for _, warning := range response.GetWarnings() {
		if value := warning.GetValue(); value != "" {
			inspection.Warnings = append(inspection.Warnings, value)
		}
	}
	return inspection
}

// currentRepositoryIdentity names the repository this session was opened
// against, or nothing when the coordinator named none.
func currentRepositoryIdentity(envelope bootstrapEnvelope) string {
	identity := envelope.SelectedRepositoryID
	if identity == nil {
		return ""
	}
	repositoryID, err := domain.ParseRepositoryID(identity.GetValue())
	if err != nil {
		return ""
	}
	return repositoryID.String()
}

// accessibleRepositoryIdentities is the set this session may route into.
func accessibleRepositoryIdentities(envelope bootstrapEnvelope) map[string]bool {
	accessible := make(map[string]bool, len(envelope.RouteAccess.AccessibleRepositories))
	for _, identity := range envelope.RouteAccess.AccessibleRepositories {
		if repositoryID, err := domain.ParseRepositoryID(identity.GetValue()); err == nil {
			accessible[repositoryID.String()] = true
		}
	}
	return accessible
}
