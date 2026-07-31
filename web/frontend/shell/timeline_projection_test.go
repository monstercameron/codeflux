package shell

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
)

func TestTimelineProjectionGivesEveryShellRoleATypedStableCard(t *testing.T) {
	fixtures := []struct {
		role string
		kind timelinecard.Kind
	}{
		{role: "requirement", kind: timelinecard.KindRequirement},
		{role: "forecast", kind: timelinecard.KindForecast},
		{role: "plan", kind: timelinecard.KindPlan},
		{role: "execution", kind: timelinecard.KindTool},
		{role: "validation", kind: timelinecard.KindValidation},
		{role: "agent", kind: timelinecard.KindMessage},
	}
	for index, fixture := range fixtures {
		message := state.MessageView{
			ID: "message-" + fixture.role, Role: fixture.role,
			Body: "Safe redacted fixture body", Sequence: uint64(index + 1),
		}
		card := timelineCardForMessage(message)
		if card.Kind != fixture.kind {
			t.Errorf("%s card kind = %s, want %s", fixture.role, card.Kind, fixture.kind)
		}
		if card.Sequence != message.Sequence || card.StableKey != message.ID || card.OccurredAt.IsZero() {
			t.Errorf("%s card lost durable identity: %#v", fixture.role, card)
		}
		if err := card.Validate(); err != nil {
			t.Errorf("%s card invalid: %v", fixture.role, err)
		}
	}
}

func TestPendingGenericMessageRemainsVisiblyProvisional(t *testing.T) {
	card := timelineCardForMessage(state.MessageView{
		ID: "pending", Role: "agent", Body: "partial", Pending: true, Sequence: 9,
	})
	if card.Message == nil || card.Message.Status != timelinecard.MessageProvisional {
		t.Fatalf("pending card = %#v", card)
	}
}
