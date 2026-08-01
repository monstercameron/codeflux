package graph

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func abuseNodeID(t *testing.T) domain.NodeID {
	t.Helper()
	id, err := domain.NewNodeID()
	if err != nil {
		t.Fatalf("new node ID: %v", err)
	}
	return id
}

// TestM22_062_OversizedGraphPayloadsAreRefused is the graph half of M22-062.
//
// A graph revision is projected from task events and then rendered in the
// browser. Every bound is checked at its exact edge, because a bound only one
// order of magnitude away from the real one is a bound nobody chose.
func TestM22_062_OversizedGraphPayloadsAreRefused(t *testing.T) {
	nodeID := abuseNodeID(t)

	t.Run("display name", func(t *testing.T) {
		atLimit := strings.Repeat("n", MaximumNodeDisplayNameBytes)
		if _, err := NewNode(nodeID, NodeClassAtomOperation, NodeStatusPending,
			atLimit, ContractSummary{}, SourceLinks{}); err != nil {
			t.Fatalf("a display name exactly at the bound was refused: %v", err)
		}
		over := strings.Repeat("n", MaximumNodeDisplayNameBytes+1)
		if _, err := NewNode(nodeID, NodeClassAtomOperation, NodeStatusPending,
			over, ContractSummary{}, SourceLinks{}); !errors.Is(err, ErrInvalidGraphModel) {
			t.Fatalf("an oversized display name gave %v, want ErrInvalidGraphModel", err)
		}
	})

	t.Run("contract purpose", func(t *testing.T) {
		if _, err := NewContractSummary(
			strings.Repeat("p", MaximumContractPurposeBytes), nil, nil, nil,
		); err != nil {
			t.Fatalf("a purpose exactly at the bound was refused: %v", err)
		}
		if _, err := NewContractSummary(
			strings.Repeat("p", MaximumContractPurposeBytes+1), nil, nil, nil,
		); !errors.Is(err, ErrInvalidGraphModel) {
			t.Fatalf("an oversized purpose gave %v, want ErrInvalidGraphModel", err)
		}
	})

	t.Run("contract item count", func(t *testing.T) {
		// Items must be distinct: the model refuses duplicates independently
		// of the count bound, so a repeating fixture would test the wrong rule.
		atLimit := make([]string, MaximumContractItems)
		for index := range atLimit {
			atLimit[index] = "item-" + strconv.Itoa(index)
		}
		if _, err := NewContractSummary("purpose", atLimit, nil, nil); err != nil {
			t.Fatalf("a contract with exactly %d inputs was refused: %v", MaximumContractItems, err)
		}
		over := append(append([]string(nil), atLimit...), "one-too-many")
		if _, err := NewContractSummary("purpose", over, nil, nil); !errors.Is(err, ErrInvalidGraphModel) {
			t.Fatalf("an oversized contract input list gave %v, want ErrInvalidGraphModel", err)
		}
	})

	t.Run("contract item size", func(t *testing.T) {
		if _, err := NewContractSummary("purpose",
			[]string{strings.Repeat("i", MaximumContractItemBytes)}, nil, nil); err != nil {
			t.Fatalf("a contract item exactly at the bound was refused: %v", err)
		}
		if _, err := NewContractSummary("purpose",
			[]string{strings.Repeat("i", MaximumContractItemBytes+1)}, nil, nil,
		); !errors.Is(err, ErrInvalidGraphModel) {
			t.Fatalf("an oversized contract item was accepted")
		}
	})

	t.Run("source event links", func(t *testing.T) {
		events := make([]domain.EventID, 0, MaximumSourceEventLinks+1)
		for range MaximumSourceEventLinks + 1 {
			eventID, err := domain.NewEventID()
			if err != nil {
				t.Fatalf("new event ID: %v", err)
			}
			events = append(events, eventID)
		}
		if _, err := NewSourceLinks(events[:MaximumSourceEventLinks], nil); err != nil {
			t.Fatalf("exactly %d source events were refused: %v", MaximumSourceEventLinks, err)
		}
		if _, err := NewSourceLinks(events, nil); !errors.Is(err, ErrInvalidGraphModel) {
			t.Fatalf("an oversized source event list gave %v, want ErrInvalidGraphModel", err)
		}
	})
}

// TestM22_062_GraphValidationErrorsCarryNoUntrustedContent proves an oversized
// payload cannot use the rejection itself as an output channel. Graph content
// originates in untrusted repository and model text, so echoing it into a
// diagnostic would move it to a surface that never validated it.
func TestM22_062_GraphValidationErrorsCarryNoUntrustedContent(t *testing.T) {
	const marker = "SECRET-MARKER-SHOULD-NOT-APPEAR"
	nodeID := abuseNodeID(t)

	_, err := NewNode(nodeID, NodeClassAtomOperation, NodeStatusPending,
		marker+strings.Repeat("n", MaximumNodeDisplayNameBytes), ContractSummary{}, SourceLinks{})
	if err == nil {
		t.Fatal("an oversized display name was accepted")
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("the validation error echoed untrusted content: %v", err)
	}

	var failure *ValidationError
	if !errors.As(err, &failure) {
		t.Fatalf("error %v is not a *ValidationError", err)
	}
	if failure.Field == "" || failure.Reason == "" {
		t.Fatalf("validation error is unusable: %+v", failure)
	}
	if strings.Contains(failure.Reason, marker) || strings.Contains(failure.Field, marker) {
		t.Fatalf("validation error fields echoed untrusted content: %+v", failure)
	}
}
