package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestResolveUsesFixedPrecedenceAndRequiresRepositoryApproval(t *testing.T) {
	defaults := validSettings(t)
	userPolicy := domain.PolicyPresetFast
	repositoryPolicy := domain.PolicyPresetEconomical
	taskPolicy := domain.PolicyPresetCorrectness
	endpoint := "https://models.example.test/v1"
	unapproved := RepositoryOverlay{
		Settings: Overlay{
			ProviderEndpoint: &endpoint,
			PolicyPreset:     &repositoryPolicy,
		},
	}
	if _, err := Resolve(
		defaults,
		Overlay{PolicyPreset: &userPolicy},
		unapproved,
		Overlay{PolicyPreset: &taskPolicy},
	); !errors.Is(err, ErrRepositoryApprovalRequired) {
		t.Fatalf("unapproved repository error = %v", err)
	}
	unapproved.Approved = true
	unapproved.ApprovalReference = "reviewed-settings-1"
	effective, err := Resolve(
		defaults,
		Overlay{PolicyPreset: &userPolicy},
		unapproved,
		Overlay{PolicyPreset: &taskPolicy},
	)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Settings.ProviderEndpoint != endpoint ||
		effective.Sources[KeyProviderEndpoint] != SourceRepository {
		t.Fatalf("provider endpoint = %#v", effective)
	}
	if effective.Settings.PolicyPreset != taskPolicy ||
		effective.Sources[KeyPolicyPreset] != SourceTask {
		t.Fatalf("policy = %#v", effective)
	}
	if effective.Sources[KeyHardBudget] != SourceDefault {
		t.Fatalf("hard budget source = %q", effective.Sources[KeyHardBudget])
	}
}

func TestSettingsValidateSecurityBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Settings)
	}{
		{
			name: "remote plain HTTP endpoint",
			change: func(settings *Settings) {
				settings.ProviderEndpoint = "http://models.example.test/v1"
			},
		},
		{
			name: "endpoint user information",
			change: func(settings *Settings) {
				settings.ProviderEndpoint = "https://user:pass@models.example.test/v1"
			},
		},
		{
			name: "endpoint query",
			change: func(settings *Settings) {
				settings.ProviderEndpoint = "https://models.example.test/v1?key=value"
			},
		},
		{
			name: "negative budget",
			change: func(settings *Settings) {
				settings.HardBudget.MinorUnits = -1
			},
		},
		{
			name: "short timeout",
			change: func(settings *Settings) {
				settings.RequestTimeout = 999
			},
		},
		{
			name: "filesystem root",
			change: func(settings *Settings) {
				volume := filepath.VolumeName(settings.WorktreeRoot)
				settings.WorktreeRoot = filepath.Clean(volume + string(filepath.Separator))
			},
		},
		{
			name: "invalid policy",
			change: func(settings *Settings) {
				settings.PolicyPreset = "unsafe"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := validSettings(t)
			test.change(&settings)
			if err := settings.Validate(); err == nil {
				t.Fatal("validation succeeded")
			}
		})
	}
}

func TestEnvironmentOverlayAllowsOnlyDeclaredNonSecretKeys(t *testing.T) {
	overlay, err := EnvironmentOverlay(map[string]string{
		"CODEFLUX_PROVIDER_ENDPOINT": "http://127.0.0.1:11434/v1",
		"CODEFLUX_HARD_BUDGET_MINOR": "2500",
		"CODEFLUX_BUDGET_CURRENCY":   "USD",
		"CODEFLUX_REQUEST_TIMEOUT":   "45s",
		"CODEFLUX_POLICY_PRESET":     "balanced",
		"UNRELATED":                  "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if overlay.ProviderEndpoint == nil ||
		overlay.HardBudget == nil ||
		overlay.HardBudget.MinorUnits != 2500 ||
		overlay.RequestTimeout == nil ||
		*overlay.RequestTimeout != 45_000 ||
		overlay.PolicyPreset == nil {
		t.Fatalf("environment overlay = %#v", overlay)
	}
	_, err = EnvironmentOverlay(map[string]string{
		"CODEFLUX_PROVIDER_API_KEY": "must-not-be-a-setting",
	})
	if !errors.Is(err, ErrUnknownSetting) {
		t.Fatalf("unknown setting error = %v", err)
	}
}

func TestImportExportIsBoundedStrictAndContainsNoPrivateFields(t *testing.T) {
	settings := validSettings(t)
	data, err := ExportJSON(settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"credential", "secret", "api_key", "task_data"} {
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Fatalf("export contains forbidden field %q: %s", forbidden, data)
		}
	}
	imported, err := ImportJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if imported != settings {
		t.Fatalf("imported = %#v, want %#v", imported, settings)
	}
	if _, err := ImportJSON([]byte(`{"api_key":"not-a-real-secret"}`)); err == nil {
		t.Fatal("unknown secret-shaped field was accepted")
	}
	if _, err := ImportJSON(make([]byte, maximumImportBytes+1)); err == nil {
		t.Fatal("oversize import was accepted")
	}
	if _, err := ImportJSON(append(data, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func validSettings(t *testing.T) Settings {
	t.Helper()
	currency, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	budget, err := domain.NewMoney(currency, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	return Settings{
		ProviderEndpoint: "http://localhost:11434/v1",
		HardBudget:       budget,
		RequestTimeout:   domain.Milliseconds((2 * time.Minute) / time.Millisecond),
		WorktreeRoot:     filepath.Join(t.TempDir(), "worktrees"),
		PolicyPreset:     domain.PolicyPresetBalanced,
	}
}
