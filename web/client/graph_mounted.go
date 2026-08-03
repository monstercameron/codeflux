package main

import (
	"errors"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graphlayout"
	"codeflux.dev/codeflux/web/frontend/threadrail"
)

var errMountedGraphSelectionUnavailable = errors.New("mounted graph selection is unavailable")

func selectedGraphResourceScope(thread threadrail.Thread) (graphResourceScope, error) {
	if thread.ProjectID().IsZero() || thread.TaskID().IsZero() {
		return graphResourceScope{}, errMountedGraphSelectionUnavailable
	}
	return graphResourceScope{ProjectID: thread.ProjectID(), TaskID: thread.TaskID()}, nil
}

// mountedGraphHasPlacement reports whether a node ID is part of a loaded
// bounded graph layout, which is the precondition for selecting and
// centering it on screen: a search hit outside the current slice cannot be
// placed, and the caller needs to know which case it is in before acting.
func mountedGraphHasPlacement(layout graphlayout.Layout, nodeID domain.NodeID) bool {
	if nodeID.IsZero() {
		return false
	}
	for _, placement := range layout.Nodes {
		if placement.NodeID == nodeID {
			return true
		}
	}
	return false
}
func graphExplanationDraft(current, explanation string) string {
	explanation = strings.TrimSpace(explanation)
	if explanation == "" {
		return current
	}
	current = strings.TrimSpace(current)
	if current == "" {
		return explanation
	}
	return current + "\n\n" + explanation
}
