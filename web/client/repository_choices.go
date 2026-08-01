package main

import (
	"errors"
	"sort"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
)

// errNoRepositoryChoices reports that the coordinator answered with nothing.
//
// It is distinguished from a transport failure because the two mean opposite
// things to a person: an empty list is a state to act on, and a failure is a
// state to retry.
var errNoRepositoryChoices = errors.New("the coordinator listed no repositories")

// projectRepositoryChoices turns a listing into the rows the chooser draws.
//
// The chooser used to be built with a hardcoded count of zero, so a person who
// had already opened a repository was told they had none and offered a browse
// control that could not create one. Everything here comes from the
// coordinator's own answer.
func projectRepositoryChoices(
	response *codefluxv1.ListRepositoriesResponse,
	openThreads map[string]routes.Route,
) ([]shell.RepositoryChoice, error) {
	summaries := response.GetRepositories()
	if len(summaries) == 0 {
		return nil, errNoRepositoryChoices
	}
	choices := make([]shell.RepositoryChoice, 0, len(summaries))
	for _, summary := range summaries {
		identity := summary.GetRepositoryId()
		repositoryID, err := domain.ParseRepositoryID(identity.GetValue())
		if err != nil {
			// A row whose identity cannot be parsed cannot be navigated to, and
			// drawing it would offer a person a control that does nothing.
			continue
		}
		choice := shell.RepositoryChoice{
			Name:     summary.GetDisplayName().GetValue(),
			Revision: shortRevision(summary.GetGit().GetHeadRevision()),
		}
		// A repository is entered through a thread when one is open, because a
		// repository with no conversation has no workspace to show.
		if route, present := openThreads[repositoryID.String()]; present {
			choice.Path = pathOrEmptyRoute(route)
			choice.Detail = "Open thread"
		} else {
			choice.Path = pathOrEmptyRoute(routes.Route{
				Name: routes.Memory, RepositoryID: repositoryID,
			})
			choice.Detail = "No open thread"
		}
		if choice.Name == "" {
			choice.Name = repositoryID.String()
		}
		choices = append(choices, choice)
	}
	if len(choices) == 0 {
		return nil, errNoRepositoryChoices
	}
	// The order is by name so a returning person finds the same row in the same
	// place; the coordinator orders by identity, which is not meaningful here.
	sort.SliceStable(choices, func(first, second int) bool {
		return choices[first].Name < choices[second].Name
	})
	return choices, nil
}

// shortRevision renders a Git revision at the length people actually read.
func shortRevision(revision string) string {
	const shortLength = 7
	if len(revision) <= shortLength {
		return revision
	}
	return revision[:shortLength]
}

// pathOrEmptyRoute renders a route, or nothing if it cannot be rendered.
func pathOrEmptyRoute(route routes.Route) string {
	path, err := routes.Path(route)
	if err != nil {
		return ""
	}
	return path
}

// selectedThreadRoutes maps a repository to the thread a browser enters it
// through.
//
// Only the repository the coordinator named is mapped. Assuming the selected
// thread belongs to every listed repository would send a person into the wrong
// conversation the moment a second repository exists, and the coordinator is
// the only thing that knows which one is right.
func selectedThreadRoutes(envelope bootstrapEnvelope) map[string]routes.Route {
	selected := map[string]routes.Route{}
	threadIdentity, repositoryIdentity := envelope.SelectedThreadID, envelope.SelectedRepositoryID
	if threadIdentity == nil || repositoryIdentity == nil {
		return selected
	}
	threadID, err := domain.ParseThreadID(threadIdentity.GetValue())
	if err != nil {
		return selected
	}
	repositoryID, err := domain.ParseRepositoryID(repositoryIdentity.GetValue())
	if err != nil {
		return selected
	}
	selected[repositoryID.String()] = routes.Route{
		Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID,
	}
	return selected
}
