package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSecretCannotBeFormattedOrSerialized(t *testing.T) {
	material := []byte("fixture-provider-value")
	secret, err := NewSecret(material)
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Destroy()
	formatted := []string{
		fmt.Sprint(secret),
		fmt.Sprintf("%s", secret),
		fmt.Sprintf("%q", secret),
		fmt.Sprintf("%x", secret),
		fmt.Sprintf("%#v", secret),
	}
	for _, output := range formatted {
		if output != "[REDACTED]" || strings.Contains(output, string(material)) {
			t.Fatalf("formatted secret = %q", output)
		}
	}
	if _, err := json.Marshal(secret); err == nil {
		t.Fatal("secret JSON serialization succeeded")
	}
	if _, err := secret.MarshalText(); err == nil {
		t.Fatal("secret text serialization succeeded")
	}
}

func TestSecretUseCopiesAndDestroysTemporaryMaterial(t *testing.T) {
	secret, err := NewSecret([]byte("temporary-value"))
	if err != nil {
		t.Fatal(err)
	}
	var captured []byte
	if err := secret.Use(func(value []byte) error {
		captured = value
		value[0] = 'X'
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, value := range captured {
		if value != 0 {
			t.Fatalf("temporary copy was not zeroed: %v", captured)
		}
	}
	if err := secret.Use(func(value []byte) error {
		if string(value) != "temporary-value" {
			t.Fatalf("owned value changed: %q", value)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	secret.Destroy()
	if err := secret.Use(func([]byte) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("destroyed secret error = %v", err)
	}
}

func TestEnvironmentStoreIsExplicitReadOnlyFallback(t *testing.T) {
	reference, err := NewReference("openai", "personal")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewEnvironmentStore(
		map[Reference]string{reference: "OPENAI_API_KEY"},
		func(name string) (string, bool) {
			if name == "OPENAI_API_KEY" {
				return "environment-fixture", true
			}
			return "", false
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Test(context.Background(), reference); err != nil {
		t.Fatal(err)
	}
	secret, err := store.Retrieve(context.Background(), reference)
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Destroy()
	if err := secret.Use(func(value []byte) error {
		if string(value) != "environment-fixture" {
			t.Fatalf("retrieved value = %q", value)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(context.Background(), reference, secret); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("create error = %v", err)
	}
	if err := store.Delete(context.Background(), reference); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("delete error = %v", err)
	}
}
