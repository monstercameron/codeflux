//go:build js && wasm

package main

import (
	"context"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/settingsview"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// mountedSettingsTimeout bounds one settings read or command.
//
// The settings page must reach a state it can explain rather than spin: a
// coordinator that has not answered in this long is one to report.
const mountedSettingsTimeout = 10 * time.Second

// useMountedSettings asks the coordinator what governs a run and what it has
// recorded.
//
// The settings page drew five headings over static sentences while the
// coordinator held the policy, the providers, and the models behind an
// unimplemented service. This is the answer that replaces them.
func useMountedSettings(
	active bool,
	envelope bootstrapEnvelope,
	focus ui.FocusManager,
) settingsview.Props {
	references := ui.UseState(map[string]string{})
	checks := ui.UseState(map[string]settingsview.CredentialCheck{})
	busy := ui.UseState("")
	notice := ui.UseState("")
	noticeTone := ui.UseState(design.StatusNeutral)
	// Run settings are edited before they are sent: the engine validates one
	// configuration as a whole, so an edit is held here until it is saved.
	flowPending := ui.UseState(map[string]settingsview.FlowSetting{})
	flowBusy := ui.UseState(false)
	flowNotice := ui.UseState("")
	flowTone := ui.UseState(design.StatusNeutral)
	// The query and the row a result sent somebody to are browser-only
	// interaction state: nothing about them belongs to the coordinator.
	search := ui.UseState("")
	jumped := ui.UseState("")
	// The workspace identity is part of the request contract for both reads.
	// Without one there is nothing to ask with, and inventing an identity would
	// be this page claiming a scope nobody selected.
	workspace := envelope.SelectedWorkspaceID
	dependency := "inactive"
	if active && workspace != nil {
		dependency = "settings|" + workspace.GetValue()
	}
	resource := fetch.UseResource(func(parent context.Context) (settingsAnswer, error) {
		if !active || workspace == nil {
			return settingsAnswer{}, nil
		}
		ctx, cancel := context.WithTimeout(parent, mountedSettingsTimeout)
		defer cancel()
		return readMountedSettings(ctx, workspace)
	}, dependency)

	current := resource.Get()
	props := settingsview.Props{
		Search:      search.Get(),
		Jumped:      jumped.Get(),
		FlowPending: flowPending.Get(),
		FlowBusy:    flowBusy.Get(),
		FlowNotice:  flowNotice.Get(),
		FlowTone:    flowTone.Get(),
		Loading:     active && workspace != nil && (current.Loading || !current.Ready),
		Failed:      active && workspace != nil && current.Error != nil,
		Reference:   references.Get(),
		Checks:      checks.Get(),
		Busy:        busy.Get(),
		Notice:      notice.Get(),
		NoticeTone:  noticeTone.Get(),
	}
	if workspace == nil {
		props.Loading = false
		props.Unavailable = true
		props.UnavailableReason = "Settings are read against an open workspace. " +
			"Choose a repository first."
		return props
	}
	if !active {
		return props
	}
	if current.Ready && current.Error == nil {
		props.Policy = current.Value.Policy
		props.Providers = current.Value.Providers
		props.Spend = current.Value.Spend
		props.Flow = current.Value.Flow
		props.FlowUnrenderable = current.Value.FlowUnrenderable
		props.FlowRevisionKnown = true
	}
	props.OnSearch = func(query string) {
		search.Set(query)
		if strings.TrimSpace(query) == "" {
			jumped.Set("")
		}
	}
	props.OnJump = func(key string) {
		jumped.Set(key)
		// Focusing the setting's own control scrolls it into view and leaves
		// the keyboard where the pointer went, so arriving by search and
		// arriving by scrolling put somebody in the same place.
		ui.PostAsync(func() { focus.FocusByID("flow-" + key) })
	}
	props.OnReload = func() {
		notice.Set("")
		flowNotice.Set("")
		flowPending.Set(map[string]settingsview.FlowSetting{})
		resource.Reload()
	}
	setPending := func(key string, apply func(settingsview.FlowSetting) settingsview.FlowSetting) {
		stored, found := flowSettingByKey(props.Flow, key)
		if !found {
			return
		}
		if existing, changed := flowPending.Get()[key]; changed {
			stored = existing
		}
		next := map[string]settingsview.FlowSetting{}
		for existingKey, existing := range flowPending.Get() {
			next[existingKey] = existing
		}
		next[key] = apply(stored)
		flowPending.Set(next)
	}
	if !flowBusy.Get() {
		props.OnFlowChoice = func(key, value string) {
			setPending(key, func(setting settingsview.FlowSetting) settingsview.FlowSetting {
				setting.Text = value
				return setting
			})
		}
		props.OnFlowNumber = func(key string, value int32) {
			setPending(key, func(setting settingsview.FlowSetting) settingsview.FlowSetting {
				setting.Number = value
				return setting
			})
		}
		props.OnFlowSwitch = func(key string) {
			setPending(key, func(setting settingsview.FlowSetting) settingsview.FlowSetting {
				setting.Enabled = !setting.Enabled
				return setting
			})
		}
		props.OnFlowDiscard = func() {
			flowNotice.Set("")
			flowPending.Set(map[string]settingsview.FlowSetting{})
		}
	}
	if len(flowPending.Get()) > 0 && !flowBusy.Get() && props.FlowRevisionKnown {
		revision := current.Value.FlowRevision
		changes := flowSettingChanges(flowPending.Get())
		props.OnFlowSave = func() {
			key, err := composer.NewIdempotencyKey()
			if err != nil {
				flowNotice.Set("This command could not be given an identity, so it was not sent.")
				flowTone.Set(design.StatusFailure)
				return
			}
			flowBusy.Set(true)
			flowNotice.Set("")
			ui.SafeGo("save the run settings", func() {
				ctx, cancel := context.WithTimeout(context.Background(), mountedSettingsTimeout)
				defer cancel()
				_, _, saveErr := saveMountedFlowSettings(
					ctx, workspace, changes, revision, string(key),
				)
				ui.PostAsync(func() {
					flowBusy.Set(false)
					if saveErr != nil {
						flowNotice.Set("The run settings were not changed: " + safeCommandReason(saveErr))
						flowTone.Set(design.StatusFailure)
						return
					}
					flowPending.Set(map[string]settingsview.FlowSetting{})
					flowNotice.Set("Saved. Runs started from now on use these settings.")
					flowTone.Set(design.StatusSuccess)
					resource.Reload()
				})
			})
		}
	}
	props.OnReferenceInput = func(providerID, value string) {
		next := map[string]string{}
		for key, existing := range references.Get() {
			next[key] = existing
		}
		next[providerID] = value
		references.Set(next)
	}
	props.OnCheckCredential = func(providerID string) {
		identity := providerIdentity(providerID)
		if identity == nil {
			return
		}
		checks.Set(withCredentialCheck(
			checks.Get(), providerID, settingsview.CredentialCheck{Running: true},
		))
		ui.SafeGo("check a provider credential", func() {
			ctx, cancel := context.WithTimeout(context.Background(), mountedSettingsTimeout)
			defer cancel()
			result, err := checkMountedProviderCredential(ctx, identity)
			ui.PostAsync(func() {
				if err != nil {
					checks.Set(withCredentialCheck(checks.Get(), providerID, settingsview.CredentialCheck{
						Summary: "The credential check could not be completed. " +
							"Nothing was changed.",
					}))
					return
				}
				checks.Set(withCredentialCheck(checks.Get(), providerID, result))
			})
		})
	}
	if busy.Get() == "" {
		props.OnConfigure = func(providerID, modelID string) {
			identity := providerIdentity(providerID)
			reference := references.Get()[providerID]
			if identity == nil || reference == "" {
				return
			}
			key, err := composer.NewIdempotencyKey()
			if err != nil {
				notice.Set("This command could not be given an identity, so it was not sent.")
				noticeTone.Set(design.StatusFailure)
				return
			}
			busy.Set(providerID)
			notice.Set("")
			ui.SafeGo("configure a provider credential", func() {
				ctx, cancel := context.WithTimeout(context.Background(), mountedSettingsTimeout)
				defer cancel()
				configureErr := configureMountedProvider(
					ctx, workspace, identity, reference, modelID, string(key),
				)
				ui.PostAsync(func() {
					busy.Set("")
					if configureErr != nil {
						notice.Set("The provider was not configured: " + safeCommandReason(configureErr))
						noticeTone.Set(design.StatusFailure)
						return
					}
					notice.Set("The credential reference was bound to this provider.")
					noticeTone.Set(design.StatusSuccess)
					resource.Reload()
				})
			})
		}
	}
	return props
}

// withCredentialCheck returns a copy carrying one provider's check.
//
// The map is copied rather than mutated because the previous value is the one
// a render in flight is still reading.
func withCredentialCheck(
	current map[string]settingsview.CredentialCheck,
	providerID string,
	check settingsview.CredentialCheck,
) map[string]settingsview.CredentialCheck {
	next := make(map[string]settingsview.CredentialCheck, len(current)+1)
	for key, existing := range current {
		next[key] = existing
	}
	next[providerID] = check
	return next
}

// providerIdentity rebuilds the typed identity a command needs.
func providerIdentity(providerID string) *codefluxv1.StableIdentity {
	if providerID == "" {
		return nil
	}
	return &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER,
		Value: providerID,
	}
}

// flowSettingByKey finds one described setting.
func flowSettingByKey(
	settings []settingsview.FlowSetting,
	key string,
) (settingsview.FlowSetting, bool) {
	for _, setting := range settings {
		if setting.Key == key {
			return setting, true
		}
	}
	return settingsview.FlowSetting{}, false
}
