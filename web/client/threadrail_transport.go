package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/threadrail"
)

var (
	errThreadRailAuthorizedRepositoryUnavailable = errors.New("thread rail authorized repository is unavailable")
	errThreadRailAuthorizedWorkspaceUnavailable  = errors.New("thread rail authorized workspace is unavailable")
	errThreadRailBridgeUnavailable               = errors.New("thread rail bridge is unavailable")
	errThreadRailSessionUnavailable              = errors.New("thread rail authoritative session is unavailable")
)

type threadRailScope struct {
	repositoryID domain.RepositoryID
	workspaceID  domain.WorkspaceID
}

func (scope threadRailScope) valid() bool {
	return !scope.repositoryID.IsZero() && !scope.workspaceID.IsZero()
}

// authorizedThreadRailScope accepts identities only from the authenticated
// bootstrap allowlist. It never derives a workspace or repository identity
// from a display name, browser path, fixture row, or route string alone.
func authorizedThreadRailScope(
	envelope bootstrapEnvelope,
	snapshot frontendstate.Snapshot,
	route routes.Route,
) (threadRailScope, error) {
	workspaceIdentity := envelope.SelectedWorkspaceID
	if workspaceIdentity == nil ||
		workspaceIdentity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE {
		return threadRailScope{}, errThreadRailAuthorizedWorkspaceUnavailable
	}
	workspaceID, err := domain.ParseWorkspaceID(workspaceIdentity.GetValue())
	if err != nil {
		return threadRailScope{}, errThreadRailAuthorizedWorkspaceUnavailable
	}

	authorized := make(map[string]domain.RepositoryID, len(envelope.RouteAccess.AccessibleRepositories))
	for _, identity := range envelope.RouteAccess.AccessibleRepositories {
		if identity == nil || identity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY {
			return threadRailScope{}, errThreadRailAuthorizedRepositoryUnavailable
		}
		repositoryID, parseErr := domain.ParseRepositoryID(identity.GetValue())
		if parseErr != nil {
			return threadRailScope{}, errThreadRailAuthorizedRepositoryUnavailable
		}
		authorized[repositoryID.String()] = repositoryID
	}

	if !route.RepositoryID.IsZero() {
		repositoryID, ok := authorized[route.RepositoryID.String()]
		if !ok {
			return threadRailScope{}, errThreadRailAuthorizedRepositoryUnavailable
		}
		return threadRailScope{repositoryID: repositoryID, workspaceID: workspaceID}, nil
	}
	if repositoryID, ok := authorized[snapshot.Workspace.RepositoryID]; ok {
		return threadRailScope{repositoryID: repositoryID, workspaceID: workspaceID}, nil
	}
	if len(authorized) == 1 {
		for _, repositoryID := range authorized {
			return threadRailScope{repositoryID: repositoryID, workspaceID: workspaceID}, nil
		}
	}
	return threadRailScope{}, errThreadRailAuthorizedRepositoryUnavailable
}

type threadRailClientLease struct {
	client threadrail.PageClient
	close  func() error
}

type threadRailClientOpener func(
	context.Context,
	domain.RepositoryID,
	domain.WorkspaceID,
) (threadRailClientLease, error)

func withThreadRailClient(
	ctx context.Context,
	opener threadRailClientOpener,
	scope threadRailScope,
	operation func(threadrail.PageClient) (threadrail.State, error),
) (threadrail.State, error) {
	if opener == nil || !scope.valid() || operation == nil {
		return threadrail.State{}, errThreadRailBridgeUnavailable
	}
	lease, err := opener(ctx, scope.repositoryID, scope.workspaceID)
	if err != nil {
		return threadrail.State{}, err
	}
	if lease.client == nil || lease.close == nil {
		return threadrail.State{}, errThreadRailBridgeUnavailable
	}
	defer lease.close()
	return operation(lease.client)
}

func retryThreadRailPage(
	ctx context.Context,
	state threadrail.State,
	client threadrail.PageClient,
) (threadrail.State, error) {
	loading, query, ok := threadrail.BeginPageRetry(state)
	if !ok {
		return state, nil
	}
	page, err := client.ListThreads(ctx, query)
	if err != nil {
		return threadrail.FailPage(loading, threadrail.PageError{
			Code: "retry-failed", Message: "Threads could not be loaded.", Retryable: true,
		}), err
	}
	return threadrail.ApplyPage(loading, page)
}

// threadRailSessionTarget resolves the server-issued session identity retained
// by the authoritative thread projection. SessionService subscriptions must not
// derive this target from a route, title, pending row, or client-generated ID.
func threadRailSessionTarget(state threadrail.State, threadID domain.ThreadID) (domain.SessionID, error) {
	if threadID.IsZero() {
		return domain.SessionID{}, errThreadRailSessionUnavailable
	}
	for _, row := range state.AllRows() {
		if row.Pending() || row.ThreadID() != threadID {
			continue
		}
		sessionID := row.Thread().SessionID()
		if sessionID.IsZero() {
			return domain.SessionID{}, errThreadRailSessionUnavailable
		}
		return sessionID, nil
	}
	return domain.SessionID{}, errThreadRailSessionUnavailable
}

// mountedThreadRailRow resolves only durable server-issued rows. Pending
// optimistic rows cannot be used to restore a selected session.
func mountedThreadRailRow(state threadrail.State, threadID domain.ThreadID) (threadrail.Row, bool) {
	for _, row := range state.AllRows() {
		if !row.Pending() && row.ThreadID() == threadID {
			return row, true
		}
	}
	return threadrail.Row{}, false
}

// notifyMountedThreadSelection re-emits the authoritative selected thread
// after a temporary interaction surface closes. The session identity check
// prevents a stale or synthetic row from replacing the live parent selection.
func notifyMountedThreadSelection(
	state threadrail.State,
	threadID domain.ThreadID,
	notify func(threadrail.Thread),
) bool {
	if notify == nil || threadID.IsZero() {
		return false
	}
	row, ok := mountedThreadRailRow(state, threadID)
	if !ok {
		return false
	}
	sessionID, err := threadRailSessionTarget(state, threadID)
	if err != nil || sessionID != row.Thread().SessionID() {
		return false
	}
	notify(row.Thread())
	return true
}

// cancelMountedThreadRename closes the temporary editor only after restoring
// its durable selection, then invokes the caller-provided GWC focus return.
func cancelMountedThreadRename(
	state threadrail.State,
	threadID domain.ThreadID,
	notify func(threadrail.Thread),
	closeEditor func(),
	returnFocus func(),
) bool {
	restored := notifyMountedThreadSelection(state, threadID, notify)
	if closeEditor != nil {
		closeEditor()
	}
	if returnFocus != nil {
		returnFocus()
	}
	return restored
}

// cancelMountedThreadArchive closes the confirmation without issuing an
// archive mutation and returns focus to the invoking archive button.
func cancelMountedThreadArchive(closeConfirmation func(), returnFocus func()) {
	if closeConfirmation != nil {
		closeConfirmation()
	}
	if returnFocus != nil {
		returnFocus()
	}
}

//lint:ignore U1000 Used by the js/wasm mounted rail; native tests exercise the injectable generator below.
func newThreadRailCommandKey(operation string) (threadrail.CommandKey, error) {
	return generateThreadRailCommandKey(rand.Reader, operation)
}

func generateThreadRailCommandKey(reader io.Reader, operation string) (threadrail.CommandKey, error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return "", errThreadRailBridgeUnavailable
	}
	var entropy [16]byte
	if _, err := io.ReadFull(reader, entropy[:]); err != nil {
		return "", fmt.Errorf("generate thread rail %s command key: %w", operation, err)
	}
	return threadrail.ParseCommandKey(operation + "-" + hex.EncodeToString(entropy[:]))
}
