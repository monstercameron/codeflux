//go:build js && wasm

package main

import (
	"context"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/telemetryview"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/durationpb"
)

const mountedTelemetryTimeout = 5 * time.Second

func sessionTelemetryIdentity(sessionID domain.SessionID) *codefluxv1.StableIdentity {
	if sessionID.IsZero() {
		return nil
	}
	return &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION,
		Value: sessionID.String(),
	}
}

func useMountedFrontendTelemetry(active bool) telemetryview.Props {
	additional := ui.UseState(mountedTelemetryPage{})
	busy := ui.UseState(false)
	failed := ui.UseState(false)
	deleteConfirmation := ui.UseState(false)
	dependency := "inactive"
	if active {
		dependency = "settings"
	}
	resource := fetch.UseResource(func(parent context.Context) (mountedTelemetryPage, error) {
		if !active {
			return mountedTelemetryPage{}, nil
		}
		ctx, cancel := context.WithTimeout(parent, mountedTelemetryTimeout)
		defer cancel()
		return listBrowserFrontendTelemetry(ctx, "")
	}, dependency)
	base := resource.Get()
	page := mountedTelemetryPage{}
	if base.Ready {
		page = appendMountedTelemetry(base.Value, additional.Get())
	}
	props := telemetryview.Props{
		Loading: active && base.Loading, Error: active && (base.Error != nil || failed.Get()),
		Busy: busy.Get(), Rows: page.Rows, HasMore: page.HasMore,
		DeleteConfirmation: deleteConfirmation.Get(), OnReload: func() {
			failed.Set(false)
			additional.Set(mountedTelemetryPage{})
			resource.Reload()
		},
	}
	if !active {
		return props
	}
	if page.HasMore && !busy.Get() {
		props.OnLoadMore = func() {
			busy.Set(true)
			failed.Set(false)
			cursor := page.NextCursor
			ui.SafeGo("load older local telemetry", func() {
				ctx, cancel := context.WithTimeout(context.Background(), mountedTelemetryTimeout)
				defer cancel()
				next, err := listBrowserFrontendTelemetry(ctx, cursor)
				ui.PostAsync(func() {
					busy.Set(false)
					if err != nil {
						failed.Set(true)
						return
					}
					additional.Set(appendMountedTelemetry(additional.Get(), next))
				})
			})
		}
	}
	props.OnDeleteRequest = func() { deleteConfirmation.Set(true) }
	props.OnDeleteCancel = func() { deleteConfirmation.Set(false) }
	if !busy.Get() {
		props.OnDeleteConfirm = func() {
			key, err := composer.NewIdempotencyKey()
			if err != nil {
				failed.Set(true)
				return
			}
			busy.Set(true)
			failed.Set(false)
			ui.SafeGo("delete local frontend telemetry", func() {
				ctx, cancel := context.WithTimeout(context.Background(), mountedTelemetryTimeout)
				defer cancel()
				deleteErr := deleteBrowserFrontendTelemetry(ctx, string(key))
				ui.PostAsync(func() {
					busy.Set(false)
					if deleteErr != nil {
						failed.Set(true)
						return
					}
					deleteConfirmation.Set(false)
					additional.Set(mountedTelemetryPage{})
					resource.Reload()
				})
			})
		}
	}
	return props
}

func listBrowserFrontendTelemetry(ctx context.Context, cursor string) (mountedTelemetryPage, error) {
	connection, err := openBrowserSettingsConnection(ctx)
	if err != nil {
		return mountedTelemetryPage{}, err
	}
	defer connection.Close()
	return listMountedFrontendTelemetry(ctx, codefluxv1.NewSettingsServiceClient(connection), cursor)
}

func deleteBrowserFrontendTelemetry(ctx context.Context, key string) error {
	connection, err := openBrowserSettingsConnection(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	return deleteAllMountedFrontendTelemetry(ctx, codefluxv1.NewSettingsServiceClient(connection), key)
}

func emitBrowserFrontendTelemetry(event *codefluxv1.FrontendTelemetryEvent) {
	key, err := composer.NewIdempotencyKey()
	if err != nil {
		return
	}
	ui.SafeGo("record content-free frontend telemetry", func() {
		ctx, cancel := context.WithTimeout(context.Background(), mountedTelemetryTimeout)
		defer cancel()
		connection, dialErr := openBrowserSettingsConnection(ctx)
		if dialErr != nil {
			return
		}
		defer connection.Close()
		_ = recordMountedFrontendTelemetry(ctx, codefluxv1.NewSettingsServiceClient(connection), string(key), event)
	})
}

func openBrowserSettingsConnection(ctx context.Context) (*grpc.ClientConn, error) {
	return grpctunnel.DialContext(ctx, sessionclient.BridgePath, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func emitFirstRunCompletionTelemetry() {
	emitBrowserFrontendTelemetry(contentFreeTelemetryEvent(
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_FIRST_RUN_STEP,
		codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED,
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_FIRST_RUN,
		domain.TaskID{},
	))
}

func emitFirstRunFailureTelemetry(err error) {
	if event := firstRunFailureTelemetry(err); event != nil {
		emitBrowserFrontendTelemetry(event)
	}
}

func emitGraphOpenedTelemetry(taskID domain.TaskID) {
	emitGraphInteractionTelemetry(taskID, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_OPENED)
}

func emitGraphNavigatedTelemetry(taskID domain.TaskID) {
	emitGraphInteractionTelemetry(taskID, codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_NAVIGATED)
}

func emitGraphInteractionTelemetry(taskID domain.TaskID, outcome codefluxv1.FrontendTelemetryOutcome) {
	if taskID.IsZero() {
		return
	}
	event := contentFreeTelemetryEvent(
		codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_GRAPH_INTERACTION,
		outcome,
		codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_GRAPH,
		taskID,
	)
	event.GraphMode = codefluxv1.FrontendTelemetryGraphMode_FRONTEND_TELEMETRY_GRAPH_MODE_PROGRAM
	emitBrowserFrontendTelemetry(event)
}

func useReconnectCompletionTelemetry(status sessionclient.Status, sessionID domain.SessionID, started ui.Ref[time.Time]) {
	dependency := string(status.State) + "|" + sessionID.String()
	ui.UseEffectOf(func() func() {
		startedAt := started.Get()
		if status.State != sessionclient.StateLive || startedAt.IsZero() || sessionID.IsZero() {
			return nil
		}
		duration := time.Since(startedAt)
		if duration <= 0 {
			duration = time.Microsecond
		}
		event := contentFreeTelemetryEvent(
			codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_RECONNECT,
			codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_RECONNECTED,
			codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_SESSION,
			domain.TaskID{},
		)
		event.SessionId = sessionTelemetryIdentity(sessionID)
		event.Duration = durationpb.New(duration)
		started.Set(time.Time{})
		emitBrowserFrontendTelemetry(event)
		return nil
	}, dependency)
}

func useSlowRenderTelemetry(started time.Time, dependency string) {
	duration := time.Since(started)
	ui.UseEffectOf(func() func() {
		if duration < 50*time.Millisecond {
			return nil
		}
		event := contentFreeTelemetryEvent(
			codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_SLOW_RENDER,
			codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED,
			codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TIMELINE,
			domain.TaskID{},
		)
		event.Duration = durationpb.New(duration)
		emitBrowserFrontendTelemetry(event)
		return nil
	}, dependency)
}
