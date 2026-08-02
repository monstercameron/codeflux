//go:build js && wasm

package main

import (
	"context"

	"codeflux.dev/codeflux/web/frontend/dataview"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/preferences"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useMountedLocalData builds the settings page's local-data panel.
//
// Only this layer can answer what the browser is holding, because only this
// layer may touch browser storage. The panel reports what is stored and offers
// to forget it; it deliberately claims nothing about the coordinator's
// database, which no browser control can reach.
func useMountedLocalData(mode primitives.Mode, active bool) dataview.Props {
	confirming := ui.UseState(false)
	busy := ui.UseState(false)
	notice := ui.UseState("")
	tone := ui.UseState(design.StatusNeutral)
	stored := ui.UseState(false)
	unavailable := ui.UseState(false)

	// The check runs on every activation of this route rather than once for
	// the life of the console: layout and route preferences are written while
	// a person works, so a panel that answered once would keep reporting an
	// empty browser after something had been stored.
	ui.UseEffectOf(func() func() {
		if !active {
			return nil
		}
		store, err := preferences.OpenBrowserStore()
		if err != nil {
			unavailable.Set(true)
			return nil
		}
		unavailable.Set(false)
		_, loadErr := store.Load(context.Background())
		stored.Set(loadErr == nil)
		return nil
	}, active)

	props := dataview.Props{
		Mode: mode, Stored: stored.Get(), Unavailable: unavailable.Get(),
		Busy: busy.Get(), Confirming: confirming.Get(),
		Notice: notice.Get(), NoticeTone: tone.Get(),
	}
	if !active || unavailable.Get() || !stored.Get() {
		return props
	}
	props.OnForget = func() { notice.Set(""); confirming.Set(true) }
	props.OnCancel = func() { confirming.Set(false) }
	props.OnConfirm = func() {
		busy.Set(true)
		ui.SafeGo("forget stored interface state", func() {
			// Both stores are cleared, because the section promises to forget
			// the route, the layout, and the theme, and the theme lives in the
			// appearance record beside the layout envelope.
			store, err := preferences.OpenBrowserStore()
			var clearErr error
			if err == nil {
				clearErr = store.Clear(context.Background())
			} else {
				clearErr = err
			}
			if appearance, appearanceErr := preferences.OpenBrowserAppearanceStore(); appearanceErr == nil {
				if forgetErr := appearance.Clear(context.Background()); forgetErr != nil && clearErr == nil {
					clearErr = forgetErr
				}
			}
			ui.PostAsync(func() {
				busy.Set(false)
				confirming.Set(false)
				if clearErr != nil {
					notice.Set("The stored interface state could not be cleared. Nothing was changed.")
					tone.Set(design.StatusFailure)
					return
				}
				stored.Set(false)
				notice.Set("The stored route, layout, and theme were forgotten.")
				tone.Set(design.StatusSuccess)
			})
		})
	}
	return props
}
