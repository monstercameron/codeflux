//go:build js && wasm

package main

import (
	"context"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/preferences"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// appearanceChoice is the appearance the console is currently rendering with,
// plus the writer that records a change.
type appearanceChoice struct {
	Theme   design.Theme
	Density design.Density
	Motion  preferences.AppearanceMotion
	Set     func(preferences.Appearance)
}

// useAppearancePreferences reads the stored appearance once and writes every
// change back.
//
// The theme control has always claimed the choice persists, and it did not:
// nothing wrote it, so a reload returned to dark. Density and the motion
// override had no control at all. All three are stored beside the layout
// envelope rather than inside it, so a change here cannot make a stored layout
// unreadable.
func useAppearancePreferences(applyTheme func(string)) appearanceChoice {
	stored := ui.UseState(preferences.Appearance{})
	loaded := ui.UseRef(false)

	ui.UseEffectOf(func() func() {
		if loaded.Get() {
			return nil
		}
		loaded.Set(true)
		store, err := preferences.OpenBrowserAppearanceStore()
		if err != nil {
			return nil
		}
		if value, loadErr := store.Load(context.Background()); loadErr == nil {
			stored.Set(value)
			// The document's own theme attribute is set by the theme hook, so
			// the stored choice is pushed into it as well. Without this the
			// tokens render light while the document still says dark, and
			// anything reading the attribute disagrees with the screen.
			if value.Theme != "" && applyTheme != nil {
				applyTheme(value.Theme)
			}
		}
		return nil
	}, "appearance")

	current := stored.Get()
	choice := appearanceChoice{
		// The console opens dark whatever the desktop prefers: its own boot
		// document is dark and a run is watched for long stretches.
		Theme:   design.ThemeDark,
		Density: design.DensityComfortable,
		Motion:  current.Motion,
	}
	switch design.Theme(current.Theme) {
	case design.ThemeLight:
		choice.Theme = design.ThemeLight
	case design.ThemeHighContrast:
		choice.Theme = design.ThemeHighContrast
	}
	if design.Density(current.Density) == design.DensityCompact {
		choice.Density = design.DensityCompact
	}
	choice.Set = func(next preferences.Appearance) {
		stored.Set(next)
		ui.SafeGo("record the appearance choice", func() {
			store, err := preferences.OpenBrowserAppearanceStore()
			if err != nil {
				return
			}
			_ = store.Save(context.Background(), next)
		})
	}
	return choice
}

// ReducesMotion reports whether motion should be reduced, honouring an
// explicit choice over the operating system's.
func (choice appearanceChoice) ReducesMotion(systemReducesMotion bool) bool {
	switch choice.Motion {
	case preferences.MotionReduce:
		return true
	case preferences.MotionFull:
		return false
	default:
		return systemReducesMotion
	}
}

// Record renders the choice back into its stored shape.
func (choice appearanceChoice) Record() preferences.Appearance {
	return preferences.Appearance{
		Theme: string(choice.Theme), Density: string(choice.Density), Motion: choice.Motion,
	}
}
