//go:build js && wasm

package main

import (
	"context"
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
func useMountedSettings(active bool, envelope bootstrapEnvelope) settingsview.Props {
	references := ui.UseState(map[string]string{})
	checks := ui.UseState(map[string]settingsview.CredentialCheck{})
	busy := ui.UseState("")
	notice := ui.UseState("")
	noticeTone := ui.UseState(design.StatusNeutral)
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
		Loading:    active && workspace != nil && (current.Loading || !current.Ready),
		Failed:     active && workspace != nil && current.Error != nil,
		Reference:  references.Get(),
		Checks:     checks.Get(),
		Busy:       busy.Get(),
		Notice:     notice.Get(),
		NoticeTone: noticeTone.Get(),
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
	}
	props.OnReload = func() {
		notice.Set("")
		resource.Reload()
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
