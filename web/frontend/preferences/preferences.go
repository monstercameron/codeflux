// Package preferences persists the small, non-sensitive subset of browser UI
// state that may survive a compatible CodeFlux replay.
package preferences

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
)

const (
	// CurrentVersion is the only preference envelope version understood here.
	CurrentVersion = 1
	// StorageKey is deliberately namespaced and version-independent so an
	// unsupported envelope can be rejected instead of silently shadowed.
	StorageKey = "codeflux.ui.preferences"
	maxPayload = 8 << 10
)

var (
	ErrMalformed           = errors.New("frontend preferences: malformed payload")
	ErrUnknownField        = errors.New("frontend preferences: unknown field")
	ErrSensitiveField      = errors.New("frontend preferences: sensitive field")
	ErrUnsupportedVersion  = errors.New("frontend preferences: unsupported version")
	ErrRestorationRejected = errors.New("frontend preferences: restoration rejected")
	ErrNotFound            = errors.New("frontend preferences: not found")
	ErrStorageUnavailable  = errors.New("frontend preferences: browser storage unavailable")
)

// Record is the complete persistence contract. Layout is normalized on every
// encode and decode. GraphWidth intentionally is not part of the wire format.
type Record struct {
	Version   int
	LastRoute string
	Layout    state.LayoutPreferences
}

type wireRecord struct {
	Version   int        `json:"version"`
	LastRoute string     `json:"last_route"`
	Layout    wireLayout `json:"layout"`
}

type wireLayout struct {
	RailCollapsed  bool                `json:"rail_collapsed"`
	RailWidth      int                 `json:"rail_width"`
	GraphCollapsed bool                `json:"graph_collapsed"`
	SplitPercent   int                 `json:"split_percent"`
	Viewport       state.ViewportClass `json:"viewport"`
	ActivePane     state.Pane          `json:"active_pane"`
}

type decodedWireRecord struct {
	Version   *int               `json:"version"`
	LastRoute *string            `json:"last_route"`
	Layout    *decodedWireLayout `json:"layout"`
}

type decodedWireLayout struct {
	RailCollapsed  *bool                `json:"rail_collapsed"`
	RailWidth      *int                 `json:"rail_width"`
	GraphCollapsed *bool                `json:"graph_collapsed"`
	SplitPercent   *int                 `json:"split_percent"`
	Viewport       *state.ViewportClass `json:"viewport"`
	ActivePane     *state.Pane          `json:"active_pane"`
}

// New constructs a normalized record from a canonical route and current UI
// layout. It copies only explicitly allow-listed, presentation-only values.
func New(lastRoute string, layout state.LayoutPreferences) (Record, error) {
	record := Record{Version: CurrentVersion, LastRoute: lastRoute, Layout: layout}
	return normalizeRecord(record)
}

// Encode returns the stable, allow-listed preference envelope.
func Encode(record Record) ([]byte, error) {
	normalized, err := normalizeRecord(record)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(toWire(normalized))
	if err != nil {
		return nil, fmt.Errorf("encode frontend preferences: %w", err)
	}
	return payload, nil
}

// Decode strictly validates and normalizes a persisted preference envelope.
// Sensitive-shaped keys are rejected before ordinary unknown-field handling.
func Decode(payload []byte) (Record, error) {
	if len(bytes.TrimSpace(payload)) == 0 || len(payload) > maxPayload {
		return Record{}, ErrMalformed
	}
	if err := inspectJSON(payload); err != nil {
		return Record{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var wire decodedWireRecord
	if err := decoder.Decode(&wire); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return Record{}, fmt.Errorf("%w: %v", ErrUnknownField, err)
		}
		return Record{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if err := requireEOF(decoder); err != nil {
		return Record{}, err
	}
	if !wire.complete() {
		return Record{}, ErrMalformed
	}
	if *wire.Version != CurrentVersion {
		return Record{}, fmt.Errorf("%w: %d", ErrUnsupportedVersion, *wire.Version)
	}
	if !validViewport(*wire.Layout.Viewport) || !validPane(*wire.Layout.ActivePane) {
		return Record{}, ErrMalformed
	}
	return normalizeRecord(fromDecodedWire(wire))
}

func normalizeRecord(record Record) (Record, error) {
	if record.Version == 0 {
		record.Version = CurrentVersion
	}
	if record.Version != CurrentVersion {
		return Record{}, fmt.Errorf("%w: %d", ErrUnsupportedVersion, record.Version)
	}
	route, err := routes.Parse(record.LastRoute)
	if err != nil {
		return Record{}, ErrMalformed
	}
	canonical, err := routes.Path(route)
	if err != nil || canonical != record.LastRoute {
		return Record{}, ErrMalformed
	}
	if !validViewport(record.Layout.Viewport) || !validPane(record.Layout.ActivePane) {
		return Record{}, ErrMalformed
	}

	normalized := record.Layout.Normalize()
	// Graph width is not persisted. Re-establishing the default prevents an
	// omitted or caller-supplied value from becoming an accidental contract.
	normalized.GraphWidth = state.DefaultLayoutPreferences().GraphWidth
	record.LastRoute = canonical
	record.Layout = normalized
	return record, nil
}

func toWire(record Record) wireRecord {
	return wireRecord{
		Version:   record.Version,
		LastRoute: record.LastRoute,
		Layout: wireLayout{
			RailCollapsed:  record.Layout.RailCollapsed,
			RailWidth:      record.Layout.RailWidth,
			GraphCollapsed: record.Layout.GraphCollapsed,
			SplitPercent:   record.Layout.SplitPercent,
			Viewport:       record.Layout.Viewport,
			ActivePane:     record.Layout.ActivePane,
		},
	}
}

func fromDecodedWire(wire decodedWireRecord) Record {
	defaults := state.DefaultLayoutPreferences()
	defaults.RailCollapsed = *wire.Layout.RailCollapsed
	defaults.RailWidth = *wire.Layout.RailWidth
	defaults.GraphCollapsed = *wire.Layout.GraphCollapsed
	defaults.SplitPercent = *wire.Layout.SplitPercent
	defaults.Viewport = *wire.Layout.Viewport
	defaults.ActivePane = *wire.Layout.ActivePane
	return Record{Version: *wire.Version, LastRoute: *wire.LastRoute, Layout: defaults}
}

func (wire decodedWireRecord) complete() bool {
	if wire.Version == nil || wire.LastRoute == nil || wire.Layout == nil {
		return false
	}
	return wire.Layout.RailCollapsed != nil &&
		wire.Layout.RailWidth != nil &&
		wire.Layout.GraphCollapsed != nil &&
		wire.Layout.SplitPercent != nil &&
		wire.Layout.Viewport != nil &&
		wire.Layout.ActivePane != nil
}

func validViewport(viewport state.ViewportClass) bool {
	switch viewport {
	case state.ViewportWide, state.ViewportMedium, state.ViewportNarrow, state.ViewportMinimum:
		return true
	default:
		return false
	}
}

func validPane(pane state.Pane) bool {
	switch pane {
	case state.PaneConversation, state.PaneGraph, state.PaneThreads:
		return true
	default:
		return false
	}
}

func inspectJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := inspectValue(decoder, 0, map[string]bool{
		"version": true, "last_route": true, "layout": true,
	}); err != nil {
		return err
	}
	return requireEOF(decoder)
}

func inspectValue(decoder *json.Decoder, depth int, allowedFields map[string]bool) error {
	if depth > 16 {
		return ErrMalformed
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("%w: %v", ErrMalformed, keyErr)
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrMalformed
			}
			if isSensitiveKey(key) {
				return fmt.Errorf("%w: %q", ErrSensitiveField, key)
			}
			if allowedFields != nil && !allowedFields[key] {
				return fmt.Errorf("%w: %q", ErrUnknownField, key)
			}
			if _, duplicate := seen[key]; duplicate {
				return ErrMalformed
			}
			seen[key] = struct{}{}
			var childFields map[string]bool
			if depth == 0 && key == "layout" {
				childFields = map[string]bool{
					"rail_collapsed": true, "rail_width": true,
					"graph_collapsed": true, "split_percent": true,
					"viewport": true, "active_pane": true,
				}
			}
			if err := inspectValue(decoder, depth+1, childFields); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim('}') {
			return ErrMalformed
		}
	case '[':
		for decoder.More() {
			if err := inspectValue(decoder, depth+1, nil); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim(']') {
			return ErrMalformed
		}
	default:
		return ErrMalformed
	}
	return nil
}

func isSensitiveKey(key string) bool {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
	for _, marker := range []string{
		"secret", "password", "credential", "authorization", "cookie",
		"apikey", "accesstoken", "refreshtoken", "sessiontoken", "providertoken",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return normalized == "token" || strings.HasSuffix(normalized, "token")
}

func requireEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrMalformed
		}
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return nil
}
