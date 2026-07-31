package shortcuts_test

import (
	"errors"
	"reflect"
	"testing"

	"codeflux.dev/codeflux/web/frontend/shortcuts"
)

func TestDefaultPolicyMapsEveryActionDeterministically(t *testing.T) {
	policy := shortcuts.DefaultPolicy()
	tests := []struct {
		key    string
		action shortcuts.Action
	}{
		{key: "1", action: shortcuts.ActionFocusConversation},
		{key: "2", action: shortcuts.ActionFocusGraph},
		{key: "A", action: shortcuts.ActionPause},
		{key: "x", action: shortcuts.ActionStop},
		{key: "/", action: shortcuts.ActionHelp},
	}
	for _, test := range tests {
		t.Run(string(test.action), func(t *testing.T) {
			windows := policy.Resolve(shortcuts.Event{
				Key: test.key, Ctrl: true, Alt: true, Target: shortcuts.TargetOther,
			}, shortcuts.PlatformWindows)
			if !windows.Handled || !windows.PreventDefault || windows.Action != test.action || windows.Reason != shortcuts.SuppressionNone {
				t.Fatalf("Windows Resolve = %+v", windows)
			}

			mac := policy.Resolve(shortcuts.Event{
				Key: test.key, Meta: true, Alt: true, Target: shortcuts.TargetOther,
			}, shortcuts.PlatformMacOS)
			if !mac.Handled || mac.Action != test.action {
				t.Fatalf("macOS Resolve = %+v", mac)
			}
		})
	}
}

func TestPolicyRequiresThePlatformPrimaryModifierExactly(t *testing.T) {
	policy := shortcuts.DefaultPolicy()
	tests := []struct {
		name     string
		event    shortcuts.Event
		platform shortcuts.Platform
	}{
		{name: "control is not command", event: shortcuts.Event{Key: "1", Ctrl: true, Alt: true}, platform: shortcuts.PlatformMacOS},
		{name: "command is not control", event: shortcuts.Event{Key: "1", Meta: true, Alt: true}, platform: shortcuts.PlatformWindows},
		{name: "extra shift", event: shortcuts.Event{Key: "1", Ctrl: true, Alt: true, Shift: true}, platform: shortcuts.PlatformWindows},
		{name: "missing alt", event: shortcuts.Event{Key: "1", Ctrl: true}, platform: shortcuts.PlatformWindows},
		{name: "both primaries", event: shortcuts.Event{Key: "1", Ctrl: true, Meta: true, Alt: true}, platform: shortcuts.PlatformWindows},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := policy.Resolve(test.event, test.platform)
			if decision.Handled || decision.PreventDefault || decision.Reason != shortcuts.SuppressionNoMatch {
				t.Fatalf("Resolve = %+v", decision)
			}
		})
	}
}

func TestDefaultPolicySuppressesAllShortcutsDuringTextEntry(t *testing.T) {
	policy := shortcuts.DefaultPolicy()
	for _, target := range []shortcuts.TargetKind{
		shortcuts.TargetInput,
		shortcuts.TargetTextArea,
		shortcuts.TargetSelect,
		shortcuts.TargetContentEditable,
	} {
		t.Run(string(target), func(t *testing.T) {
			for _, binding := range policy.Bindings() {
				decision := policy.Resolve(shortcuts.Event{
					Key: binding.Chord.Key, Ctrl: true, Alt: binding.Chord.Alt,
					Shift: binding.Chord.Shift, Target: target, Scope: shortcuts.ScopeComposer,
				}, shortcuts.PlatformWindows)
				if decision.Handled || decision.PreventDefault || decision.Reason != shortcuts.SuppressionTyping {
					t.Fatalf("%s on %s = %+v", binding.Action, target, decision)
				}
			}
		})
	}
}

func TestClassifyTargetIncludesEffectiveContentEditableAncestors(t *testing.T) {
	tests := []struct {
		tag      string
		editable bool
		want     shortcuts.TargetKind
	}{
		{tag: " INPUT ", want: shortcuts.TargetInput},
		{tag: "TEXTAREA", want: shortcuts.TargetTextArea},
		{tag: "select", want: shortcuts.TargetSelect},
		{tag: "span", editable: true, want: shortcuts.TargetContentEditable},
		{tag: "button", want: shortcuts.TargetOther},
	}
	for _, test := range tests {
		if got := shortcuts.ClassifyTarget(test.tag, test.editable); got != test.want {
			t.Fatalf("ClassifyTarget(%q, %t) = %s, want %s", test.tag, test.editable, got, test.want)
		}
	}
}

func TestExplicitScopedBindingMayRunWhileEditingOnlyInItsScope(t *testing.T) {
	policy, err := shortcuts.NewPolicy([]shortcuts.Binding{{
		Action: shortcuts.ActionHelp,
		Chord:  shortcuts.Chord{Key: "/", Primary: true, Alt: true},
		Scope:  shortcuts.ScopeComposer, AllowWhileEditing: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	accepted := policy.Resolve(shortcuts.Event{
		Key: "/", Ctrl: true, Alt: true, Target: shortcuts.TargetTextArea, Scope: shortcuts.ScopeComposer,
	}, shortcuts.PlatformWindows)
	if !accepted.Handled || accepted.Action != shortcuts.ActionHelp {
		t.Fatalf("scoped Resolve = %+v", accepted)
	}
	rejected := policy.Resolve(shortcuts.Event{
		Key: "/", Ctrl: true, Alt: true, Target: shortcuts.TargetContentEditable, Scope: shortcuts.ScopeConversation,
	}, shortcuts.PlatformWindows)
	if rejected.Handled || rejected.Reason != shortcuts.SuppressionScope {
		t.Fatalf("wrong-scope Resolve = %+v", rejected)
	}
}

func TestCompositionAndRepeatNeverDispatch(t *testing.T) {
	policy := shortcuts.DefaultPolicy()
	for _, event := range []shortcuts.Event{
		{Key: "a", Ctrl: true, Alt: true, Composing: true},
		{Key: "a", Ctrl: true, Alt: true, Repeat: true},
	} {
		decision := policy.Resolve(event, shortcuts.PlatformWindows)
		if decision.Handled || decision.PreventDefault {
			t.Fatalf("Resolve = %+v", decision)
		}
	}
}

func TestPolicyRejectsUnsafeAmbiguousBindings(t *testing.T) {
	tests := []struct {
		name     string
		bindings []shortcuts.Binding
		want     error
	}{
		{
			name: "collision",
			bindings: []shortcuts.Binding{
				{Action: shortcuts.ActionPause, Chord: shortcuts.Chord{Key: "a", Primary: true, Alt: true}, Scope: shortcuts.ScopeApplication},
				{Action: shortcuts.ActionStop, Chord: shortcuts.Chord{Key: "A", Primary: true, Alt: true}, Scope: shortcuts.ScopeApplication},
			},
			want: shortcuts.ErrCollision,
		},
		{
			name: "reserved browser chord",
			bindings: []shortcuts.Binding{{
				Action: shortcuts.ActionHelp, Chord: shortcuts.Chord{Key: "l", Primary: true}, Scope: shortcuts.ScopeApplication,
			}},
			want: shortcuts.ErrReservedChord,
		},
		{
			name: "reserved on mac only",
			bindings: []shortcuts.Binding{{
				Action: shortcuts.ActionHelp, Chord: shortcuts.Chord{Key: "c", Primary: true, Alt: true}, Scope: shortcuts.ScopeApplication,
			}},
			want: shortcuts.ErrReservedChord,
		},
		{
			name: "reserved PDF viewer chord",
			bindings: []shortcuts.Binding{{
				Action: shortcuts.ActionPause, Chord: shortcuts.Chord{Key: "p", Primary: true, Alt: true}, Scope: shortcuts.ScopeApplication,
			}},
			want: shortcuts.ErrReservedChord,
		},
		{
			name: "unmodified global key",
			bindings: []shortcuts.Binding{{
				Action: shortcuts.ActionHelp, Chord: shortcuts.Chord{Key: "?"}, Scope: shortcuts.ScopeApplication,
			}},
			want: shortcuts.ErrInvalidBinding,
		},
		{
			name: "editing without explicit scope",
			bindings: []shortcuts.Binding{{
				Action: shortcuts.ActionHelp, Chord: shortcuts.Chord{Key: "k", Primary: true, Alt: true}, Scope: shortcuts.ScopeApplication, AllowWhileEditing: true,
			}},
			want: shortcuts.ErrInvalidBinding,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := shortcuts.NewPolicy(test.bindings)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewPolicy error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDefaultPolicyContainsNoReservedChord(t *testing.T) {
	for _, binding := range shortcuts.DefaultPolicy().Bindings() {
		for _, platform := range []shortcuts.Platform{
			shortcuts.PlatformMacOS, shortcuts.PlatformWindows, shortcuts.PlatformLinux, shortcuts.PlatformOther,
		} {
			if shortcuts.IsBrowserReserved(binding.Chord, platform) {
				t.Fatalf("%s uses a reserved chord on %s", binding.Action, platform)
			}
		}
	}
}

func TestPlatformLabelsAndDetection(t *testing.T) {
	chord := shortcuts.Chord{Key: "p", Primary: true, Alt: true, Shift: true}
	if got := chord.Label(shortcuts.PlatformMacOS); got != "⌘⌥⇧P" {
		t.Fatalf("mac label = %q", got)
	}
	if got := chord.AccessibleLabel(shortcuts.PlatformMacOS); got != "Command+Option+Shift+P" {
		t.Fatalf("mac accessible label = %q", got)
	}
	if got := chord.Label(shortcuts.PlatformWindows); got != "Ctrl+Alt+Shift+P" {
		t.Fatalf("Windows label = %q", got)
	}

	platforms := map[string]shortcuts.Platform{
		"MacIntel":  shortcuts.PlatformMacOS,
		"darwin":    shortcuts.PlatformMacOS,
		"ios":       shortcuts.PlatformMacOS,
		"iPhone":    shortcuts.PlatformMacOS,
		"Win32":     shortcuts.PlatformWindows,
		"Linux x86": shortcuts.PlatformLinux,
		"Android":   shortcuts.PlatformLinux,
		"unknown":   shortcuts.PlatformOther,
	}
	for raw, want := range platforms {
		if got := shortcuts.ParsePlatform(raw); got != want {
			t.Fatalf("ParsePlatform(%q) = %s, want %s", raw, got, want)
		}
	}
}

func TestTabOrderIsStableAndCallerCannotMutateIt(t *testing.T) {
	want := []shortcuts.FocusRegion{
		shortcuts.FocusRail,
		shortcuts.FocusConversation,
		shortcuts.FocusComposer,
		shortcuts.FocusGraph,
		shortcuts.FocusInspector,
		shortcuts.FocusReview,
	}
	got := shortcuts.TabOrder()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TabOrder = %v, want %v", got, want)
	}
	got[0] = shortcuts.FocusReview
	if next := shortcuts.TabOrder(); next[0] != shortcuts.FocusRail {
		t.Fatalf("caller mutated TabOrder: %v", next)
	}
}

func TestHelpDialogModelIsAccessibleAndDeterministic(t *testing.T) {
	dialog := shortcuts.DefaultPolicy().HelpDialog(shortcuts.PlatformMacOS)
	if dialog.Role != "dialog" || !dialog.AriaModal || !dialog.TrapFocus || !dialog.RestoreFocus || !dialog.CloseOnEscape {
		t.Fatalf("dialog accessibility contract = %+v", dialog)
	}
	if dialog.ID == "" || dialog.Title == "" || dialog.TitleID == "" || dialog.LabelledBy != dialog.TitleID || dialog.DescriptionID == "" || dialog.DescribedBy != dialog.DescriptionID || dialog.CloseControlID == "" || dialog.CloseLabel == "" || dialog.InitialFocusID != dialog.CloseControlID {
		t.Fatalf("dialog labels/identities are incomplete: %+v", dialog)
	}
	var actions []shortcuts.Action
	for _, group := range dialog.Groups {
		if group.ID == "" || group.Heading == "" || len(group.Entries) == 0 {
			t.Fatalf("incomplete help group: %+v", group)
		}
		for _, entry := range group.Entries {
			if entry.Description == "" || entry.KeyLabel == "" || entry.AccessibleKeyLabel == "" {
				t.Fatalf("incomplete help entry: %+v", entry)
			}
			actions = append(actions, entry.Action)
		}
	}
	want := []shortcuts.Action{
		shortcuts.ActionFocusConversation,
		shortcuts.ActionFocusGraph,
		shortcuts.ActionPause,
		shortcuts.ActionStop,
		shortcuts.ActionHelp,
	}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("help actions = %v, want %v", actions, want)
	}
}
