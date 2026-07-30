package providers

import (
	"errors"
	"testing"
)

func TestNewProviderUsageDistinguishesReportedZeroFromOmission(t *testing.T) {
	reported, err := NewProviderUsage(true, 0, 0, 0, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reported.Known || reported.Source != UsageSourceProvider {
		t.Fatalf("reported zero usage = %#v", reported)
	}
	unknown, err := NewProviderUsage(false, 0, 0, 0, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Known || unknown.Source != UsageSourceUnknown {
		t.Fatalf("omitted usage = %#v", unknown)
	}
}

func TestNewProviderUsageRejectsValuesWhenObjectWasOmitted(t *testing.T) {
	_, err := NewProviderUsage(false, 1, 0, 0, 0, 0, nil)
	if !errors.Is(err, ErrInvalidProviderUsage) {
		t.Fatalf("omitted usage error = %v", err)
	}
}
