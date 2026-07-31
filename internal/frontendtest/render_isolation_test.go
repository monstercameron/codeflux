package frontendtest

import "testing"

func TestValidateRenderIsolationAcceptsOwnedUpdates(t *testing.T) {
	before := RenderCounts{Thread: 10, Graph: 4, Message: 20, Cost: 3}
	cases := []struct {
		name  string
		event RenderEvent
		after RenderCounts
	}{
		{
			name:  "cost",
			event: RenderEventCostUpdate,
			after: RenderCounts{Thread: 10, Graph: 4, Message: 20, Cost: 4},
		},
		{
			name:  "chat",
			event: RenderEventChatAppend,
			after: RenderCounts{Thread: 11, Graph: 4, Message: 21, Cost: 3},
		},
		{
			name:  "graph",
			event: RenderEventGraphSelection,
			after: RenderCounts{Thread: 10, Graph: 5, Message: 20, Cost: 3},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := ValidateRenderIsolation(before, testCase.after, testCase.event); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateRenderIsolationRejectsCrossOwnerRerenders(t *testing.T) {
	before := RenderCounts{Thread: 10, Graph: 4, Message: 20, Cost: 3}
	cases := []struct {
		name  string
		event RenderEvent
		after RenderCounts
	}{
		{
			name:  "cost-rerenders-thread",
			event: RenderEventCostUpdate,
			after: RenderCounts{Thread: 11, Graph: 4, Message: 20, Cost: 4},
		},
		{
			name:  "chat-rerenders-graph",
			event: RenderEventChatAppend,
			after: RenderCounts{Thread: 11, Graph: 5, Message: 21, Cost: 3},
		},
		{
			name:  "selection-rerenders-messages",
			event: RenderEventGraphSelection,
			after: RenderCounts{Thread: 10, Graph: 5, Message: 21, Cost: 3},
		},
		{
			name:  "counter-reset",
			event: RenderEventGraphSelection,
			after: RenderCounts{Thread: 9, Graph: 5, Message: 20, Cost: 3},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := ValidateRenderIsolation(before, testCase.after, testCase.event); err == nil {
				t.Fatal("invalid render transition accepted")
			}
		})
	}
}
