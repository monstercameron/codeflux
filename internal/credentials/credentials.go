// Package credentials owns opaque provider-secret access and storage.
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
)

var (
	ErrNotFound      = errors.New("credential not found")
	ErrAlreadyExists = errors.New("credential already exists")
	ErrUnavailable   = errors.New("credential store unavailable")
	ErrReadOnly      = errors.New("credential store is read-only")
)

// Reference is a non-secret opaque identity safe to persist in SQLite.
type Reference struct {
	Service string
	Account string
}

// NewReference validates one credential identity.
func NewReference(service, account string) (Reference, error) {
	reference := Reference{
		Service: strings.TrimSpace(service),
		Account: strings.TrimSpace(account),
	}
	if reference.Service == "" ||
		reference.Account == "" ||
		len(reference.Service) > 128 ||
		len(reference.Account) > 128 ||
		strings.ContainsAny(reference.Service, "/\x00\r\n") ||
		strings.ContainsAny(reference.Account, "/\x00\r\n") {
		return Reference{}, errors.New("credential reference is invalid")
	}
	return reference, nil
}

// Opaque returns the stable non-secret representation persisted by callers.
func (reference Reference) Opaque() string {
	return "os://" + reference.Service + "/" + reference.Account
}

// Secret contains provider credential bytes without exposing string or marshal
// operations. Callers can access a temporary copy only through Use.
type Secret struct {
	value []byte
}

// NewSecret copies non-empty secret material into an opaque value.
func NewSecret(value []byte) (Secret, error) {
	if len(value) == 0 || len(value) > 2560 {
		return Secret{}, errors.New("credential value must contain 1 to 2560 bytes")
	}
	copied := append([]byte(nil), value...)
	return Secret{value: copied}, nil
}

// Use supplies a temporary copy and zeroes it immediately afterward.
func (secret Secret) Use(operation func([]byte) error) error {
	if operation == nil {
		return errors.New("credential operation must not be nil")
	}
	if len(secret.value) == 0 {
		return ErrNotFound
	}
	copied := append([]byte(nil), secret.value...)
	defer zero(copied)
	return operation(copied)
}

// Destroy zeroes this instance's owned memory.
func (secret *Secret) Destroy() {
	if secret == nil {
		return
	}
	zero(secret.value)
	secret.value = nil
}

func (Secret) String() string   { return "[REDACTED]" }
func (Secret) GoString() string { return "[REDACTED]" }

// Format prevents every fmt verb from exposing the backing bytes.
func (Secret) Format(state fmt.State, verb rune) {
	_, _ = state.Write([]byte("[REDACTED]"))
}

// MarshalJSON refuses serialization instead of emitting either content or a
// misleading placeholder into a correctness-bearing payload.
func (Secret) MarshalJSON() ([]byte, error) {
	return nil, errors.New("credential values cannot be serialized")
}

// MarshalText refuses accidental text serialization.
func (Secret) MarshalText() ([]byte, error) {
	return nil, errors.New("credential values cannot be serialized")
}

// Store defines create, update, retrieve, test, and delete without revealing a
// credential through diagnostics.
type Store interface {
	Create(context.Context, Reference, Secret) error
	Update(context.Context, Reference, Secret) error
	Retrieve(context.Context, Reference) (Secret, error)
	Test(context.Context, Reference) error
	Delete(context.Context, Reference) error
}

// EnvironmentStore is an explicit read-only fallback from references to named
// environment variables.
type EnvironmentStore struct {
	variables map[Reference]string
	lookup    func(string) (string, bool)
}

// NewEnvironmentStore constructs a fallback with an explicit reference map.
func NewEnvironmentStore(
	variables map[Reference]string,
	lookup func(string) (string, bool),
) (*EnvironmentStore, error) {
	if lookup == nil {
		return nil, errors.New("environment lookup must not be nil")
	}
	copied := make(map[Reference]string, len(variables))
	for reference, variable := range variables {
		if _, err := NewReference(reference.Service, reference.Account); err != nil {
			return nil, err
		}
		if !validEnvironmentName(variable) {
			return nil, errors.New("credential environment variable name is invalid")
		}
		copied[reference] = variable
	}
	return &EnvironmentStore{variables: copied, lookup: lookup}, nil
}

func (*EnvironmentStore) Create(context.Context, Reference, Secret) error {
	return ErrReadOnly
}

func (*EnvironmentStore) Update(context.Context, Reference, Secret) error {
	return ErrReadOnly
}

func (store *EnvironmentStore) Retrieve(
	ctx context.Context,
	reference Reference,
) (Secret, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, err
	}
	variable, ok := store.variables[reference]
	if !ok {
		return Secret{}, ErrNotFound
	}
	value, ok := store.lookup(variable)
	if !ok || value == "" {
		return Secret{}, ErrNotFound
	}
	return NewSecret([]byte(value))
}

func (store *EnvironmentStore) Test(
	ctx context.Context,
	reference Reference,
) error {
	secret, err := store.Retrieve(ctx, reference)
	if err != nil {
		return err
	}
	secret.Destroy()
	return nil
}

func (*EnvironmentStore) Delete(context.Context, Reference) error {
	return ErrReadOnly
}

// UnavailableStore returns stable platform-unavailable errors.
type UnavailableStore struct {
	platform string
}

// NewUnavailableStore creates an honest unsupported-platform implementation.
func NewUnavailableStore(platform string) *UnavailableStore {
	return &UnavailableStore{platform: platform}
}

func (store *UnavailableStore) unavailable() error {
	return fmt.Errorf("%w: %s", ErrUnavailable, store.platform)
}

func (store *UnavailableStore) Create(context.Context, Reference, Secret) error {
	return store.unavailable()
}

func (store *UnavailableStore) Update(context.Context, Reference, Secret) error {
	return store.unavailable()
}

func (store *UnavailableStore) Retrieve(context.Context, Reference) (Secret, error) {
	return Secret{}, store.unavailable()
}

func (store *UnavailableStore) Test(context.Context, Reference) error {
	return store.unavailable()
}

func (store *UnavailableStore) Delete(context.Context, Reference) error {
	return store.unavailable()
}

func validEnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character >= 'A' && character <= 'Z' ||
			index > 0 && character >= '0' && character <= '9' ||
			index > 0 && character == '_' {
			continue
		}
		return false
	}
	return true
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
	runtime.KeepAlive(value)
}

var _ fmt.Formatter = Secret{}
var _ json.Marshaler = Secret{}
