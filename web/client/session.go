package main

import (
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
)

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
