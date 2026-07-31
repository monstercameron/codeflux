package shell

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/state"
)

func TestOfflineConnectionSeversDurableComposerCallbacksButKeepsDraftEditing(t *testing.T) {
	textChanges := 0
	sends := 0
	retries := 0
	props := composer.Props{
		TransportMode:     "authoritative-bridge-with-local-preview-fallback",
		OnTextChange:      func(string) { textChanges++ },
		OnSubmitRequested: func() { sends++ },
		OnRetryRequested:  func(composer.IdempotencyKey) { retries++ },
	}

	gated := composerPropsForConnection(props, state.ConnectionDisconnected)
	if !gated.MutationDisabled {
		t.Fatal("offline connection did not mark the composer mutation-disabled")
	}
	if gated.MutationDisabledReason != "Local Disconnected: reconnect to send this draft" {
		t.Fatalf("offline reason = %q", gated.MutationDisabledReason)
	}
	if gated.OnSubmitRequested != nil || gated.OnRetryRequested != nil {
		t.Fatal("offline connection retained a durable send callback")
	}
	if gated.OnTextChange == nil {
		t.Fatal("offline connection removed browser-local draft editing")
	}
	gated.OnTextChange("still editable")
	if textChanges != 1 || sends != 0 || retries != 0 {
		t.Fatalf("offline callbacks = text %d, sends %d, retries %d", textChanges, sends, retries)
	}
}

func TestLiveConnectionRetainsComposerCallbacks(t *testing.T) {
	submit := func() {}
	props := composer.Props{OnSubmitRequested: submit}
	gated := composerPropsForConnection(props, state.ConnectionLive)
	if gated.MutationDisabled || gated.OnSubmitRequested == nil {
		t.Fatal("live connection unexpectedly gated composer submission")
	}
}
