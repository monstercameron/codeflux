package preferences

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// AppearanceStorageKey is a sibling of StorageKey rather than a field inside
// it.
//
// The layout envelope is a strict versioned contract that rejects unknown
// fields and refuses an unrecognized version outright, so adding appearance to
// it would make every existing installation's stored layout unreadable on the
// first boot after the change. Appearance is a small independent preference
// with no restoration semantics, so it gets its own key and can be discarded
// on its own.
const AppearanceStorageKey = "codeflux.ui.appearance"

// AppearanceMotion is the person's override of the operating system's
// reduced-motion preference.
type AppearanceMotion string

const (
	// MotionFollowSystem is the default: whatever the system asks for.
	MotionFollowSystem AppearanceMotion = ""
	MotionFull         AppearanceMotion = "full"
	MotionReduce       AppearanceMotion = "reduce"
)

// IsValid reports whether the motion choice is one this product declares.
func (value AppearanceMotion) IsValid() bool {
	switch value {
	case MotionFollowSystem, MotionFull, MotionReduce:
		return true
	default:
		return false
	}
}

// Appearance is the persisted appearance choice.
//
// Every field is optional and an empty value means "not chosen", which is what
// makes a partially written record safe to read: a person who set the theme
// and never touched density still follows the default density.
type Appearance struct {
	Theme   string
	Density string
	Motion  AppearanceMotion
}

type wireAppearance struct {
	Theme   string `json:"theme,omitempty"`
	Density string `json:"density,omitempty"`
	Motion  string `json:"motion,omitempty"`
}

// AppearanceStore persists the appearance choice.
type AppearanceStore struct {
	backend Backend
}

// NewAppearanceStore creates a store around a supplied backend.
func NewAppearanceStore(backend Backend) (AppearanceStore, error) {
	if backend == nil {
		return AppearanceStore{}, ErrStorageUnavailable
	}
	return AppearanceStore{backend: backend}, nil
}

// OpenBrowserAppearanceStore opens the browser's local storage.
func OpenBrowserAppearanceStore() (AppearanceStore, error) {
	backend, err := openBrowserBackend()
	if err != nil {
		return AppearanceStore{}, err
	}
	return NewAppearanceStore(backend)
}

// Load reads the stored appearance choice. A missing record is not an error:
// it is the ordinary state of a console nobody has configured.
func (store AppearanceStore) Load(ctx context.Context) (Appearance, error) {
	if err := contextError(ctx); err != nil {
		return Appearance{}, err
	}
	if store.backend == nil {
		return Appearance{}, ErrStorageUnavailable
	}
	payload, found, err := store.backend.GetItem(AppearanceStorageKey)
	if err != nil {
		return Appearance{}, fmt.Errorf("load appearance preferences: %w", err)
	}
	if !found || strings.TrimSpace(payload) == "" {
		return Appearance{}, nil
	}
	var wire wireAppearance
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		// A record this build cannot read is discarded rather than repaired:
		// it only holds a colour scheme, and guessing at it would be a
		// preference nobody expressed.
		return Appearance{}, nil
	}
	value := Appearance{
		Theme: wire.Theme, Density: wire.Density, Motion: AppearanceMotion(wire.Motion),
	}
	if !value.Motion.IsValid() {
		value.Motion = MotionFollowSystem
	}
	return value, nil
}

// Save replaces the stored appearance choice.
func (store AppearanceStore) Save(ctx context.Context, value Appearance) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if store.backend == nil {
		return ErrStorageUnavailable
	}
	if !value.Motion.IsValid() {
		return fmt.Errorf("%w: motion %q", ErrMalformed, value.Motion)
	}
	payload, err := json.Marshal(wireAppearance{
		Theme: value.Theme, Density: value.Density, Motion: string(value.Motion),
	})
	if err != nil {
		return fmt.Errorf("save appearance preferences: %w", err)
	}
	if err := store.backend.SetItem(AppearanceStorageKey, string(payload)); err != nil {
		return fmt.Errorf("save appearance preferences: %w", err)
	}
	return nil
}

// Clear forgets the stored appearance choice.
func (store AppearanceStore) Clear(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if store.backend == nil {
		return ErrStorageUnavailable
	}
	if err := store.backend.RemoveItem(AppearanceStorageKey); err != nil {
		return fmt.Errorf("clear appearance preferences: %w", err)
	}
	return nil
}
