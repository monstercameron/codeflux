package main

import (
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/web/frontend/timelinecard"
)

func TestApprovalTransportFailureNeverInventsACommittedResolution(t *testing.T) {
	state := approvalPreviewState{
		Approval: timelinecard.Approval{ID: "approval", State: "pending"},
		Busy:     true, CommandKey: "retained-key", TransportMode: "authoritative-bridge",
	}
	next, committed, err := settleApprovalPreview(
		state, timelinecard.ApprovalAllowOnce, errors.New("synthetic unavailable"), time.Now().UTC(),
	)
	if err != nil || committed || next.Busy || next.Approval.State != "pending" ||
		next.CommandKey != state.CommandKey || next.TransportMode != "local-preview-fallback" {
		t.Fatalf("failed approval settlement = %#v, committed=%t, error=%v", next, committed, err)
	}
}
