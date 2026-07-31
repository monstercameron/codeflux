package timeline

import (
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

var ErrInvalidInteractionState = errors.New("invalid timeline interaction state")

// WorkspacePosition is the complete non-durable position that review must
// preserve. Anchors are stable presentation identities, never pixel offsets.
type WorkspacePosition struct {
	ThreadAnchor      ItemKey
	ThreadOffset      float64
	GraphRevision     domain.GraphRevisionID
	GraphSelectedNode string
	GraphPanX         float64
	GraphPanY         float64
	GraphZoom         float64
}

// ReviewTransition retains the exact chat and graph position while the review
// drawer is open.
type ReviewTransition struct {
	Open     bool
	Retained WorkspacePosition
}

func OpenReview(current ReviewTransition, position WorkspacePosition) (ReviewTransition, error) {
	if current.Open || strings.TrimSpace(string(position.ThreadAnchor)) == "" ||
		position.GraphRevision.IsZero() || position.GraphZoom <= 0 {
		return current, ErrInvalidInteractionState
	}
	return ReviewTransition{Open: true, Retained: position}, nil
}

func CloseReview(current ReviewTransition) (ReviewTransition, WorkspacePosition, error) {
	if !current.Open {
		return current, WorkspacePosition{}, ErrInvalidInteractionState
	}
	return ReviewTransition{}, current.Retained, nil
}

// GraphInspection prevents live auto-highlighting from panning away from
// deliberate user inspection.
type GraphInspection struct {
	SelectedNode             string
	CurrentExecutionNode     string
	UserInspecting           bool
	ReturnToCurrentAvailable bool
}

func BeginGraphInspection(current GraphInspection, nodeID string) (GraphInspection, error) {
	if strings.TrimSpace(nodeID) == "" {
		return current, ErrInvalidInteractionState
	}
	current.SelectedNode = nodeID
	current.UserInspecting = true
	current.ReturnToCurrentAvailable = current.CurrentExecutionNode != "" &&
		current.CurrentExecutionNode != current.SelectedNode
	return current, nil
}

func ApplyGraphAutoHighlight(current GraphInspection, nodeID string) (GraphInspection, error) {
	if strings.TrimSpace(nodeID) == "" {
		return current, ErrInvalidInteractionState
	}
	current.CurrentExecutionNode = nodeID
	if current.UserInspecting {
		current.ReturnToCurrentAvailable = current.SelectedNode != nodeID
		return current, nil
	}
	current.SelectedNode = nodeID
	current.ReturnToCurrentAvailable = false
	return current, nil
}

func ReturnToCurrentGraphNode(current GraphInspection) (GraphInspection, error) {
	if strings.TrimSpace(current.CurrentExecutionNode) == "" {
		return current, ErrInvalidInteractionState
	}
	current.SelectedNode = current.CurrentExecutionNode
	current.UserInspecting = false
	current.ReturnToCurrentAvailable = false
	return current, nil
}

type RepairTargetKind string

const (
	RepairTargetTask       RepairTargetKind = "task"
	RepairTargetFile       RepairTargetKind = "file"
	RepairTargetHunk       RepairTargetKind = "hunk"
	RepairTargetValidation RepairTargetKind = "validation"
	RepairTargetGraphNode  RepairTargetKind = "graph-node"
)

// RepairFeedbackTarget is an explicit one-of stable identity. Browser paths
// and line-number guesses are intentionally absent.
type RepairFeedbackTarget struct {
	Kind       RepairTargetKind
	Task       domain.TaskID
	File       domain.ArtifactID
	Hunk       string
	Validation domain.ValidationID
	GraphNode  string
}

func (target RepairFeedbackTarget) Validate() error {
	present := 0
	for _, set := range []bool{
		!target.Task.IsZero(),
		!target.File.IsZero(),
		strings.TrimSpace(target.Hunk) != "",
		!target.Validation.IsZero(),
		strings.TrimSpace(target.GraphNode) != "",
	} {
		if set {
			present++
		}
	}
	if present != 1 {
		return fmt.Errorf("%w: repair target must contain one identity", ErrInvalidInteractionState)
	}
	switch target.Kind {
	case RepairTargetTask:
		if target.Task.IsZero() {
			return ErrInvalidInteractionState
		}
	case RepairTargetFile:
		if target.File.IsZero() {
			return ErrInvalidInteractionState
		}
	case RepairTargetHunk:
		if value := strings.TrimSpace(target.Hunk); value == "" || len(value) > 255 {
			return ErrInvalidInteractionState
		}
	case RepairTargetValidation:
		if target.Validation.IsZero() {
			return ErrInvalidInteractionState
		}
	case RepairTargetGraphNode:
		if value := strings.TrimSpace(target.GraphNode); value == "" || len(value) > 255 {
			return ErrInvalidInteractionState
		}
	default:
		return ErrInvalidInteractionState
	}
	return nil
}
