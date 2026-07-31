package main

import (
	"codeflux.dev/codeflux/web/frontend/sessionprojection"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
)

// diagnosticsViewForSessionProjection exposes only the durable cursor and its
// content-free delivery classification. A zero cursor is meaningful only when
// an authoritative mounted projection is present; otherwise it remains unknown.
func diagnosticsViewForSessionProjection(
	diagnostics sessionprojection.Diagnostics,
	connection sessionprojection.ConnectionProjection,
	known bool,
) frontendstate.DiagnosticsView {
	view := frontendstate.DiagnosticsView{
		State:                    frontendstate.DataReady,
		LastAppliedSequence:      diagnostics.LastAppliedSequence,
		LastAppliedSequenceKnown: known,
	}
	if !known {
		view.LastAppliedSequence = 0
		return view
	}
	view.SessionGapRepairRequired = diagnostics.Repair != nil
	if view.SessionGapRepairRequired {
		return view
	}
	view.SessionReplayActive = connection.State == sessionprojection.ConnectionReplaying
	view.SessionLive = connection.State == sessionprojection.ConnectionLive
	return view
}
