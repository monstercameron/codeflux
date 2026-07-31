package graphcanvas

import (
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
)

type AuthoritativeInteraction struct {
	Viewport         Viewport
	SelectedNodeID   domain.NodeID
	Mode             graph.Mode
	UserSelectedMode bool
	RevisionID       domain.GraphRevisionID
}

func DefaultGraphMode(state domain.TaskState) graph.Mode {
	switch state {
	case domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval,
		domain.TaskStateReady, domain.TaskStateRunning, domain.TaskStateAwaitingAuthority,
		domain.TaskStatePaused, domain.TaskStateValidating, domain.TaskStateRecoveryRequired:
		return graph.ModeExecution
	case domain.TaskStateAwaitingReview, domain.TaskStateCompleted, domain.TaskStateFailed,
		domain.TaskStateCancelled, domain.TaskStateRolledBack:
		return graph.ModeEvidence
	default:
		return graph.ModeProgram
	}
}

func InitialGraphMode(requested graph.Mode, userSelected bool, state domain.TaskState) graph.Mode {
	if userSelected && requested.IsValid() {
		return requested
	}
	return DefaultGraphMode(state)
}

func SynchronizeAuthoritativeMode(
	current AuthoritativeInteraction,
	requested graph.Mode,
	userSelected bool,
	state domain.TaskState,
) AuthoritativeInteraction {
	current.UserSelectedMode = userSelected && requested.IsValid()
	if current.UserSelectedMode {
		current.Mode = requested
	} else {
		current.Mode = DefaultGraphMode(state)
	}
	return current
}

// ReconcileAuthoritativeInteraction applies a compatible immutable graph patch
// without resetting viewport or a still-visible stable node selection.
func ReconcileAuthoritativeInteraction(
	current AuthoritativeInteraction,
	revisionID domain.GraphRevisionID,
	layout graphlayout.Layout,
	taskState domain.TaskState,
) AuthoritativeInteraction {
	current.RevisionID = revisionID
	if !current.UserSelectedMode || !current.Mode.IsValid() {
		current.Mode = DefaultGraphMode(taskState)
	}
	if !hasAuthoritativePlacement(layout, current.SelectedNodeID) {
		current.SelectedNodeID = domain.NodeID{}
	}
	return current
}

func SelectAuthoritativeNode(
	current AuthoritativeInteraction,
	layout graphlayout.Layout,
	nodeID domain.NodeID,
) (AuthoritativeInteraction, bool) {
	if !hasAuthoritativePlacement(layout, nodeID) {
		return current, false
	}
	current.SelectedNodeID = nodeID
	return current, true
}

// SelectAuthoritativeMode records a deliberate mode choice without changing
// the stable node selection or viewport shared by all three projections.
func SelectAuthoritativeMode(
	current AuthoritativeInteraction,
	mode graph.Mode,
) (AuthoritativeInteraction, bool) {
	if !mode.IsValid() {
		return current, false
	}
	current.Mode = mode
	current.UserSelectedMode = true
	return current, true
}

func ActivateLinkedNode(
	current AuthoritativeInteraction,
	layout graphlayout.Layout,
	nodeID domain.NodeID,
	width, height float64,
) (AuthoritativeInteraction, bool) {
	next, ok := SelectAuthoritativeNode(current, layout, nodeID)
	if !ok {
		return current, false
	}
	placement, _ := authoritativePlacement(layout, nodeID)
	next.Viewport = centerAuthoritativePlacement(next.Viewport, placement, width, height)
	return next, true
}

func TraverseAuthoritativeNodes(
	layout graphlayout.Layout,
	current domain.NodeID,
	key string,
) domain.NodeID {
	ordered := append([]graphlayout.Placement(nil), layout.Nodes...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Rank != ordered[j].Rank {
			return ordered[i].Rank < ordered[j].Rank
		}
		if ordered[i].Order != ordered[j].Order {
			return ordered[i].Order < ordered[j].Order
		}
		return ordered[i].NodeID.String() < ordered[j].NodeID.String()
	})
	if len(ordered) == 0 {
		return domain.NodeID{}
	}
	index := 0
	for candidateIndex, placement := range ordered {
		if placement.NodeID == current {
			index = candidateIndex
			break
		}
	}
	switch key {
	case "ArrowRight", "ArrowDown":
		index = (index + 1) % len(ordered)
	case "ArrowLeft", "ArrowUp":
		index = (index - 1 + len(ordered)) % len(ordered)
	case "Home":
		index = 0
	case "End":
		index = len(ordered) - 1
	default:
		return current
	}
	return ordered[index].NodeID
}

func shouldActivateAuthoritativeNodeAfterGesture(gesture dragState) bool {
	return !gesture.Moved
}

func authoritativeLayoutIdentity(layout graphlayout.Layout) string {
	identities := make([]string, 0, len(layout.Nodes))
	for _, placement := range layout.Nodes {
		identities = append(identities, placement.NodeID.String())
	}
	sort.Strings(identities)
	return layout.AlgorithmVersion + "|" + strings.Join(identities, "|")
}

func hasAuthoritativePlacement(layout graphlayout.Layout, nodeID domain.NodeID) bool {
	_, ok := authoritativePlacement(layout, nodeID)
	return ok
}

func authoritativePlacement(layout graphlayout.Layout, nodeID domain.NodeID) (graphlayout.Placement, bool) {
	if nodeID.IsZero() {
		return graphlayout.Placement{}, false
	}
	for _, placement := range layout.Nodes {
		if placement.NodeID == nodeID {
			return placement, true
		}
	}
	return graphlayout.Placement{}, false
}

func centerAuthoritativePlacement(
	viewport Viewport,
	placement graphlayout.Placement,
	width, height float64,
) Viewport {
	viewport = viewport.Normalize()
	centerX := float64(placement.Bounds.X) + float64(placement.Bounds.Width)/2
	centerY := float64(placement.Bounds.Y) + float64(placement.Bounds.Height)/2
	viewport.PanX = width/2 - centerX*viewport.Zoom
	viewport.PanY = height/2 - centerY*viewport.Zoom
	return viewport
}
