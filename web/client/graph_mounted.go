package main

import (
	"errors"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/threadrail"
)

var errMountedGraphSelectionUnavailable = errors.New("mounted graph selection is unavailable")

func selectedGraphResourceScope(thread threadrail.Thread) (graphResourceScope, error) {
	if thread.ProjectID().IsZero() || thread.TaskID().IsZero() {
		return graphResourceScope{}, errMountedGraphSelectionUnavailable
	}
	return graphResourceScope{ProjectID: thread.ProjectID(), TaskID: thread.TaskID()}, nil
}

func mountedGraphCurrentNode(resource graphResource) domain.NodeID {
	nodes := resource.Revision.Nodes()
	for _, node := range nodes {
		if node.Status() == taskgraph.NodeStatusActive {
			return node.ID()
		}
	}
	if len(nodes) > 0 {
		return nodes[0].ID()
	}
	return domain.NodeID{}
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
