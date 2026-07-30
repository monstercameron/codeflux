//go:build windows

package credentials

import (
	"context"
	"errors"
	"testing"
)

func TestWindowsStoreCreateUpdateRetrieveTestDelete(t *testing.T) {
	api := &memoryCredentialManager{values: make(map[string][]byte)}
	store := &WindowsStore{api: api}
	reference, err := NewReference("openai", "unit-test")
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewSecret([]byte("first-value"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Destroy()
	if err := store.Create(context.Background(), reference, first); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(context.Background(), reference, first); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate create error = %v", err)
	}
	second, err := NewSecret([]byte("second-value"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Destroy()
	if err := store.Update(context.Background(), reference, second); err != nil {
		t.Fatal(err)
	}
	if err := store.Test(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
	retrieved, err := store.Retrieve(context.Background(), reference)
	if err != nil {
		t.Fatal(err)
	}
	defer retrieved.Destroy()
	if err := retrieved.Use(func(value []byte) error {
		if string(value) != "second-value" {
			t.Fatalf("retrieved value = %q", value)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Retrieve(context.Background(), reference); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retrieve after delete error = %v", err)
	}
}

type memoryCredentialManager struct {
	values map[string][]byte
}

func (manager *memoryCredentialManager) Write(target, account string, value []byte) error {
	manager.values[target] = append([]byte(nil), value...)
	return nil
}

func (manager *memoryCredentialManager) Read(target string) ([]byte, error) {
	value, ok := manager.values[target]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (manager *memoryCredentialManager) Delete(target string) error {
	if _, ok := manager.values[target]; !ok {
		return ErrNotFound
	}
	delete(manager.values, target)
	return nil
}
