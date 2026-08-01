package main

import (
	"errors"
	"strings"

	"codeflux.dev/codeflux/web/frontend/threadrail"
)

var errMountedGraphSelectionUnavailable = errors.New("mounted graph selection is unavailable")

func selectedGraphResourceScope(thread threadrail.Thread) (graphResourceScope, error) {
	if thread.ProjectID().IsZero() || thread.TaskID().IsZero() {
		return graphResourceScope{}, errMountedGraphSelectionUnavailable
	}
	return graphResourceScope{ProjectID: thread.ProjectID(), TaskID: thread.TaskID()}, nil
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
