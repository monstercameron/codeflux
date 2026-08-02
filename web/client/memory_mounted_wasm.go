//go:build js && wasm

package main

import (
	"context"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/shell"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	mountedMemoryTimeout   = 8 * time.Second
	mountedMemoryPageLimit = 200
)

type memoryResourceClient interface {
	ListMemoryArtifacts(context.Context, *codefluxv1.ListMemoryArtifactsRequest, ...grpc.CallOption) (*codefluxv1.ListMemoryArtifactsResponse, error)
	GetMemoryArtifact(context.Context, *codefluxv1.GetMemoryArtifactRequest, ...grpc.CallOption) (*codefluxv1.GetMemoryArtifactResponse, error)
}

type memoryResourceLease struct {
	client memoryResourceClient
	close  func() error
}

// useMountedMemory reads this project's memory and keeps one artifact open.
//
// The list and the selected artifact are two reads, not one: the list is what
// the project has learned, and opening an entry is a deliberate act that
// fetches its content and provenance. Loading every artifact's full content to
// render a list would make the page slower the more the project has learned.
func useMountedMemory(
	projectID domain.ProjectID,
	translator frontendi18n.Translator,
) *shell.MemoryWorkspaceProps {
	selected := ui.UseState("")
	kindFilter := ui.UseState(domain.MemoryArtifactKind(""))

	dependency := "unavailable"
	if !projectID.IsZero() {
		dependency = projectID.String()
	}
	list := fetch.UseResource(func(parent context.Context) ([]shell.MemoryArtifactRow, error) {
		if projectID.IsZero() {
			return nil, errMountedMemoryScopeUnavailable
		}
		ctx, cancel := context.WithTimeout(parent, mountedMemoryTimeout)
		defer cancel()
		return loadMemoryArtifactRows(ctx, openBrowserMemoryResourceClient, projectID)
	}, dependency)

	detailDependency := "none"
	if selected.Get() != "" && !projectID.IsZero() {
		detailDependency = projectID.String() + "|" + selected.Get()
	}
	detail := fetch.UseResource(func(parent context.Context) (shell.MemoryArtifactDetail, error) {
		if detailDependency == "none" {
			return shell.MemoryArtifactDetail{}, errMountedMemoryScopeUnavailable
		}
		artifactID, err := domain.ParseMemoryArtifactID(selected.Get())
		if err != nil {
			return shell.MemoryArtifactDetail{}, err
		}
		ctx, cancel := context.WithTimeout(parent, mountedMemoryTimeout)
		defer cancel()
		return loadMemoryArtifactDetail(ctx, openBrowserMemoryResourceClient, projectID, artifactID)
	}, detailDependency)

	props := &shell.MemoryWorkspaceProps{
		Translator: translator,
		SelectedID: selected.Get(),
		KindFilter: kindFilter.Get(),
		OnSelect:   selected.Set,
		OnFilterKind: func(kind domain.MemoryArtifactKind) {
			kindFilter.Set(kind)
		},
		OnRetry: list.Reload,
	}
	listState := list.Get()
	switch {
	case projectID.IsZero():
		props.State = shell.SurfaceUnavailable
	case listState.Loading:
		props.State = shell.SurfaceLoading
	case listState.Error != nil:
		props.State = shell.SurfaceFailed
		props.ErrorMessage = listState.Error.Error()
	case listState.Ready:
		props.State = shell.SurfaceReady
		props.Rows = listState.Value
	default:
		props.State = shell.SurfaceLoading
	}
	if props.State != shell.SurfaceReady {
		return props
	}
	detailState := detail.Get()
	switch {
	case selected.Get() == "":
		props.DetailState = shell.SurfaceReady
	case detailState.Loading:
		props.DetailState = shell.SurfaceLoading
	case detailState.Error != nil:
		props.DetailState = shell.SurfaceFailed
		props.DetailError = detailState.Error.Error()
	case detailState.Ready:
		props.DetailState = shell.SurfaceReady
		value := detailState.Value
		props.Detail = &value
	default:
		props.DetailState = shell.SurfaceLoading
	}
	return props
}

func loadMemoryArtifactRows(
	ctx context.Context,
	opener func(context.Context) (memoryResourceLease, error),
	projectID domain.ProjectID,
) ([]shell.MemoryArtifactRow, error) {
	lease, err := opener(ctx)
	if err != nil {
		return nil, err
	}
	if lease.client == nil || lease.close == nil {
		return nil, errMountedMemoryBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.ListMemoryArtifacts(ctx, &codefluxv1.ListMemoryArtifactsRequest{
		ProjectId: graphProjectIdentity(projectID),
		Page:      &codefluxv1.PageRequest{Limit: mountedMemoryPageLimit},
	})
	if err != nil {
		return nil, err
	}
	return decodeMemoryArtifactRows(response)
}

func loadMemoryArtifactDetail(
	ctx context.Context,
	opener func(context.Context) (memoryResourceLease, error),
	projectID domain.ProjectID,
	artifactID domain.MemoryArtifactID,
) (shell.MemoryArtifactDetail, error) {
	lease, err := opener(ctx)
	if err != nil {
		return shell.MemoryArtifactDetail{}, err
	}
	if lease.client == nil || lease.close == nil {
		return shell.MemoryArtifactDetail{}, errMountedMemoryBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.GetMemoryArtifact(ctx, &codefluxv1.GetMemoryArtifactRequest{
		ProjectId: graphProjectIdentity(projectID),
		ArtifactId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MEMORY_ARTIFACT,
			Value: artifactID.String(),
		},
	})
	if err != nil {
		return shell.MemoryArtifactDetail{}, err
	}
	return decodeMemoryArtifactDetail(response)
}

func openBrowserMemoryResourceClient(ctx context.Context) (memoryResourceLease, error) {
	connection, err := grpctunnel.DialContext(ctx, sessionclient.BridgePath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return memoryResourceLease{}, err
	}
	return memoryResourceLease{client: codefluxv1.NewMemoryServiceClient(connection), close: connection.Close}, nil
}
