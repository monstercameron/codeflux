//go:build js && wasm

package main

import (
	"context"
	"errors"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/shell"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// repositoryInspectionTimeout bounds one working-tree read. It is longer than
// a plain database query because the coordinator shells out to Git, and a
// repository with a large tree takes a moment to answer.
const repositoryInspectionTimeout = 12 * time.Second

var errRepositoryInspectionScope = errors.New("no repository is selected to inspect")

// useMountedRepositoryPage builds the repositories surface.
//
// The listing is shared with the chooser's own read so the page does not ask
// the coordinator the same question twice. The working-tree read is separate
// and happens only for the repository a person selected: listing twenty
// repositories must not run twenty git status calls to draw a list.
func useMountedRepositoryPage(
	envelope bootstrapEnvelope,
	rows []shell.RepositoryRow,
	listState shell.SurfaceLoadState,
	listError string,
	reload func(),
	navigate func(string),
) *shell.RepositoryWorkspaceProps {
	selected := ui.UseState("")
	active := selected.Get()
	if active == "" {
		active = defaultSelectedRepository(rows, currentRepositoryIdentity(envelope))
	}
	dependency := "none"
	if active != "" && listState == shell.SurfaceReady {
		dependency = active
	}
	inspection := fetch.UseResource(func(parent context.Context) (shell.RepositoryInspection, error) {
		if dependency == "none" {
			return shell.RepositoryInspection{}, errRepositoryInspectionScope
		}
		repositoryID, err := domain.ParseRepositoryID(active)
		if err != nil {
			return shell.RepositoryInspection{}, err
		}
		ctx, cancel := context.WithTimeout(parent, repositoryInspectionTimeout)
		defer cancel()
		connection, err := grpctunnel.DialContext(
			ctx, sessionclient.BridgePath,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return shell.RepositoryInspection{}, err
		}
		defer func() { _ = connection.Close() }()
		response, err := codefluxv1.NewWorkspaceServiceClient(connection).InspectRepository(
			ctx,
			&codefluxv1.InspectRepositoryRequest{
				RepositoryId: &codefluxv1.StableIdentity{
					Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
					Value: repositoryID.String(),
				},
			},
		)
		if err != nil {
			return shell.RepositoryInspection{}, err
		}
		return projectRepositoryInspection(response), nil
	}, dependency)

	props := &shell.RepositoryWorkspaceProps{
		State: listState, ErrorMessage: listError, Rows: rows,
		SelectedID: active, OnSelect: selected.Set, OnRetry: reload,
		OnNavigatePath: navigate,
	}
	if listState != shell.SurfaceReady || active == "" {
		return props
	}
	current := inspection.Get()
	switch {
	case current.Loading:
		props.InspectionState = shell.SurfaceLoading
	case current.Error != nil:
		props.InspectionState = shell.SurfaceFailed
		props.InspectionError = current.Error.Error()
	case current.Ready:
		props.InspectionState = shell.SurfaceReady
		value := current.Value
		props.Inspection = &value
	default:
		props.InspectionState = shell.SurfaceLoading
	}
	return props
}

// defaultSelectedRepository opens the page on the repository this session is
// working in, because that is the one whose working tree the reader is about
// to act on. With none open it falls back to the first row rather than showing
// an empty panel beside a populated list.
func defaultSelectedRepository(rows []shell.RepositoryRow, currentID string) string {
	for _, row := range rows {
		if row.RepositoryID == currentID {
			return row.RepositoryID
		}
	}
	if len(rows) > 0 {
		return rows[0].RepositoryID
	}
	return ""
}
