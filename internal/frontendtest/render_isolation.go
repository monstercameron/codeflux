package frontendtest

import "fmt"

// RenderCounts is the stable instrumentation read from Go-rendered component
// roots before and after an interaction.
type RenderCounts struct {
	Thread  int `json:"thread"`
	Graph   int `json:"graph"`
	Message int `json:"message"`
	Cost    int `json:"cost"`
}

// RenderEvent identifies the ownership boundary exercised by an interaction.
type RenderEvent string

const (
	RenderEventCostUpdate     RenderEvent = "cost-update"
	RenderEventChatAppend     RenderEvent = "chat-append"
	RenderEventGraphSelection RenderEvent = "graph-selection"
)

// ValidateRenderIsolation enforces M16-071..074 without accepting a total
// render count reset or a no-op interaction as evidence.
func ValidateRenderIsolation(before, after RenderCounts, event RenderEvent) error {
	if after.Thread < before.Thread || after.Graph < before.Graph ||
		after.Message < before.Message || after.Cost < before.Cost {
		return fmt.Errorf("render counters must be monotonic: before=%+v after=%+v", before, after)
	}
	switch event {
	case RenderEventCostUpdate:
		if after.Cost <= before.Cost {
			return fmt.Errorf("cost update did not advance cost render count")
		}
		if after.Thread != before.Thread {
			return fmt.Errorf("cost update rerendered full thread: %d -> %d", before.Thread, after.Thread)
		}
	case RenderEventChatAppend:
		if after.Message <= before.Message {
			return fmt.Errorf("chat append did not advance message render count")
		}
		if after.Graph != before.Graph {
			return fmt.Errorf("chat append rerendered graph: %d -> %d", before.Graph, after.Graph)
		}
	case RenderEventGraphSelection:
		if after.Graph <= before.Graph {
			return fmt.Errorf("graph selection did not advance graph render count")
		}
		if after.Message != before.Message {
			return fmt.Errorf(
				"graph selection rerendered messages: %d -> %d",
				before.Message,
				after.Message,
			)
		}
	default:
		return fmt.Errorf("unknown render event %q", event)
	}
	return nil
}
