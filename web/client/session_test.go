package main

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/sessionclient"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
)

func TestSessionViewForLifecycleKeepsUncertainControlsOfflineOrRecovering(t *testing.T) {
	tests := []struct {
		status sessionclient.Status
		want   frontendstate.ConnectionState
	}{
		{status: sessionclient.Status{State: sessionclient.StateConnecting}, want: frontendstate.ConnectionConnecting},
		{status: sessionclient.Status{State: sessionclient.StateReplaying}, want: frontendstate.ConnectionReplaying},
		{status: sessionclient.Status{State: sessionclient.StateReconnecting}, want: frontendstate.ConnectionDegraded},
		{status: sessionclient.Status{State: sessionclient.StateGap}, want: frontendstate.ConnectionDegraded},
		{status: sessionclient.Status{State: sessionclient.StateLive}, want: frontendstate.ConnectionLive},
		{status: sessionclient.Status{State: sessionclient.StateFailed, Failure: sessionclient.FailureAuthentication}, want: frontendstate.ConnectionUnauthorized},
		{status: sessionclient.Status{State: sessionclient.StateFailed, Failure: sessionclient.FailureIncompatible}, want: frontendstate.ConnectionIncompatible},
		{status: sessionclient.Status{State: sessionclient.StateFailed, Failure: sessionclient.FailureUnavailable}, want: frontendstate.ConnectionDisconnected},
	}
	for _, test := range tests {
		got := sessionViewForLifecycle(test.status)
		if got.Bootstrap != frontendstate.BootstrapReady || got.Connection != test.want {
			t.Fatalf("status %+v mapped to %+v, want %q", test.status, got, test.want)
		}
	}
}
