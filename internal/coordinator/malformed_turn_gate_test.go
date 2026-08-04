package coordinator

import "testing"

// TestAMalformedTurnIsNotALostBuild is the mislabel that bought a dearer model
// for a protocol slip.
//
// A malformed turn was sent back under the assembly gate, which means the code
// did not compile. assembly is one of the regression-prone gates: failing it
// twice halves the stall threshold, because losing a build you already had is
// going round rather than going slowly. A malformed turn is neither. The code
// may compile perfectly; what failed is the protocol — two writes to one file
// in a turn, a call attributed to a step that cannot accept it.
//
// So a run that slipped twice escalated to a more expensive model, which is the
// same mistake the infrastructure path was separated out to avoid: money spent
// on the wrong remedy. The lesson written for the project said the build broke,
// which it had not.
func TestAMalformedTurnIsNotALostBuild(t *testing.T) {
	const gate = "model-turn"
	if regressionProneGates[gate] {
		t.Errorf("%s is treated as a lost property, so a second protocol slip "+
			"escalates the model", gate)
	}
	// assembly keeps its meaning, which is the reason this needed its own name.
	if !regressionProneGates["assembly"] {
		t.Error("assembly is a build that was had and lost; it belongs in the " +
			"regression-prone set")
	}
}

// TestAMalformedTurnStillGetsAWideScope is the control.
//
// Renaming the gate must not narrow what the next attempt may touch. A protocol
// slip says nothing about which files are at fault, so the next attempt needs
// the same freedom the assembly gate gave it.
func TestAMalformedTurnStillGetsAWideScope(t *testing.T) {
	if scope := scopeOfNextAttempt("model-turn", false); scope != editAnything {
		t.Errorf("a protocol slip restricted the next attempt to %s", scope)
	}
}

// TestAMalformedTurnIsWorthEscalating keeps it out of the other exemption.
//
// escalationWouldNotHelp names the gates a dearer model cannot satisfy any
// better — the ones asking for text the run has already been given. A malformed
// turn is not one of those: honouring a tool protocol is exactly the kind of
// thing a stronger model does more reliably, so it should still escalate, just
// at the ordinary threshold rather than the halved one.
func TestAMalformedTurnIsWorthEscalating(t *testing.T) {
	if escalationWouldNotHelp["model-turn"] {
		t.Error("a protocol slip is worth a stronger model; it is not a " +
			"transcription task")
	}
}
