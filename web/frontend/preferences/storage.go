package preferences

import (
	"context"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/web/frontend/routes"
)

// Backend is the minimum storage surface required by preference persistence.
// Browser builds adapt GWC local storage to this interface.
type Backend interface {
	GetItem(string) (string, bool, error)
	SetItem(string, string) error
	RemoveItem(string) error
}

// Store persists one versioned preference envelope.
type Store struct {
	backend Backend
	key     string
}

// NewStore creates a store around a supplied backend. This is useful for
// deterministic tests and for browser storage wrappers owned by callers.
func NewStore(backend Backend) (Store, error) {
	if backend == nil {
		return Store{}, ErrStorageUnavailable
	}
	return Store{backend: backend, key: StorageKey}, nil
}

// OpenBrowserStore opens the GWC local-storage adapter on WebAssembly builds.
func OpenBrowserStore() (Store, error) {
	backend, err := openBrowserBackend()
	if err != nil {
		return Store{}, err
	}
	return NewStore(backend)
}

// Save normalizes and replaces the persisted preference envelope.
func (s Store) Save(ctx context.Context, record Record) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	payload, err := Encode(record)
	if err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if err := s.backend.SetItem(s.key, string(payload)); err != nil {
		return fmt.Errorf("save frontend preferences: %w", err)
	}
	return nil
}

// Load returns a strictly decoded, normalized preference record.
func (s Store) Load(ctx context.Context) (Record, error) {
	if err := contextError(ctx); err != nil {
		return Record{}, err
	}
	if err := s.ready(); err != nil {
		return Record{}, err
	}
	payload, ok, err := s.backend.GetItem(s.key)
	if err != nil {
		return Record{}, fmt.Errorf("load frontend preferences: %w", err)
	}
	if !ok {
		return Record{}, ErrNotFound
	}
	return Decode([]byte(payload))
}

// LoadAndRestore loads preferences and applies current route authorization and
// compatibility checks before returning them.
func (s Store) LoadAndRestore(ctx context.Context, restorationContext routes.RestorationContext) (RestoredPreferences, error) {
	record, err := s.Load(ctx)
	if err != nil {
		return RestoredPreferences{}, err
	}
	return RestoreRecord(record, restorationContext)
}

// Clear removes only CodeFlux's preference envelope.
func (s Store) Clear(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if err := s.backend.RemoveItem(s.key); err != nil {
		return fmt.Errorf("clear frontend preferences: %w", err)
	}
	return nil
}

func (s Store) ready() error {
	if s.backend == nil || strings.TrimSpace(s.key) == "" {
		return ErrStorageUnavailable
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
