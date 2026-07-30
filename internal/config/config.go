// Package config resolves validated non-secret Codeflux settings.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const maximumImportBytes = 64 * 1024

var (
	// ErrRepositoryApprovalRequired means repository settings were not admitted
	// by an explicit first-use review.
	ErrRepositoryApprovalRequired = errors.New("repository settings require approval")
	// ErrUnknownSetting means a security-sensitive or unsupported key was
	// supplied rather than silently ignored.
	ErrUnknownSetting = errors.New("unknown configuration setting")
)

// Source identifies the winning layer for one effective setting.
type Source string

const (
	SourceDefault    Source = "default"
	SourceUser       Source = "user"
	SourceRepository Source = "repository"
	SourceTask       Source = "task"
)

// Key is one supported non-secret setting.
type Key string

const (
	KeyProviderEndpoint Key = "provider_endpoint"
	KeyHardBudget       Key = "hard_budget"
	KeyRequestTimeout   Key = "request_timeout_ms"
	KeyWorktreeRoot     Key = "worktree_root"
	KeyPolicyPreset     Key = "policy_preset"
)

// Settings is a complete validated non-secret configuration snapshot.
type Settings struct {
	ProviderEndpoint string              `json:"provider_endpoint,omitempty"`
	HardBudget       domain.Money        `json:"hard_budget"`
	RequestTimeout   domain.Milliseconds `json:"request_timeout_ms"`
	WorktreeRoot     string              `json:"worktree_root"`
	PolicyPreset     domain.PolicyPreset `json:"policy_preset"`
}

// Overlay contains only values explicitly supplied by one precedence layer.
type Overlay struct {
	ProviderEndpoint *string              `json:"provider_endpoint,omitempty"`
	HardBudget       *domain.Money        `json:"hard_budget,omitempty"`
	RequestTimeout   *domain.Milliseconds `json:"request_timeout_ms,omitempty"`
	WorktreeRoot     *string              `json:"worktree_root,omitempty"`
	PolicyPreset     *domain.PolicyPreset `json:"policy_preset,omitempty"`
}

// RepositoryOverlay binds repository-provided settings to explicit approval.
type RepositoryOverlay struct {
	Settings          Overlay
	Approved          bool
	ApprovalReference string
}

// Effective is the resolved configuration and the attributable winning layer
// for every field.
type Effective struct {
	Settings Settings
	Sources  map[Key]Source
}

// Resolve applies the fixed precedence task > approved repository > user >
// defaults and validates the resulting non-secret snapshot.
func Resolve(
	defaults Settings,
	user Overlay,
	repository RepositoryOverlay,
	task Overlay,
) (Effective, error) {
	if err := defaults.Validate(); err != nil {
		return Effective{}, fmt.Errorf("validate defaults: %w", err)
	}
	if !repository.Settings.empty() {
		if !repository.Approved ||
			strings.TrimSpace(repository.ApprovalReference) == "" {
			return Effective{}, ErrRepositoryApprovalRequired
		}
	}
	effective := Effective{
		Settings: defaults,
		Sources: map[Key]Source{
			KeyProviderEndpoint: SourceDefault,
			KeyHardBudget:       SourceDefault,
			KeyRequestTimeout:   SourceDefault,
			KeyWorktreeRoot:     SourceDefault,
			KeyPolicyPreset:     SourceDefault,
		},
	}
	apply(&effective, user, SourceUser)
	apply(&effective, repository.Settings, SourceRepository)
	apply(&effective, task, SourceTask)
	if err := effective.Settings.Validate(); err != nil {
		return Effective{}, err
	}
	return effective, nil
}

func apply(effective *Effective, overlay Overlay, source Source) {
	if overlay.ProviderEndpoint != nil {
		effective.Settings.ProviderEndpoint = *overlay.ProviderEndpoint
		effective.Sources[KeyProviderEndpoint] = source
	}
	if overlay.HardBudget != nil {
		effective.Settings.HardBudget = *overlay.HardBudget
		effective.Sources[KeyHardBudget] = source
	}
	if overlay.RequestTimeout != nil {
		effective.Settings.RequestTimeout = *overlay.RequestTimeout
		effective.Sources[KeyRequestTimeout] = source
	}
	if overlay.WorktreeRoot != nil {
		effective.Settings.WorktreeRoot = *overlay.WorktreeRoot
		effective.Sources[KeyWorktreeRoot] = source
	}
	if overlay.PolicyPreset != nil {
		effective.Settings.PolicyPreset = *overlay.PolicyPreset
		effective.Sources[KeyPolicyPreset] = source
	}
}

func (overlay Overlay) empty() bool {
	return overlay.ProviderEndpoint == nil &&
		overlay.HardBudget == nil &&
		overlay.RequestTimeout == nil &&
		overlay.WorktreeRoot == nil &&
		overlay.PolicyPreset == nil
}

// Validate checks endpoint, exact budget, timeout, worktree, and policy values.
func (settings Settings) Validate() error {
	if settings.ProviderEndpoint != "" {
		endpoint, err := url.Parse(settings.ProviderEndpoint)
		if err != nil ||
			endpoint.Host == "" ||
			endpoint.User != nil ||
			endpoint.Fragment != "" {
			return errors.New("provider endpoint must be an absolute URL without user information or fragment")
		}
		switch endpoint.Scheme {
		case "https":
		case "http":
			if !isLoopbackHost(endpoint.Hostname()) {
				return errors.New("non-loopback provider endpoint must use HTTPS")
			}
		default:
			return errors.New("provider endpoint scheme must be HTTPS or loopback HTTP")
		}
	}
	if err := settings.HardBudget.Validate(); err != nil {
		return fmt.Errorf("hard budget: %w", err)
	}
	if settings.HardBudget.MinorUnits < 0 {
		return errors.New("hard budget must not be negative")
	}
	if settings.RequestTimeout < domain.Milliseconds(time.Second/time.Millisecond) ||
		settings.RequestTimeout > domain.Milliseconds((10*time.Minute)/time.Millisecond) {
		return errors.New("request timeout must be between 1 second and 10 minutes")
	}
	if !filepath.IsAbs(settings.WorktreeRoot) ||
		filepath.Clean(settings.WorktreeRoot) != settings.WorktreeRoot ||
		isFilesystemRoot(settings.WorktreeRoot) {
		return errors.New("worktree root must be a clean absolute non-root path")
	}
	if !settings.PolicyPreset.IsValid() {
		return errors.New("policy preset is invalid")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func isFilesystemRoot(path string) bool {
	volume := filepath.VolumeName(path)
	return filepath.Clean(path) == filepath.Clean(volume+string(filepath.Separator))
}

// EnvironmentOverlay parses only the explicitly supported non-secret
// CODEFLUX_* environment variables and rejects every other Codeflux key.
func EnvironmentOverlay(values map[string]string) (Overlay, error) {
	allowed := map[string]bool{
		"CODEFLUX_PROVIDER_ENDPOINT": true,
		"CODEFLUX_HARD_BUDGET_MINOR": true,
		"CODEFLUX_BUDGET_CURRENCY":   true,
		"CODEFLUX_REQUEST_TIMEOUT":   true,
		"CODEFLUX_POLICY_PRESET":     true,
	}
	for key := range values {
		if strings.HasPrefix(key, "CODEFLUX_") && !allowed[key] {
			return Overlay{}, fmt.Errorf("%w: %s", ErrUnknownSetting, key)
		}
	}
	var overlay Overlay
	if raw := values["CODEFLUX_PROVIDER_ENDPOINT"]; raw != "" {
		overlay.ProviderEndpoint = &raw
	}
	minor, hasMinor := values["CODEFLUX_HARD_BUDGET_MINOR"]
	currency, hasCurrency := values["CODEFLUX_BUDGET_CURRENCY"]
	if hasMinor != hasCurrency {
		return Overlay{}, errors.New("budget minor units and currency must be supplied together")
	}
	if hasMinor {
		parsedMinor, err := strconv.ParseInt(minor, 10, 64)
		if err != nil {
			return Overlay{}, errors.New("hard budget minor units are invalid")
		}
		parsedCurrency, err := domain.ParseCurrencyCode(currency)
		if err != nil {
			return Overlay{}, errors.New("budget currency is invalid")
		}
		money, err := domain.NewMoney(parsedCurrency, parsedMinor)
		if err != nil {
			return Overlay{}, errors.New("hard budget is invalid")
		}
		overlay.HardBudget = &money
	}
	if raw := values["CODEFLUX_REQUEST_TIMEOUT"]; raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil || duration%time.Millisecond != 0 {
			return Overlay{}, errors.New("request timeout must be a whole number of milliseconds")
		}
		timeout := domain.Milliseconds(duration / time.Millisecond)
		overlay.RequestTimeout = &timeout
	}
	if raw := values["CODEFLUX_POLICY_PRESET"]; raw != "" {
		policy := domain.PolicyPreset(raw)
		overlay.PolicyPreset = &policy
	}
	return overlay, nil
}

// ExportJSON serializes only the complete non-secret settings schema.
func ExportJSON(settings Settings) ([]byte, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(settings)
}

// ImportJSON parses a bounded complete settings document, rejecting unknown
// fields so a secret-shaped or future security-sensitive key is never ignored.
func ImportJSON(data []byte) (Settings, error) {
	if len(data) > maximumImportBytes {
		return Settings{}, errors.New("configuration import exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var settings Settings
	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, fmt.Errorf("decode configuration: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Settings{}, err
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("configuration import contains trailing data")
		}
		return fmt.Errorf("decode trailing configuration: %w", err)
	}
	return nil
}
