package main

import (
	"errors"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/web/frontend/pipelineledger"
)

// errPipelineStageResourceMalformed is refused rather than shown with blanks:
// a stage row misreporting its own number, name, or state is exactly the
// kind of quiet drift PIPE-006's own history warns about (a stale binding
// that "reads as authoritative" until a reader chases it and finds nothing
// there), so a row this decoder cannot trust is not surfaced at all.
var errPipelineStageResourceMalformed = errors.New("authoritative pipeline stage ledger response is malformed")

// decodePipelineStageRows converts one ListPipelineStagesResponse into the
// ledger rows pipelineledger.LedgerCard renders, plus the attempt the
// coordinator actually resolved (PIPE-006a).
//
// A row is refused outright, rather than rendered with a placeholder, when
// its stage number is zero or its state is not one of the five the ledger
// ever writes -- accepting an unrecognised state would let this surface
// silently invent a sixth outcome the pipeline package does not define.
func decodePipelineStageRows(response *codefluxv1.ListPipelineStagesResponse) ([]pipelineledger.StageRow, uint64, error) {
	if response == nil {
		return nil, 0, errPipelineStageResourceMalformed
	}
	views := response.GetStages()
	rows := make([]pipelineledger.StageRow, 0, len(views))
	for _, view := range views {
		row, err := decodePipelineStageRow(view)
		if err != nil {
			return nil, 0, err
		}
		rows = append(rows, row)
	}
	return rows, response.GetAttempt(), nil
}

func decodePipelineStageRow(view *codefluxv1.PipelineStageView) (pipelineledger.StageRow, error) {
	if view == nil || view.GetStageNumber() == 0 || view.GetStageName() == "" {
		return pipelineledger.StageRow{}, errPipelineStageResourceMalformed
	}
	state := pipeline.State(view.GetState())
	if !state.Valid() {
		return pipelineledger.StageRow{}, errPipelineStageResourceMalformed
	}
	row := pipelineledger.StageRow{
		Number: pipeline.Number(view.GetStageNumber()), Name: view.GetStageName(), State: state,
		ElapsedMeasured: view.GetElapsedMeasured(),
	}
	if stamp := view.GetFinishedAt(); stamp != nil {
		row.FinishedAt = stamp.AsTime()
	}
	// elapsed is meaningful only when the wire reported it measured; a
	// caller that trusted view.GetElapsed() alone when it was left unset
	// would read protobuf's own zero value as "measured, and it was zero"
	// (PIPE-006b's own handler comment names exactly this hazard).
	if row.ElapsedMeasured {
		if elapsed := view.GetElapsed(); elapsed != nil {
			row.Elapsed = elapsed.AsDuration()
		}
	}
	return row, nil
}
