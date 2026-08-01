//go:build js && wasm

package main

import (
	"context"
	"errors"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// repositoryChoiceTimeout bounds the listing call.
//
// The chooser is the first thing a person sees, so it must reach a state it
// can explain rather than spin: a coordinator that has not answered in this
// long is one to report, not to keep waiting on.
const repositoryChoiceTimeout = 15 * time.Second

// useMountedRepositoryChoices asks the coordinator what can be opened.
//
// The chooser used to be drawn from a hardcoded count of zero, so somebody who
// had already opened a repository was told they had none, beside a browse
// control that could not create one either. This is the answer that replaces
// that.
func useMountedRepositoryChoices(envelope bootstrapEnvelope) mountedWorkspaceSource {
	// The dependency is the repository set the coordinator already published,
	// so the listing is re-asked when what is openable changes and not on every
	// unrelated render.
	dependency := repositoryChoiceDependency(envelope)
	resource := fetch.UseResource(func(parent context.Context) (mountedWorkspaceAnswer, error) {
		ctx, cancel := context.WithTimeout(parent, repositoryChoiceTimeout)
		defer cancel()
		connection, err := grpctunnel.DialContext(
			ctx, sessionclient.BridgePath,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return mountedWorkspaceAnswer{}, err
		}
		defer func() { _ = connection.Close() }()
		client := codefluxv1.NewWorkspaceServiceClient(connection)
		listed, err := client.ListRepositories(ctx, &codefluxv1.ListRepositoriesRequest{})
		if err != nil {
			return mountedWorkspaceAnswer{}, err
		}
		choices, err := projectRepositoryChoices(listed, selectedThreadRoutes(envelope))
		if err != nil {
			return mountedWorkspaceAnswer{}, err
		}
		answer := mountedWorkspaceAnswer{Choices: choices}
		// The inspection is a second call because it reads the working tree,
		// which the listing deliberately does not: a picker over twenty
		// repositories must not run twenty git status calls to draw itself.
		if selected := envelope.SelectedRepositoryID; selected != nil {
			inspected, inspectErr := client.InspectRepository(
				ctx, &codefluxv1.InspectRepositoryRequest{RepositoryId: selected},
			)
			if inspectErr == nil {
				answer.Summary = projectWorkspaceSummary(inspected)
			}
			// A failed inspection is not reported as a failure of the whole
			// answer. The repository list is still correct and still usable;
			// only the top bar's live fields go unanswered.
		}
		return answer, nil
	}, dependency)

	source := mountedWorkspaceSource{}
	set := shell.RepositoryChoiceSet{}
	current := resource.Get()
	switch {
	case errors.Is(current.Error, errNoRepositoryChoices):
		// An empty list is a state to act on, not one to retry, so it is not
		// reported as a failure.
		set.State = state.DataReadyEmpty
	case current.Error != nil:
		set.State = state.DataRecoverableError
	case current.Loading || !current.Ready:
		set.State = state.DataLoading
	case len(current.Value.Choices) == 0:
		set.State = state.DataReadyEmpty
	default:
		set.State, set.Choices = state.DataReady, current.Value.Choices
		source.Summary = current.Value.Summary
	}
	source.Choices = &set
	return source
}

// mountedWorkspaceAnswer is one coordinator answer about the workspace.
type mountedWorkspaceAnswer struct {
	Choices []shell.RepositoryChoice
	Summary workspaceSummary
}

// mountedWorkspaceSource is what the root reads from that answer.
type mountedWorkspaceSource struct {
	Choices *shell.RepositoryChoiceSet
	Summary workspaceSummary
}

// repositoryChoiceDependency keys the listing to what the coordinator says is
// openable.
func repositoryChoiceDependency(envelope bootstrapEnvelope) string {
	key := "repositories"
	for _, identity := range envelope.RouteAccess.AccessibleRepositories {
		key += "|" + identity.GetValue()
	}
	return key
}
