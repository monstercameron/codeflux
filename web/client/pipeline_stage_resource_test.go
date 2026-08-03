package main

import (
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/pipeline"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDecodePipelineStageRowsReadsMeasuredAndUnmeasuredStagesApart(t *testing.T) {
	finished := time.Date(2026, 8, 1, 12, 0, 3, 0, time.UTC)
	response := &codefluxv1.ListPipelineStagesResponse{
		Attempt: 2,
		Stages: []*codefluxv1.PipelineStageView{
			{
				StageNumber: 1, StageName: "instructions", State: "satisfied",
				FinishedAt: timestamppb.New(finished), ElapsedMeasured: true,
				Elapsed: durationpb.New(1500 * time.Millisecond),
			},
			{
				// elapsed_measured false: the wire deliberately leaves Elapsed
				// unset here, matching what the real handler does (PIPE-006b).
				StageNumber: 2, StageName: "clarification", State: "satisfied",
				FinishedAt: timestamppb.New(finished), ElapsedMeasured: false,
			},
		},
	}
	rows, attempt, err := decodePipelineStageRows(response)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if attempt != 2 {
		t.Errorf("attempt = %d, want 2", attempt)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	if !rows[0].ElapsedMeasured || rows[0].Elapsed != 1500*time.Millisecond {
		t.Errorf("measured row = %+v", rows[0])
	}
	if rows[1].ElapsedMeasured {
		t.Errorf("unmeasured row must report ElapsedMeasured=false, got %+v", rows[1])
	}
	if rows[1].Elapsed != 0 {
		t.Errorf("unmeasured row must carry a zero Elapsed, not a fabricated one, got %+v", rows[1])
	}
}

// TestDecodePipelineStageRowsIgnoresWireElapsedWhenNotMeasured guards the
// exact hazard PIPE-006b's handler comment names: a stage the wire marks
// unmeasured must never be read as having a real elapsed value even if one
// happened to be present on the wire (protobuf's zero value or a defect
// upstream), because "measured, and it was zero" and "not measured" must
// never collapse into the same reading on this side either.
func TestDecodePipelineStageRowsIgnoresWireElapsedWhenNotMeasured(t *testing.T) {
	response := &codefluxv1.ListPipelineStagesResponse{
		Stages: []*codefluxv1.PipelineStageView{{
			StageNumber: 1, StageName: "instructions", State: "satisfied",
			ElapsedMeasured: false, Elapsed: durationpb.New(5 * time.Second),
		}},
	}
	rows, _, err := decodePipelineStageRows(response)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rows[0].Elapsed != 0 {
		t.Errorf("unmeasured row must not surface a stray wire elapsed, got %v", rows[0].Elapsed)
	}
}

func TestDecodePipelineStageRowsRefusesMalformedInput(t *testing.T) {
	cases := []struct {
		name     string
		response *codefluxv1.ListPipelineStagesResponse
	}{
		{"nil response", nil},
		{"zero stage number", &codefluxv1.ListPipelineStagesResponse{
			Stages: []*codefluxv1.PipelineStageView{{StageNumber: 0, StageName: "instructions", State: "satisfied"}},
		}},
		{"empty stage name", &codefluxv1.ListPipelineStagesResponse{
			Stages: []*codefluxv1.PipelineStageView{{StageNumber: 1, StageName: "", State: "satisfied"}},
		}},
		{"unrecognised state", &codefluxv1.ListPipelineStagesResponse{
			Stages: []*codefluxv1.PipelineStageView{{StageNumber: 1, StageName: "instructions", State: "vibing"}},
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, _, err := decodePipelineStageRows(testCase.response); err == nil {
				t.Fatal("expected a refusal, got nil error")
			}
		})
	}
}

func TestDecodePipelineStageRowsAcceptsEveryValidState(t *testing.T) {
	for _, state := range []pipeline.State{
		pipeline.StateSatisfied, pipeline.StateFailed, pipeline.StateSkipped,
		pipeline.StateBlocked, pipeline.StateNotImplemented,
	} {
		response := &codefluxv1.ListPipelineStagesResponse{
			Stages: []*codefluxv1.PipelineStageView{{StageNumber: 1, StageName: "instructions", State: string(state)}},
		}
		rows, _, err := decodePipelineStageRows(response)
		if err != nil {
			t.Fatalf("state %q refused: %v", state, err)
		}
		if rows[0].State != state {
			t.Errorf("state %q decoded as %q", state, rows[0].State)
		}
	}
}
