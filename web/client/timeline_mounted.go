//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/sessionprojection"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"codeflux.dev/codeflux/web/frontend/timeline"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const mountedTimelineTimeout = 5 * time.Second

type mountedTimelineSource struct {
	Props     shell.TimelineControlProps
	Task      taskprojection.TaskProjection
	TaskReady bool
	// Events is the ordered durable event state, which the execution panels
	// project into steps and log lines. It is exposed rather than re-fetched
	// so both surfaces read the same events in the same order.
	Events             timeline.State
	SessionReady       bool
	SessionConnection  sessionprojection.ConnectionProjection
	SessionDiagnostics sessionprojection.Diagnostics
}

func mountedAuthoritativeTimeline(
	thread threadrail.Thread,
	fallback shell.TimelineControlProps,
	reconnectVersion uint64,
	requestedEventID *domain.EventID,
	onEvent func(events.SessionEvent),
	onStatus func(sessionclient.Status),
) mountedTimelineSource {
	feedState := ui.UseState(timeline.MessageFeed{})
	eventState := ui.UseState(timeline.State{})
	pageBusy := ui.UseState(false)
	reviewOpen := ui.UseState(false)
	streamStatus := ui.UseState(sessionclient.Status{})
	projectionState := ui.UseState(sessionprojection.New())
	decisionState := ui.UseState(mountedReviewMutationState{})
	decisionRef := ui.UseRef(mountedReviewMutationState{})
	// The review material is loaded here as well as in the drawer, because the
	// decision controls live in the timeline and must carry the exact review,
	// report, and diff identity a decision is bound to. The hook is called
	// unconditionally and returns nothing when the task is not in review: GWC
	// hooks are positional, so a conditional call would misalign every hook
	// after it.
	decisionResource := fetch.UseResource(
		func(parent context.Context) (reviewMutationScope, error) {
			if thread.TaskID().IsZero() {
				return reviewMutationScope{}, nil
			}
			ctx, cancel := context.WithTimeout(parent, mountedTimelineTimeout)
			defer cancel()
			resource, err := loadReviewResource(
				ctx, openBrowserReviewResourceClient, thread.TaskID())
			if err != nil {
				// A review that cannot be loaded leaves the decisions refused,
				// which is what the projection already says. Surfacing the load
				// error here would replace an accurate refusal with a transient
				// one.
				return reviewMutationScope{}, nil
			}
			return resource.DecisionScope(thread.TaskID()), nil
		},
		thread.TaskID().String(),
	)

	// The task is part of the dependency because the session snapshot is
	// scoped to one, and it is fetched once when this mounts. A thread that
	// gained a task afterwards kept the snapshot taken before the task existed,
	// so a worker could be running with a real worktree while the workspace
	// still read "No task yet" — the interface disagreeing with the machine it
	// supervises.
	dependency := thread.ID().String() + "|" + thread.SessionID().String() + "|" +
		thread.TaskID().String() + "|" +
		strconv.FormatUint(reconnectVersion, 10)
	ui.UseEffectOf(func() func() {
		if thread.ID().IsZero() || thread.SessionID().IsZero() {
			return nil
		}
		base := projectionState.Get()
		trusted := base.Snapshot().Session
		if trusted.SessionID != thread.SessionID() || trusted.ThreadID != thread.ID() {
			feedState.Set(timeline.MessageFeed{})
			eventState.Set(timeline.State{})
			reviewOpen.Set(false)
			base = sessionprojection.New()
		}
		projectionState.Set(base)
		ctx, cancel := context.WithCancel(context.Background())
		var mounted atomic.Bool
		mounted.Store(true)
		ui.SafeGo("load authoritative timeline page", func() {
			pageCtx, pageCancel := context.WithTimeout(ctx, mountedTimelineTimeout)
			defer pageCancel()
			lease, err := openBrowserTimelinePageClient(pageCtx, thread.ID())
			if err != nil {
				return
			}
			feed, err := fetchNewestTimelinePage(pageCtx, lease)
			if err == nil && mounted.Load() {
				ui.PostAsync(func() { feedState.Set(feed) })
			}
		})
		retryPolicy := sessionclient.NormalizedRetryPolicy(sessionclient.RetryPolicy{})
		localProjection := base
		localEvents := eventState.Get()
		refreshProjection := func(parent context.Context) (uint64, error) {
			repairContext, repairCancel := context.WithTimeout(parent, mountedTimelineTimeout)
			defer repairCancel()
			lease, repairErr := openBrowserSessionProjectionSnapshotClient(repairContext)
			if repairErr != nil {
				return 0, repairErr
			}
			defer lease.close()
			snapshot, repairErr := fetchSessionProjectionSnapshot(
				repairContext, lease.client, thread.SessionID(), thread.ID(), thread.TaskID(),
			)
			if repairErr != nil {
				return 0, repairErr
			}
			repaired, repairErr := sessionprojection.ApplySessionSnapshot(localProjection, snapshot)
			if repairErr != nil {
				return 0, repairErr
			}
			localProjection = repaired
			if mounted.Load() {
				projection := repaired
				ui.PostAsync(func() { projectionState.Set(projection) })
			}
			return repaired.SubscriptionAfterSequence(), nil
		}
		publishFailure := func(kind sessionclient.FailureKind) {
			failed := sessionclient.Status{State: sessionclient.StateFailed, Failure: kind}
			localProjection = sessionprojection.ProjectConnection(localProjection, failed, retryPolicy)
			if mounted.Load() {
				projection := localProjection
				ui.PostAsync(func() {
					streamStatus.Set(failed)
					projectionState.Set(projection)
					if onStatus != nil {
						onStatus(failed)
					}
				})
			}
		}
		var clientMu sync.Mutex
		var client *sessionclient.Client
		ui.SafeGo("bootstrap authoritative session snapshot", func() {
			if _, err := refreshProjection(ctx); err != nil {
				publishFailure(sessionclient.FailureApplication)
				return
			}
			identity := &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION,
				Value: thread.SessionID().String(),
			}
			created, err := sessionclient.New(sessionclient.Config{
				Connector: sessionclient.BrowserConnector{}, SessionID: identity,
				AfterSequence: localProjection.SubscriptionAfterSequence(), Retry: retryPolicy,
				Repair: refreshProjection,
				Observe: func(status sessionclient.Status) {
					localProjection = sessionprojection.ProjectConnection(localProjection, status, retryPolicy)
					if mounted.Load() {
						projection := localProjection
						ui.PostAsync(func() {
							streamStatus.Set(status)
							projectionState.Set(projection)
							if onStatus != nil {
								onStatus(status)
							}
						})
					}
				},
				Apply: func(_ context.Context, value *codefluxv1.SessionEvent) error {
					event, decodeErr := sessionclient.DecodeEvent(value)
					if decodeErr != nil {
						return decodeErr
					}
					if event.ThreadID != thread.ID() || event.SessionID != thread.SessionID() {
						return sessionclient.ErrSessionEventIdentityMismatch
					}
					nextProjection, projectionErr := sessionprojection.ApplySessionEvent(localProjection, event)
					localProjection = nextProjection
					if projectionErr != nil {
						if mounted.Load() {
							projection := localProjection
							ui.PostAsync(func() { projectionState.Set(projection) })
						}
						return projectionErr
					}
					next, mergeErr := timeline.MergeThreadPage(localEvents, timeline.Page{Events: []events.SessionEvent{event}})
					if mergeErr != nil {
						return mergeErr
					}
					localEvents = next
					if mounted.Load() {
						projection := localProjection
						ui.PostAsync(func() {
							eventState.Set(next)
							projectionState.Set(projection)
							if onEvent != nil {
								onEvent(event)
							}
						})
					}
					return nil
				},
			})
			if err != nil {
				publishFailure(sessionclient.FailureProtocol)
				return
			}
			clientMu.Lock()
			if !mounted.Load() {
				clientMu.Unlock()
				return
			}
			client = created
			clientMu.Unlock()
			if err = created.Start(ctx); err != nil {
				publishFailure(sessionclient.FailureProtocol)
			}
		})
		return func() {
			mounted.Store(false)
			cancel()
			clientMu.Lock()
			active := client
			clientMu.Unlock()
			if active != nil {
				_ = active.Close()
			}
		}
	}, dependency)

	if thread.ID().IsZero() || thread.SessionID().IsZero() {
		return mountedTimelineSource{Props: fallback}
	}
	feed := feedState.Get()
	stream := eventState.Get()
	props := fallback
	props.Authoritative = true
	props.Enabled = true
	props.Cards = authoritativeTimelineCards(thread, feed, stream)
	props.HasOlder = feed.ThreadID == thread.ID() && feed.HasOlder
	props.LoadingOlder = pageBusy.Get() || feed.LoadingOlder
	props.OlderError = feed.SafeError
	props.Gaps = stream.Gaps
	props.Actions = fallback.Actions
	props.Actions.OnApproval = nil
	props.Actions.ApprovalCommand = nil
	props.OnOpenReview = nil
	props.OnCloseReview = nil
	props.SelectedStableKey = ""
	props.SelectionNotice = ""
	if requestedEventID != nil {
		if stableKey, ok := relatedTimelineStableKey(stream, *requestedEventID); ok {
			props.SelectedStableKey = stableKey
		} else {
			props.SelectionNotice = "The related recovery event is not present in the loaded authoritative timeline."
		}
	}
	props.OnLoadOlder = func() {
		if pageBusy.Get() || feed.ThreadID != thread.ID() || !feed.HasOlder {
			return
		}
		pageBusy.Set(true)
		ui.SafeGo("load older authoritative timeline page", func() {
			ctx, cancel := context.WithTimeout(context.Background(), mountedTimelineTimeout)
			defer cancel()
			lease, err := openBrowserTimelinePageClient(ctx, thread.ID())
			if err != nil {
				pageBusy.Set(false)
				return
			}
			next, err := fetchOlderTimelinePage(ctx, feedState.Get(), lease)
			pageBusy.Set(false)
			if err != nil {
				// fetchOlderTimelinePage already marks the common failure path
				// (the remote fetch itself failing) with a retryable SafeError.
				// But a page-merge failure — e.g. ErrPageScope when the cursor
				// moved between read and merge — instead returns the in-flight
				// "loading" feed unchanged, with LoadingOlder still true and no
				// SafeError. Nothing else ever clears that flag, and the next
				// pagination attempt would be rejected by BeginOlderMessagePage's
				// LoadingOlder guard, wedging "load older" permanently with no
				// visible error. Force a definite failed state here so the error
				// is never silently dropped and the control never gets stuck.
				feedState.Set(timeline.FailOlderMessagePage(next))
				return
			}
			feedState.Set(next)
		})
	}
	props.OnRetryOlder = props.OnLoadOlder
	_ = streamStatus.Get()
	projection := projectionState.Get()
	task, taskReady := projection.TaskProjection()
	if taskReady {
		props = bindAuthoritativeTimelineActions(
			props, task, taskprojection.ConnectionProjection(projection.Connection().State),
			func() { reviewOpen.Set(true) }, func() { reviewOpen.Set(false) },
			mountedReviewDecisionBinder(
				decisionResource.Get().Value, decisionState, decisionRef,
				decisionResource.Reload,
			),
		)
		props.ReviewOpen = reviewOpen.Get()
	}
	trusted := projection.Snapshot().Session
	sessionReady := trusted.SessionID == thread.SessionID() && trusted.ThreadID == thread.ID()
	return mountedTimelineSource{
		Props: props, Task: task, TaskReady: taskReady, Events: eventState.Get(),
		SessionReady: sessionReady, SessionConnection: projection.Connection(),
		SessionDiagnostics: projection.Diagnostics(),
	}
}
