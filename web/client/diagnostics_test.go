package main

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/sessionprojection"
)

func TestDiagnosticsViewForSessionProjectionPreservesUnknownAndKnownZero(t *testing.T) {
	unknown := diagnosticsViewForSessionProjection(
		sessionprojection.Diagnostics{LastAppliedSequence: 91},
		sessionprojection.ConnectionProjection{State: sessionprojection.ConnectionLive},
		false,
	)
	if unknown.LastAppliedSequenceKnown || unknown.LastAppliedSequence != 0 ||
		unknown.SessionLive || unknown.SessionReplayActive || unknown.SessionGapRepairRequired {
		t.Fatalf("unknown diagnostics = %+v", unknown)
	}

	knownZero := diagnosticsViewForSessionProjection(
		sessionprojection.Diagnostics{},
		sessionprojection.ConnectionProjection{State: sessionprojection.ConnectionReplaying},
		true,
	)
	if !knownZero.LastAppliedSequenceKnown || knownZero.LastAppliedSequence != 0 ||
		!knownZero.SessionReplayActive || knownZero.SessionLive || knownZero.SessionGapRepairRequired {
		t.Fatalf("known-zero diagnostics = %+v", knownZero)
	}
}

func TestDiagnosticsViewForSessionProjectionClassifiesLiveAndGapRepair(t *testing.T) {
	live := diagnosticsViewForSessionProjection(
		sessionprojection.Diagnostics{LastAppliedSequence: 42},
		sessionprojection.ConnectionProjection{State: sessionprojection.ConnectionLive},
		true,
	)
	if live.LastAppliedSequence != 42 || !live.SessionLive || live.SessionReplayActive || live.SessionGapRepairRequired {
		t.Fatalf("live diagnostics = %+v", live)
	}

	gap := diagnosticsViewForSessionProjection(
		sessionprojection.Diagnostics{
			LastAppliedSequence: 42,
			Repair: &sessionprojection.SnapshotRepairRequest{
				Reason: sessionprojection.RepairSequenceGap, AfterSequence: 42,
			},
		},
		sessionprojection.ConnectionProjection{State: sessionprojection.ConnectionReplaying},
		true,
	)
	if gap.LastAppliedSequence != 42 || !gap.SessionGapRepairRequired || gap.SessionLive || gap.SessionReplayActive {
		t.Fatalf("gap diagnostics = %+v", gap)
	}
}
