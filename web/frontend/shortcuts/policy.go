// Package shortcuts defines CodeFlux's deterministic, browser-safe keyboard
// shortcut and focus-order policy independently from shell rendering.
package shortcuts

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Action identifies one application command produced by a shortcut.
type Action string

const (
	ActionFocusConversation Action = "focus-conversation"
	ActionFocusGraph        Action = "focus-graph"
	ActionPause             Action = "pause"
	ActionStop              Action = "stop"
	ActionHelp              Action = "help"
)

// Scope identifies the application region in which a key event originated.
type Scope string

const (
	ScopeApplication  Scope = "application"
	ScopeConversation Scope = "conversation"
	ScopeComposer     Scope = "composer"
	ScopeGraph        Scope = "graph"
)

// TargetKind classifies whether the event target accepts typed content.
// TargetContentEditable includes descendants of an effective contenteditable
// ancestor, not only the element that declares the attribute.
type TargetKind string

const (
	TargetOther           TargetKind = "other"
	TargetInput           TargetKind = "input"
	TargetTextArea        TargetKind = "textarea"
	TargetSelect          TargetKind = "select"
	TargetContentEditable TargetKind = "contenteditable"
)

// IsTyping reports whether ordinary application shortcuts must be suppressed.
func (kind TargetKind) IsTyping() bool {
	switch kind {
	case TargetInput, TargetTextArea, TargetSelect, TargetContentEditable:
		return true
	default:
		return false
	}
}

// ClassifyTarget converts normalized browser target metadata into the editing
// classification required by Resolve.
func ClassifyTarget(tagName string, effectiveContentEditable bool) TargetKind {
	if effectiveContentEditable {
		return TargetContentEditable
	}
	switch strings.ToLower(strings.TrimSpace(tagName)) {
	case "input":
		return TargetInput
	case "textarea":
		return TargetTextArea
	case "select":
		return TargetSelect
	default:
		return TargetOther
	}
}

// Chord is a physical modifier-independent shortcut definition. Primary means
// Command on macOS and Control on other platforms.
type Chord struct {
	Key     string
	Primary bool
	Alt     bool
	Shift   bool
}

// Binding maps one chord to one action. Editing is opt-in and is valid only for
// a binding restricted to a non-application scope.
type Binding struct {
	Action            Action
	Chord             Chord
	Scope             Scope
	AllowWhileEditing bool
}

// Event is the normalized keyboard input consumed by Policy.Resolve.
type Event struct {
	Key       string
	Ctrl      bool
	Meta      bool
	Alt       bool
	Shift     bool
	Repeat    bool
	Composing bool
	Target    TargetKind
	Scope     Scope
}

// SuppressionReason explains why a key event did not produce an action.
type SuppressionReason string

const (
	SuppressionNone      SuppressionReason = ""
	SuppressionNoMatch   SuppressionReason = "no-match"
	SuppressionTyping    SuppressionReason = "typing"
	SuppressionComposing SuppressionReason = "composing"
	SuppressionRepeat    SuppressionReason = "repeat"
	SuppressionScope     SuppressionReason = "scope"
)

// Decision is the deterministic result of resolving one key event.
type Decision struct {
	Action         Action
	Handled        bool
	PreventDefault bool
	Reason         SuppressionReason
}

// Policy is an immutable validated shortcut map.
type Policy struct {
	bindings []Binding
}

var (
	ErrInvalidBinding = errors.New("frontend shortcuts: invalid binding")
	ErrCollision      = errors.New("frontend shortcuts: chord collision")
	ErrReservedChord  = errors.New("frontend shortcuts: browser-reserved chord")
)

// DefaultPolicy returns the stable CodeFlux application shortcut policy.
func DefaultPolicy() Policy {
	policy, err := NewPolicy([]Binding{
		{Action: ActionFocusConversation, Chord: Chord{Key: "1", Primary: true, Alt: true}, Scope: ScopeApplication},
		{Action: ActionFocusGraph, Chord: Chord{Key: "2", Primary: true, Alt: true}, Scope: ScopeApplication},
		{Action: ActionPause, Chord: Chord{Key: "a", Primary: true, Alt: true}, Scope: ScopeApplication},
		{Action: ActionStop, Chord: Chord{Key: "x", Primary: true, Alt: true}, Scope: ScopeApplication},
		{Action: ActionHelp, Chord: Chord{Key: "/", Primary: true, Alt: true}, Scope: ScopeApplication},
	})
	if err != nil {
		panic(err)
	}
	return policy
}

// NewPolicy validates and copies bindings so resolution cannot change through
// caller-owned slices.
func NewPolicy(bindings []Binding) (Policy, error) {
	if len(bindings) == 0 {
		return Policy{}, fmt.Errorf("%w: empty policy", ErrInvalidBinding)
	}
	copyBindings := slices.Clone(bindings)
	actions := make(map[Action]struct{}, len(copyBindings))
	chords := make(map[string]Action, len(copyBindings))
	for index := range copyBindings {
		binding := &copyBindings[index]
		binding.Chord.Key = normalizeKey(binding.Chord.Key)
		if !validAction(binding.Action) || !validScope(binding.Scope) || binding.Chord.Key == "" {
			return Policy{}, fmt.Errorf("%w: action=%q", ErrInvalidBinding, binding.Action)
		}
		if !binding.Chord.Primary && !binding.Chord.Alt {
			return Policy{}, fmt.Errorf("%w: %s requires a modifier", ErrInvalidBinding, binding.Action)
		}
		if binding.AllowWhileEditing && binding.Scope == ScopeApplication {
			return Policy{}, fmt.Errorf("%w: editing requires an explicit scope", ErrInvalidBinding)
		}
		if _, duplicate := actions[binding.Action]; duplicate {
			return Policy{}, fmt.Errorf("%w: duplicate action %q", ErrInvalidBinding, binding.Action)
		}
		actions[binding.Action] = struct{}{}
		signature := chordSignature(binding.Chord)
		if previous, collision := chords[signature]; collision {
			return Policy{}, fmt.Errorf("%w: %s and %s", ErrCollision, previous, binding.Action)
		}
		chords[signature] = binding.Action
		for _, platform := range []Platform{PlatformMacOS, PlatformWindows, PlatformLinux, PlatformOther} {
			if IsBrowserReserved(binding.Chord, platform) {
				return Policy{}, fmt.Errorf("%w: %s", ErrReservedChord, binding.Chord.Label(platform))
			}
		}
	}
	return Policy{bindings: copyBindings}, nil
}

// Bindings returns an independent copy in deterministic policy order.
func (policy Policy) Bindings() []Binding { return slices.Clone(policy.bindings) }

// Resolve maps one event to at most one action. Only handled events request
// preventDefault; suppressed or unrelated browser behavior remains untouched.
func (policy Policy) Resolve(event Event, platform Platform) Decision {
	if event.Composing {
		return Decision{Reason: SuppressionComposing}
	}
	if event.Repeat {
		return Decision{Reason: SuppressionRepeat}
	}
	key := normalizeKey(event.Key)
	for _, binding := range policy.bindings {
		if !matchesChord(binding.Chord, event, platform, key) {
			continue
		}
		if binding.Scope != ScopeApplication && binding.Scope != event.Scope {
			return Decision{Reason: SuppressionScope}
		}
		if event.Target.IsTyping() {
			if !binding.AllowWhileEditing || binding.Scope == ScopeApplication || binding.Scope != event.Scope {
				return Decision{Reason: SuppressionTyping}
			}
		}
		return Decision{Action: binding.Action, Handled: true, PreventDefault: true}
	}
	return Decision{Reason: SuppressionNoMatch}
}

func matchesChord(chord Chord, event Event, platform Platform, key string) bool {
	if normalizeKey(chord.Key) != key || chord.Alt != event.Alt || chord.Shift != event.Shift {
		return false
	}
	if platform == PlatformMacOS {
		return chord.Primary == event.Meta && !event.Ctrl
	}
	return chord.Primary == event.Ctrl && !event.Meta
}

func validAction(action Action) bool {
	switch action {
	case ActionFocusConversation, ActionFocusGraph, ActionPause, ActionStop, ActionHelp:
		return true
	default:
		return false
	}
}

func validScope(scope Scope) bool {
	switch scope {
	case ScopeApplication, ScopeConversation, ScopeComposer, ScopeGraph:
		return true
	default:
		return false
	}
}

func normalizeKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func chordSignature(chord Chord) string {
	parts := []string{normalizeKey(chord.Key)}
	if chord.Primary {
		parts = append(parts, "primary")
	}
	if chord.Alt {
		parts = append(parts, "alt")
	}
	if chord.Shift {
		parts = append(parts, "shift")
	}
	sort.Strings(parts[1:])
	return strings.Join(parts, "+")
}
