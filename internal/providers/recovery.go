package providers

import (
	"errors"
	"reflect"
	"strings"
)

type RecoveryAction string

const (
	RecoveryRetrySameIdentity RecoveryAction = "retry"
	RecoveryResumeCheckpoint  RecoveryAction = "resume"
	RecoverySwitchProvider    RecoveryAction = "switch-provider"
)

type RecoveryChoice struct {
	Action                    RecoveryAction
	RequiresExplicitAuthority bool
}

// RetryExhaustedChoices are the only actions offered after the durable request
// is paused. Switching is always marked as an authority-bearing new decision.
func RetryExhaustedChoices(checkpointAvailable bool) []RecoveryChoice {
	choices := []RecoveryChoice{{
		Action: RecoveryRetrySameIdentity,
	}}
	if checkpointAvailable {
		choices = append(choices, RecoveryChoice{
			Action: RecoveryResumeCheckpoint,
		})
	}
	return append(choices, RecoveryChoice{
		Action: RecoverySwitchProvider, RequiresExplicitAuthority: true,
	})
}

type ProviderSwitchAuthority struct {
	DecisionID string
	Approved   bool
	From       ModelIdentity
	To         ModelIdentity
}

var ErrProviderSwitchAuthorityRequired = errors.New(
	"provider switch requires explicit user authority",
)

// ValidateProviderSwitch rejects fallback-by-implementation and binds approval
// to exact source and target provider/model/version identities.
func ValidateProviderSwitch(
	current ModelIdentity,
	target ModelIdentity,
	authority ProviderSwitchAuthority,
) error {
	if reflect.DeepEqual(current, target) {
		return errors.New("provider switch target must differ from the current model identity")
	}
	if !authority.Approved ||
		strings.TrimSpace(authority.DecisionID) == "" ||
		!reflect.DeepEqual(authority.From, current) ||
		!reflect.DeepEqual(authority.To, target) {
		return ErrProviderSwitchAuthorityRequired
	}
	return nil
}
