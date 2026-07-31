package main

import (
	"time"

	"codeflux.dev/codeflux/web/frontend/timelinecard"
)

type approvalPreviewState struct {
	Approval      timelinecard.Approval
	Busy          bool
	CommandKey    string
	TransportMode string
}

func settleApprovalPreview(
	state approvalPreviewState,
	action timelinecard.ApprovalAction,
	transportErr error,
	resolvedAt time.Time,
) (approvalPreviewState, bool, error) {
	state.Busy = false
	if transportErr != nil {
		state.TransportMode = "local-preview-fallback"
		return state, false, nil
	}
	resolutionState := "granted"
	if action == timelinecard.ApprovalDeny {
		resolutionState = "denied"
	}
	resolved, _, err := timelinecard.ResolveApproval(state.Approval, timelinecard.ApprovalResolution{
		State: resolutionState, ResolvedBy: "local user", ResolvedAt: resolvedAt,
	})
	if err != nil {
		return state, false, err
	}
	state.Approval = resolved
	state.TransportMode = "authoritative-bridge"
	return state, true, nil
}
