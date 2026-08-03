//go:build js && wasm

package main

import (
	"context"
	"time"

	"codeflux.dev/codeflux/web/frontend/pipelineledger"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const mountedPipelineLedgerTimeout = 5 * time.Second

// useMountedPipelineLedger renders the current task's pipeline stage ledger
// (PIPE-006a) and returns the skip-audit summary alongside it, so a caller
// composing the surrounding thread workspace can also feed the same summary
// into the timeline's final-message caveat (PIPE-044) without a second
// fetch. attempt zero resolves to the coordinator's own default (attempt
// one); this hook does not invent a "latest attempt" concept the product
// does not otherwise have (see loadPipelineStageResource's own doc comment).
//
// This mirrors useMountedReview's resource-fetch shape deliberately: same
// timeout budget, same loading/error/ready presentation contract, so the two
// read-only task surfaces behave identically from a user's perspective.
func useMountedPipelineLedger(
	thread threadrail.Thread,
	mode primitives.Mode,
	attempt uint64,
) (ui.Node, pipelineledger.SkipSummary) {
	taskID := thread.TaskID()
	dependency := taskID.String()
	resource := fetch.UseResource(func(parent context.Context) (pipelineStageResource, error) {
		ctx, cancel := context.WithTimeout(parent, mountedPipelineLedgerTimeout)
		defer cancel()
		return loadPipelineStageResource(ctx, openBrowserPipelineStageResourceClient, taskID, attempt)
	}, dependency)
	state := resource.Get()

	if taskID.IsZero() {
		return pipelineledger.LedgerCard(pipelineledger.Props{Mode: mode, State: pipelineledger.Unavailable}), pipelineledger.SkipSummary{}
	}
	if state.Loading {
		return pipelineledger.LedgerCard(pipelineledger.Props{Mode: mode, State: pipelineledger.Loading}), pipelineledger.SkipSummary{}
	}
	if state.Error != nil || !state.Ready {
		message := ""
		if state.Error != nil {
			message = state.Error.Error()
		}
		return pipelineledger.LedgerCard(pipelineledger.Props{
			Mode: mode, State: pipelineledger.Failed, ErrorMessage: message,
		}), pipelineledger.SkipSummary{}
	}
	value := state.Value
	summary := pipelineledger.ComputeSkipSummary(value.Rows)
	card := pipelineledger.LedgerCard(pipelineledger.Props{
		Mode: mode, State: pipelineledger.Ready, Attempt: value.Attempt, Rows: value.Rows,
	})
	return card, summary
}
