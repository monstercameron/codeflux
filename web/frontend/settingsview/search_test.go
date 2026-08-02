package settingsview_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/settingsview"
)

func TestSearchRanksANameAboveARationale(t *testing.T) {
	props := answeredSheet()
	hits := settingsview.SearchSettings(props, "repeat")
	if len(hits) == 0 {
		t.Fatal("a word in a setting's name must find it")
	}
	// "Times to repeat the suite" is named for it; the mutation setting only
	// mentions repeating in its explanation.
	if hits[0].Label != "Times to repeat the suite" {
		t.Fatalf("first hit = %q, want the setting named for the word", hits[0].Label)
	}
	if hits[0].Matched != "name" {
		t.Fatalf("matched = %q, want the match to say where it was found", hits[0].Matched)
	}
}

func TestSearchFindsASettingByItsGroupAndItsKey(t *testing.T) {
	props := answeredSheet()
	if hits := settingsview.SearchSettings(props, "verification"); len(hits) < 2 {
		t.Fatalf("a group name must find the settings in it, got %d", len(hits))
	}
	hits := settingsview.SearchSettings(props, "maximum_attempts")
	if len(hits) != 1 || hits[0].Key != "maximum_attempts" {
		t.Fatalf("a key must find exactly its setting: %+v", hits)
	}
	if hits[0].Matched != "maximum_attempts" {
		t.Fatalf("a key match must say so: %q", hits[0].Matched)
	}
}

func TestSearchSaysWhenItFoundNothing(t *testing.T) {
	props := answeredSheet()
	props.OnSearch = func(string) {}
	props.Search = "kubernetes"
	markup := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(markup, "No setting matches kubernetes.") {
		t.Fatalf("an empty result must say so: %s", markup)
	}
	if settingsview.SearchSettings(props, "   ") != nil {
		t.Fatal("a blank query must not search")
	}
}

func TestAResultOffersAJumpAndTheRowItLandsOnIsMarked(t *testing.T) {
	props := answeredSheet()
	props.Search = "attempts"
	props.OnSearch = func(string) {}
	props.OnJump = func(string) {}
	markup := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(markup, `data-component="settings-search-result"`) {
		t.Fatalf("a match must offer a way to reach it: %s", markup)
	}
	if !strings.Contains(markup, "Go to Attempts before stopping in Run behaviour") {
		t.Fatalf("a result must name where it goes: %s", markup)
	}
	// The control carries the identity the jump focuses, so arriving by search
	// lands on the thing itself rather than near it.
	if !strings.Contains(markup, `id="flow-maximum_attempts"`) {
		t.Fatalf("the setting's control must be addressable: %s", markup)
	}

	props.Jumped = "maximum_attempts"
	marked := renderSettings(t, settingsview.Sheet(props))
	if !strings.Contains(marked, `data-jumped="true"`) {
		t.Fatalf("the row somebody was sent to must be marked: %s", marked)
	}
}
