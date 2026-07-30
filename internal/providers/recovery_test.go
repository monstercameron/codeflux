package providers

import (
	"errors"
	"testing"
)

func TestRetryExhaustedChoicesRequireAuthorityForProviderSwitch(t *testing.T) {
	choices := RetryExhaustedChoices(true)
	if len(choices) != 3 ||
		choices[0].Action != RecoveryRetrySameIdentity ||
		choices[1].Action != RecoveryResumeCheckpoint ||
		choices[2].Action != RecoverySwitchProvider ||
		!choices[2].RequiresExplicitAuthority {
		t.Fatalf("choices = %#v", choices)
	}
}

func TestValidateProviderSwitchBindsExactApprovedIdentities(t *testing.T) {
	from := ModelIdentity{
		Provider: ProviderIdentity{
			Adapter: "openai-responses", AdapterVersion: "1",
			Provider: "openai", ProviderVersion: "responses-v1",
		},
		Model: "model-a", Revision: "revision-a",
	}
	to := ModelIdentity{
		Provider: ProviderIdentity{
			Adapter: "anthropic-messages", AdapterVersion: "1",
			Provider: "anthropic", ProviderVersion: "messages-v1",
		},
		Model: "model-b", Revision: "revision-b",
	}
	if err := ValidateProviderSwitch(from, to, ProviderSwitchAuthority{}); !errors.Is(
		err,
		ErrProviderSwitchAuthorityRequired,
	) {
		t.Fatalf("unapproved switch error = %v", err)
	}
	authority := ProviderSwitchAuthority{
		DecisionID: "decision-fixture", Approved: true, From: from, To: to,
	}
	if err := ValidateProviderSwitch(from, to, authority); err != nil {
		t.Fatal(err)
	}
	changed := to
	changed.Revision = "different"
	if err := ValidateProviderSwitch(from, changed, authority); !errors.Is(
		err,
		ErrProviderSwitchAuthorityRequired,
	) {
		t.Fatalf("authority was reused for a different target: %v", err)
	}
}
