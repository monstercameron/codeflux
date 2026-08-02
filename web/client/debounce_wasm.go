//go:build js && wasm

package main

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useDebouncedValue returns the value once it has stopped changing for delay.
//
// It exists so a search box can ask the coordinator a question per typed word
// rather than per typed letter, without the caller holding a timer of its own.
// The settled value is returned unchanged the moment it stops moving, so a
// caller keyed on it re-reads exactly once.
func useDebouncedValue(value string, delay time.Duration) string {
	settled := ui.UseState(value)
	ui.UseEffectOf(func() func() {
		if settled.Get() == value {
			return nil
		}
		timer := time.AfterFunc(delay, func() {
			ui.PostAsync(func() { settled.Set(value) })
		})
		// The timer is stopped when the value moves again, so only the last
		// keystroke in a burst reaches the coordinator.
		return func() { timer.Stop() }
	}, value)
	return settled.Get()
}
