package main

import (
	"context"
	"errors"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
)

var errInvalidSelectedSession = errors.New("frontend bootstrap selected session is invalid")

func selectedSessionIdentity(envelope bootstrapEnvelope) (*codefluxv1.StableIdentity, error) {
	identity := envelope.SelectedSessionID
	if identity == nil {
		return nil, nil
	}
	if identity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION {
		return nil, errInvalidSelectedSession
	}
	if _, err := domain.ParseSessionID(identity.GetValue()); err != nil {
		return nil, errInvalidSelectedSession
	}
	return &codefluxv1.StableIdentity{Kind: identity.GetKind(), Value: identity.GetValue()}, nil
}

func startAuthenticatedSession(
	ctx context.Context,
	envelope bootstrapEnvelope,
	connector sessionclient.Connector,
	observe func(sessionclient.Status),
	apply func(context.Context, *codefluxv1.SessionEvent) error,
) (*sessionclient.Client, error) {
	identity, err := selectedSessionIdentity(envelope)
	if err != nil || identity == nil {
		return nil, err
	}
	client, err := sessionclient.New(sessionclient.Config{
		Connector: connector,
		SessionID: identity,
		Apply:     apply,
		Observe:   observe,
	})
	if err != nil {
		return nil, err
	}
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func sessionViewForLifecycle(status sessionclient.Status) frontendstate.SessionView {
	view := frontendstate.SessionView{Bootstrap: frontendstate.BootstrapReady}
	switch status.State {
	case sessionclient.StateLive:
		view.Connection = frontendstate.ConnectionLive
	case sessionclient.StateConnecting:
		view.Connection = frontendstate.ConnectionConnecting
		view.Message = "Connecting to live session updates."
	case sessionclient.StateReplaying:
		view.Connection = frontendstate.ConnectionReplaying
		view.Message = "Replaying committed session updates."
	case sessionclient.StateReconnecting, sessionclient.StateGap:
		view.Connection = frontendstate.ConnectionDegraded
		view.Message = "Live session updates are reconnecting."
	case sessionclient.StateFailed:
		switch status.Failure {
		case sessionclient.FailureAuthentication:
			view.Connection = frontendstate.ConnectionUnauthorized
			view.Message = "The local session is no longer authorized. Reload to authenticate again."
		case sessionclient.FailureIncompatible:
			view.Connection = frontendstate.ConnectionIncompatible
			view.Message = "The frontend and coordinator stream contracts do not match."
		default:
			view.Connection = frontendstate.ConnectionDisconnected
			view.Message = "Live session updates are unavailable."
		}
	default:
		// A selected thread whose stream has not reported yet is opening, not
		// disconnected. Reporting the idle moment as a fault made every page
		// load flash "Local Disconnected" and dead-lock the composer with
		// "reconnect to send this draft" for the two seconds before the first
		// status arrived.
		view.Connection = frontendstate.ConnectionConnecting
		view.Message = "Opening the live session."
	}
	return view
}
