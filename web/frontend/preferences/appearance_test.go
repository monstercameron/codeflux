package preferences_test

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/web/frontend/preferences"
)

type appearanceBackend struct{ items map[string]string }

func (backend *appearanceBackend) GetItem(key string) (string, bool, error) {
	value, found := backend.items[key]
	return value, found, nil
}
func (backend *appearanceBackend) SetItem(key, value string) error {
	backend.items[key] = value
	return nil
}
func (backend *appearanceBackend) RemoveItem(key string) error {
	delete(backend.items, key)
	return nil
}

// TestAppearanceRoundTripsAndStaysOutOfTheLayoutEnvelope proves the appearance
// choice survives a reload and is stored beside the layout envelope rather
// than inside it, so a change here can never make a stored layout unreadable.
func TestAppearanceRoundTripsAndStaysOutOfTheLayoutEnvelope(t *testing.T) {
	backend := &appearanceBackend{items: map[string]string{}}
	store, err := preferences.NewAppearanceStore(backend)
	if err != nil {
		t.Fatal(err)
	}
	saved := preferences.Appearance{
		Theme: "light", Density: "compact", Motion: preferences.MotionReduce,
	}
	if err := store.Save(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	if _, holdsLayout := backend.items[preferences.StorageKey]; holdsLayout {
		t.Fatal("appearance was written into the layout envelope")
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded != saved {
		t.Fatalf("loaded = %#v err = %v, want %#v", loaded, err, saved)
	}
	if err := store.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	empty, err := store.Load(context.Background())
	if err != nil || empty != (preferences.Appearance{}) {
		t.Fatalf("after clear = %#v err = %v", empty, err)
	}
}

// TestAppearanceDiscardsARecordItCannotRead keeps an unreadable colour scheme
// from becoming an error a person has to resolve.
func TestAppearanceDiscardsARecordItCannotRead(t *testing.T) {
	backend := &appearanceBackend{items: map[string]string{
		preferences.AppearanceStorageKey: `{"theme":"light","unknown":true}`,
	}}
	store, err := preferences.NewAppearanceStore(backend)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded != (preferences.Appearance{}) {
		t.Fatalf("loaded = %#v err = %v, want the zero appearance", loaded, err)
	}
}
