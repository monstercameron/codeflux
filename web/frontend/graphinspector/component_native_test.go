//go:build !js

package graphinspector

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestNativeInspectorDispatchesOnlyEnabledTypedActions(t *testing.T) {
	props := inspectorFixture(t)
	nodeCalls := make(map[ActionKind]domain.NodeID)
	var compared domain.GraphRevisionID
	var openedPath string
	var openedLine uint32
	props.OnExplainInChat = func(id domain.NodeID) { nodeCalls[ActionExplainInChat] = id }
	props.OnExpandNeighbors = func(id domain.NodeID) { nodeCalls[ActionExpandNeighbors] = id }
	props.OnIsolateDependencyCone = func(id domain.NodeID) { nodeCalls[ActionDependencyCone] = id }
	props.OnIsolateEvidenceCone = func(id domain.NodeID) { nodeCalls[ActionEvidenceCone] = id }
	props.OnCompareRevision = func(id domain.GraphRevisionID) { compared = id }
	props.OnOpenInEditor = func(path string, line uint32) { openedPath, openedLine = path, line }

	for _, kind := range []ActionKind{
		ActionExplainInChat, ActionExpandNeighbors, ActionDependencyCone,
		ActionEvidenceCone, ActionCompareRevision,
	} {
		if !ActivateAction(props, kind) {
			t.Fatalf("enabled native action %s was not dispatched", kind)
		}
	}
	for _, kind := range []ActionKind{
		ActionExplainInChat, ActionExpandNeighbors, ActionDependencyCone, ActionEvidenceCone,
	} {
		if nodeCalls[kind] != props.Node.ID() {
			t.Fatalf("action %s node = %s", kind, nodeCalls[kind])
		}
	}
	if compared != props.RevisionID {
		t.Fatalf("compared revision = %s", compared)
	}
	if !ActivateSource(props, 0) || openedPath != "internal/graph/projector.go" || openedLine != 42 {
		t.Fatalf("validated editor dispatch = %q:%d", openedPath, openedLine)
	}

	props.Actions.EvidenceCone = ActionAvailability{Enabled: true, Busy: true}
	delete(nodeCalls, ActionEvidenceCone)
	if ActivateAction(props, ActionEvidenceCone) || !nodeCalls[ActionEvidenceCone].IsZero() {
		t.Fatal("busy evidence action dispatched")
	}
	props.Actions.OpenInEditor = ActionAvailability{DisabledReason: "Editor handoff denied by policy"}
	if ActivateSource(props, 0) {
		t.Fatal("disabled editor handoff dispatched")
	}
	if ActivateSource(props, -1) || ActivateSource(props, len(props.SourceLocations)) {
		t.Fatal("out-of-range editor handoff dispatched")
	}
}

func TestNativeInspectorRejectsUnknownActionKind(t *testing.T) {
	if ActivateAction(inspectorFixture(t), ActionKind("invented")) {
		t.Fatal("unknown inspector action dispatched")
	}
}
