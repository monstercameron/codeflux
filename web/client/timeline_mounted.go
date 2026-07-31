//go:build js && wasm

package main

import (
	"context"
	"sync/atomic"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"codeflux.dev/codeflux/web/frontend/timeline"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const mountedTimelineTimeout = 5 * time.Second

func mountedAuthoritativeTimeline(
	thread threadrail.Thread,
	fallback shell.TimelineControlProps,
	onEvent func(events.SessionEvent),
) shell.TimelineControlProps {
	feedState := ui.UseState(timeline.MessageFeed{})
	eventState := ui.UseState(timeline.State{})
	pageBusy := ui.UseState(false)
	streamStatus := ui.UseState(sessionclient.Status{})

	dependency := thread.ID().String() + "|" + thread.SessionID().String()
	ui.UseEffectOf(func() func() {
		if thread.ID().IsZero() || thread.SessionID().IsZero() {
			return nil
		}
		feedState.Set(timeline.MessageFeed{})
		eventState.Set(timeline.State{})
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
				feedState.Set(feed)
			}
		})
		identity := &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION,
			Value: thread.SessionID().String(),
		}
		client, err := sessionclient.New(sessionclient.Config{
			Connector: sessionclient.BrowserConnector{}, SessionID: identity,
			Observe: func(status sessionclient.Status) {
				if mounted.Load() {
					streamStatus.Set(status)
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
				next, mergeErr := timeline.MergeThreadPage(eventState.Get(), timeline.Page{Events: []events.SessionEvent{event}})
				if mergeErr != nil {
					return mergeErr
				}
				if mounted.Load() {
					eventState.Set(next)
					if onEvent != nil {
						onEvent(event)
					}
				}
				return nil
			},
		})
		if err == nil {
			if err = client.Start(ctx); err != nil {
				streamStatus.Set(sessionclient.Status{State: sessionclient.StateFailed, Failure: sessionclient.FailureProtocol})
			}
		} else {
			streamStatus.Set(sessionclient.Status{State: sessionclient.StateFailed, Failure: sessionclient.FailureProtocol})
		}
		return func() {
			mounted.Store(false)
			cancel()
			if client != nil {
				_ = client.Close()
			}
		}
	}, dependency)

	if thread.ID().IsZero() || thread.SessionID().IsZero() {
		return fallback
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
			feedState.Set(next)
		})
	}
	props.OnRetryOlder = props.OnLoadOlder
	_ = streamStatus.Get()
	return props
}
