//go:build js && wasm

package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const mountedThreadRailTimeout = 5 * time.Second

type mountedThreadRailProps struct {
	Envelope                 bootstrapEnvelope
	Snapshot                 frontendstate.Snapshot
	Route                    routes.Route
	Mode                     primitives.Mode
	OnNavigate               func(routes.Route)
	OnAuthoritativeSelection func(threadrail.Thread)
	FirstPage                fetch.ResourceState[threadrail.State]
}

type mountedThreadRailFirstPageSource struct {
	State  fetch.ResourceState[threadrail.State]
	Reload func()
}

func useMountedThreadRailFirstPage(
	envelope bootstrapEnvelope,
	snapshot frontendstate.Snapshot,
	route routes.Route,
) mountedThreadRailFirstPageSource {
	scope, scopeErr := authorizedThreadRailScope(envelope, snapshot, route)
	dependency := "unavailable"
	if scopeErr == nil {
		dependency = scope.repositoryID.String() + "|" + scope.workspaceID.String()
	}
	resource := fetch.UseResource(func(parent context.Context) (threadrail.State, error) {
		if scopeErr != nil {
			return threadrail.State{}, scopeErr
		}
		ctx, cancel := context.WithTimeout(parent, mountedThreadRailTimeout)
		defer cancel()
		initial, err := threadrail.NewState(scope.repositoryID, scope.workspaceID)
		if err != nil {
			return threadrail.State{}, err
		}
		if route.Name == routes.ThreadWorkspace && route.RepositoryID == scope.repositoryID && !route.ThreadID.IsZero() {
			initial, err = threadrail.RestoreSelection(initial, route)
			if err != nil {
				return threadrail.State{}, err
			}
		}
		return withThreadRailClient(ctx, openBrowserThreadRailClient, scope, func(client threadrail.PageClient) (threadrail.State, error) {
			return threadrail.LoadFirstPage(ctx, initial, client)
		})
	}, dependency)
	return mountedThreadRailFirstPageSource{State: resource.Get(), Reload: resource.Reload}
}

// mountedThreadRail owns the product rail's generated-client lifecycle. It
// falls back only when bootstrap lacks an authoritative scope or ThreadService
// is unavailable; ordinary list failures remain visible retryable rail state.
func mountedThreadRail(props mountedThreadRailProps) ui.Node {
	scope, scopeErr := authorizedThreadRailScope(props.Envelope, props.Snapshot, props.Route)
	initial := threadrail.State{}
	if scopeErr == nil {
		initial, scopeErr = threadrail.NewState(scope.repositoryID, scope.workspaceID)
		if scopeErr == nil && props.Route.Name == routes.ThreadWorkspace &&
			props.Route.RepositoryID == scope.repositoryID && !props.Route.ThreadID.IsZero() {
			initial, scopeErr = threadrail.RestoreSelection(initial, props.Route)
		}
		if scopeErr == nil {
			initial, _ = threadrail.BeginFirstPage(initial)
		}
	}
	railState := ui.UseState(initial)
	fallback := ui.UseState(scopeErr != nil)
	fallbackReason := ui.UseState(threadRailFallbackReason(scopeErr))
	transportMessage := ui.UseState("Loading threads from the local coordinator.")
	createBusy := ui.UseState(false)
	renameBusy := ui.UseState(false)
	archiveBusy := ui.UseState(false)
	pageBusy := ui.UseState(false)
	createCommand := ui.UseState(threadrail.CreateCommand{})
	renameCommand := ui.UseState(threadrail.RenameCommand{})
	archiveCommand := ui.UseState(threadrail.ArchiveCommand{})
	archiveConfirmation := ui.UseState(domain.ThreadID{})
	renameTarget := ui.UseState(domain.ThreadID{})
	renameTitle := ui.UseState("")
	focusManager := ui.UseFocusManager()

	if fallback.Get() {
		return threadRailFallback(props, fallbackReason.Get())
	}
	if scopeErr != nil {
		return threadRailFallback(props, threadRailFallbackReason(scopeErr))
	}
	resolvedRailState := func() threadrail.State {
		current := railState.Get()
		remote := props.FirstPage.Value
		if !remote.RepositoryID().IsZero() &&
			(current.LoadState() == threadrail.LoadNotRequested || current.LoadState() == threadrail.LoadLoading) {
			return remote
		}
		return current
	}

	applyState := func(next threadrail.State, err error) bool {
		if err != nil {
			return false
		}
		railState.Set(next)
		return true
	}
	runRemote := func(
		label string,
		setBusy func(bool),
		operation func(context.Context, threadrail.State, threadrail.PageClient) (threadrail.State, error),
	) {
		if setBusy != nil {
			setBusy(true)
		}
		ui.SafeGo(label, func() {
			ctx, cancel := context.WithTimeout(context.Background(), mountedThreadRailTimeout)
			defer cancel()
			current := resolvedRailState()
			next, err := withThreadRailClient(ctx, openBrowserThreadRailClient, scope, func(client threadrail.PageClient) (threadrail.State, error) {
				return operation(ctx, current, client)
			})
			ui.PostAsync(func() {
				if setBusy != nil {
					setBusy(false)
				}
				if isThreadRailServiceUnavailable(err) {
					fallbackReason.Set("ThreadService became unavailable; showing the local preview.")
					fallback.Set(true)
					return
				}
				if !next.RepositoryID().IsZero() {
					railState.Set(next)
					notifyMountedThreadSelection(next, next.SelectedThreadID(), props.OnAuthoritativeSelection)
				}
				if err != nil {
					transportMessage.Set("The thread command was not confirmed. Its request identity remains retained.")
					return
				}
				transportMessage.Set("Thread changes are synchronized with the local coordinator.")
			})
		})
	}

	current := resolvedRailState()
	message := transportMessage.Get()
	if message == "Loading threads from the local coordinator." {
		switch {
		case props.FirstPage.Loading:
			message = "Connecting to ThreadService through the local bridge."
		case props.FirstPage.Error != nil:
			message = "Threads could not be loaded from the local coordinator. Retry when ready."
		case props.FirstPage.Ready:
			message = "Threads are synchronized with the local coordinator."
		}
	}
	rail := ui.CreateElement(threadrail.ThreadRail, threadrail.ThreadRailProps{
		State: current, Mode: props.Mode, Embedded: true, Height: 360,
		NewThreadBusy: createBusy.Get(), RenameBusy: renameBusy.Get(), ArchiveBusy: archiveBusy.Get(),
		OnNewThread: func() {
			if createBusy.Get() {
				return
			}
			command := createCommand.Get()
			if command.Key == "" {
				key, err := newThreadRailCommandKey("create-thread")
				if err != nil {
					return
				}
				command = threadrail.CreateCommand{Key: key, Title: "Untitled thread", StartedAt: time.Now().UTC()}
				createCommand.Set(command)
			}
			runRemote("create authoritative thread", createBusy.Set, func(ctx context.Context, state threadrail.State, client threadrail.PageClient) (threadrail.State, error) {
				next, commandErr := threadrail.CreateThread(ctx, state, client, command)
				if commandErr == nil {
					createCommand.Set(threadrail.CreateCommand{})
				}
				return next, commandErr
			})
		},
		OnFilterChange: func(filter threadrail.Filter) {
			applyState(threadrail.SetFilter(resolvedRailState(), filter))
		},
		OnActiveChange: func(key threadrail.RowKey) {
			applyState(threadrail.SetActiveRow(resolvedRailState(), key))
		},
		OnSelect: func(key threadrail.RowKey) {
			next, route, err := threadrail.SelectThread(resolvedRailState(), key)
			if applyState(next, err) && props.OnNavigate != nil {
				notifyMountedThreadSelection(next, route.ThreadID, props.OnAuthoritativeSelection)
				props.OnNavigate(route)
			}
		},
		OnLoadNext: func() {
			if pageBusy.Get() {
				return
			}
			runRemote("load next authoritative thread page", pageBusy.Set, func(ctx context.Context, state threadrail.State, client threadrail.PageClient) (threadrail.State, error) {
				return threadrail.LoadNextPage(ctx, state, client)
			})
		},
		OnRetry: func() {
			if pageBusy.Get() {
				return
			}
			runRemote("retry authoritative thread page", pageBusy.Set, retryThreadRailPage)
		},
		OnRename: func(threadID domain.ThreadID) {
			retained := renameCommand.Get()
			if retained.Key != "" && retained.ThreadID != threadID {
				transportMessage.Set("Retry or resolve the retained rename before renaming another thread.")
				return
			}
			if row, ok := mountedThreadRailRow(resolvedRailState(), threadID); ok {
				renameTarget.Set(threadID)
				if retained.Key != "" {
					renameTitle.Set(retained.Title)
				} else {
					renameTitle.Set(row.Title())
				}
			}
		},
		ArchiveConfirmation: archiveConfirmation.Get(),
		OnArchiveRequest:    archiveConfirmation.Set,
		OnArchiveCancel: func() {
			cancelMountedThreadArchive(
				func() { archiveConfirmation.Set(domain.ThreadID{}) },
				func() { ui.PostAsync(func() { focusManager.FocusByID("thread-rail-archive") }) },
			)
		},
		OnArchive: func(threadID domain.ThreadID, archived bool) {
			if archiveBusy.Get() {
				return
			}
			row, ok := mountedThreadRailRow(resolvedRailState(), threadID)
			if !ok {
				return
			}
			command := archiveCommand.Get()
			if command.Key != "" && (command.ThreadID != threadID || command.Archived != archived) {
				transportMessage.Set("Retry or resolve the retained archive command before changing another thread.")
				return
			}
			if command.Key == "" {
				key, err := newThreadRailCommandKey("archive-thread")
				if err != nil {
					return
				}
				command = threadrail.ArchiveCommand{
					Key: key, ThreadID: threadID, Archived: archived, Confirmed: true,
					ExpectedRevision: row.Thread().Revision(),
				}
				archiveCommand.Set(command)
			}
			runRemote("archive authoritative thread", archiveBusy.Set, func(ctx context.Context, state threadrail.State, client threadrail.PageClient) (threadrail.State, error) {
				next, commandErr := threadrail.ArchiveThread(ctx, state, client, command)
				if commandErr == nil {
					archiveConfirmation.Set(domain.ThreadID{})
					archiveCommand.Set(threadrail.ArchiveCommand{})
				}
				return next, commandErr
			})
		},
	})

	children := []ui.Node{
		// Silence is the healthy state. A permanent banner saying threads are
		// synchronized occupied the top of the rail with a fact that is true
		// almost always and interesting almost never; it now speaks only when
		// something is loading, retained, or refused.
		threadRailTransportNotice("authoritative-bridge", noticeWhenNotable(message), props.Mode),
		rail,
	}
	if !renameTarget.Get().IsZero() {
		children = append(children, mountedThreadRailRenameEditor(
			props.Mode,
			renameTitle.Get(),
			renameTitle.Set,
			renameBusy.Get(),
			renameCommand.Get().Key != "",
			func() {
				if renameBusy.Get() {
					return
				}
				title := strings.TrimSpace(renameTitle.Get())
				row, ok := mountedThreadRailRow(resolvedRailState(), renameTarget.Get())
				if !ok || title == "" {
					return
				}
				command := renameCommand.Get()
				if command.Key == "" {
					key, err := newThreadRailCommandKey("rename-thread")
					if err != nil {
						return
					}
					command = threadrail.RenameCommand{
						Key: key, ThreadID: renameTarget.Get(), Title: title,
						ExpectedRevision: row.Thread().Revision(),
					}
					renameCommand.Set(command)
				}
				runRemote("rename authoritative thread", renameBusy.Set, func(ctx context.Context, state threadrail.State, client threadrail.PageClient) (threadrail.State, error) {
					next, commandErr := threadrail.RenameThread(ctx, state, client, command)
					if commandErr == nil {
						renameTarget.Set(domain.ThreadID{})
						renameTitle.Set("")
						renameCommand.Set(threadrail.RenameCommand{})
					}
					return next, commandErr
				})
			},
			func() {
				if renameCommand.Get().Key != "" {
					transportMessage.Set("The retained rename must be retried before this editor can close.")
					return
				}
				cancelMountedThreadRename(
					resolvedRailState(), renameTarget.Get(), props.OnAuthoritativeSelection,
					func() {
						renameTarget.Set(domain.ThreadID{})
						renameTitle.Set("")
					},
					func() { ui.PostAsync(func() { focusManager.FocusByID("thread-rail-rename") }) },
				)
			},
		))
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Authoritative thread navigation"},
		Data: map[string]string{
			"component": "mounted-thread-rail", "transport-mode": "authoritative-bridge",
			"authority-state": string(props.Snapshot.Session.Connection),
		},
	}, children...)
}

func threadRailFallback(props mountedThreadRailProps, reason string) ui.Node {
	if strings.TrimSpace(reason) == "" {
		reason = "Authoritative thread state is unavailable."
	}
	if props.Snapshot.Session.Connection == frontendstate.ConnectionDisconnected {
		reason = "The coordinator is disconnected. " + reason
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Local thread preview fallback"},
		Data: map[string]string{
			"component": "mounted-thread-rail", "transport-mode": "local-preview-fallback",
			"authority-state": string(props.Snapshot.Session.Connection),
		},
	},
		threadRailTransportNotice("local-preview-fallback", "Local preview · "+reason, props.Mode),
		ui.CreateElement(shell.TypedThreadRailPreview, shell.TypedThreadRailPreviewProps{
			Snapshot: props.Snapshot, Route: props.Route, Mode: props.Mode, OnNavigate: props.OnNavigate,
		}),
	)
}

func threadRailFallbackReason(err error) string {
	switch {
	case errors.Is(err, errThreadRailAuthorizedWorkspaceUnavailable):
		return "The coordinator did not provide an authorized workspace identity."
	case errors.Is(err, errThreadRailAuthorizedRepositoryUnavailable):
		return "The coordinator did not provide one authorized repository scope."
	case err != nil:
		return "The authoritative thread scope is invalid."
	default:
		return ""
	}
}

func isThreadRailServiceUnavailable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errThreadRailBridgeUnavailable) ||
		status.Code(err) == codes.Unimplemented || status.Code(err) == codes.Unavailable
}

// noticeWhenNotable drops the two messages that only confirm the ordinary.
func noticeWhenNotable(message string) string {
	switch strings.TrimSpace(message) {
	case "Threads are synchronized with the local coordinator.",
		"Thread changes are synchronized with the local coordinator.":
		return ""
	default:
		return message
	}
}

func threadRailTransportNotice(mode, message string, primitiveMode primitives.Mode) ui.Node {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	tokens := primitiveMode.Tokens()
	return html.P(html.Props{
		Role: "status", Text: message,
		Data: map[string]string{"component": "thread-rail-transport-status", "transport-mode": mode},
		Class: css.New(
			css.Margin(css.Zero), css.MarginY(css.Px(tokens.Spacing.SM)),
			css.PaddingX(css.Px(tokens.Spacing.SM)), css.PaddingY(css.Px(tokens.Spacing.XS)),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface2))),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		).String(),
	})
}

func mountedThreadRailRenameEditor(
	mode primitives.Mode,
	title string,
	onChange func(string),
	busy bool,
	retained bool,
	onSave func(),
	onCancel func(),
) ui.Node {
	tokens := mode.Tokens()
	inputProps := html.PropsOf(html.OnInput(func(event ui.InputEvent) {
		if onChange != nil {
			onChange(event.GetValue())
		}
	}))
	inputProps.ID = "thread-rail-rename-title"
	inputProps.Value = title
	inputProps.Disabled = busy || retained
	inputProps.Aria = map[string]string{"label": "New thread title"}
	inputProps.Class = css.New(
		css.W(css.Full), css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	).String()
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Rename selected thread"},
		Data:  map[string]string{"component": "thread-rail-rename-editor"},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)), css.MarginY(css.Px(tokens.Spacing.SM))).String(),
	},
		html.Label(html.Props{For: "thread-rail-rename-title", Text: "Thread title"}),
		html.Input(inputProps),
		html.Div(html.Props{Class: css.New(u.Flex, css.Gap(css.Px(tokens.Spacing.SM))).String()},
			primitives.Button(primitives.ButtonProps{
				Label: map[bool]string{true: "Retry title change", false: "Save title"}[retained], Primary: true, Busy: busy,
				Disabled: busy || strings.TrimSpace(title) == "", Mode: mode, OnClick: onSave,
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Cancel rename", Disabled: busy || retained, Mode: mode, OnClick: onCancel,
			}),
		),
	)
}
