package preferences_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/preferences"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
)

func TestEncodeDecodeNormalizesAllowListedLayout(t *testing.T) {
	record, err := preferences.New("/settings", state.LayoutPreferences{
		RailCollapsed:  true,
		RailWidth:      900,
		GraphCollapsed: true,
		GraphWidth:     699,
		SplitPercent:   1,
		Viewport:       state.ViewportNarrow,
		ActivePane:     state.PaneGraph,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := preferences.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("graph_width")) || bytes.Contains(payload, []byte("699")) {
		t.Fatalf("payload persisted a non-contract field: %s", payload)
	}

	got, err := preferences.Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != preferences.CurrentVersion || got.LastRoute != "/settings" {
		t.Fatalf("decoded identity = %+v", got)
	}
	if !got.Layout.RailCollapsed || !got.Layout.GraphCollapsed {
		t.Fatalf("collapse preferences = %+v", got.Layout)
	}
	if got.Layout.RailWidth != 480 || got.Layout.SplitPercent != 35 {
		t.Fatalf("normalized dimensions = %+v", got.Layout)
	}
	if got.Layout.GraphWidth != state.DefaultLayoutPreferences().GraphWidth {
		t.Fatalf("graph width = %d, want default", got.Layout.GraphWidth)
	}
	if got.Layout.Viewport != state.ViewportNarrow || got.Layout.ActivePane != state.PaneGraph {
		t.Fatalf("responsive preferences = %+v", got.Layout)
	}
}

func TestDecodeRejectsSensitiveUnknownAndMalformedPayloads(t *testing.T) {
	valid := `{"version":1,"last_route":"/","layout":{"rail_collapsed":false,"rail_width":288,"graph_collapsed":false,"split_percent":58,"viewport":"wide","active_pane":"conversation"}}`
	tests := []struct {
		name    string
		payload string
		want    error
	}{
		{name: "sensitive top level", payload: `{"version":1,"api_key":"do-not-store"}`, want: preferences.ErrSensitiveField},
		{name: "sensitive nested", payload: `{"version":1,"layout":{"provider_credentials":"do-not-store"}}`, want: preferences.ErrSensitiveField},
		{name: "unknown top level", payload: strings.Replace(valid, `"version":1`, `"version":1,"theme":"dark"`, 1), want: preferences.ErrUnknownField},
		{name: "wrong field casing", payload: strings.Replace(valid, `"version":1`, `"Version":1`, 1), want: preferences.ErrUnknownField},
		{name: "unknown layout", payload: strings.Replace(valid, `"rail_width":288`, `"rail_width":288,"graph_width":500`, 1), want: preferences.ErrUnknownField},
		{name: "malformed", payload: `{"version":`, want: preferences.ErrMalformed},
		{name: "missing field", payload: strings.Replace(valid, `,"active_pane":"conversation"`, ``, 1), want: preferences.ErrMalformed},
		{name: "null scalar", payload: strings.Replace(valid, `"rail_collapsed":false`, `"rail_collapsed":null`, 1), want: preferences.ErrMalformed},
		{name: "duplicate", payload: strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1), want: preferences.ErrMalformed},
		{name: "trailing", payload: valid + `{}`, want: preferences.ErrMalformed},
		{name: "unsupported version", payload: strings.Replace(valid, `"version":1`, `"version":2`, 1), want: preferences.ErrUnsupportedVersion},
		{name: "unknown viewport", payload: strings.Replace(valid, `"viewport":"wide"`, `"viewport":"television"`, 1), want: preferences.ErrMalformed},
		{name: "unknown pane", payload: strings.Replace(valid, `"active_pane":"conversation"`, `"active_pane":"terminal"`, 1), want: preferences.ErrMalformed},
		{name: "noncanonical query", payload: strings.Replace(valid, `"last_route":"/"`, `"last_route":"/?tokenless=yes"`, 1), want: preferences.ErrMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := preferences.Decode([]byte(test.payload))
			if !errors.Is(err, test.want) {
				t.Fatalf("Decode error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestRestoreRequiresCurrentServerConfirmation(t *testing.T) {
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	path, err := routes.Path(routes.Route{
		Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := preferences.New(path, state.DefaultLayoutPreferences())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := preferences.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	base := routes.RestorationContext{
		Authenticated: true, Compatible: true, FirstRunComplete: true, CoordinatorAvailable: true,
		AccessibleRepositories: map[string]bool{repositoryID.String(): true},
		AccessibleThreads:      map[string]bool{threadID.String(): true},
	}

	restored, err := preferences.Restore(payload, base)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Route.Name != routes.ThreadWorkspace || restored.Record.LastRoute != path {
		t.Fatalf("restored = %+v", restored)
	}

	tests := []struct {
		name   string
		mutate func(routes.RestorationContext) routes.RestorationContext
		want   routes.RestoreReason
	}{
		{
			name: "missing repository",
			mutate: func(ctx routes.RestorationContext) routes.RestorationContext {
				ctx.AccessibleRepositories = map[string]bool{}
				return ctx
			},
			want: routes.RestoreRepositoryUnavailable,
		},
		{
			name: "archived thread",
			mutate: func(ctx routes.RestorationContext) routes.RestorationContext {
				ctx.ArchivedThreads = map[string]bool{threadID.String(): true}
				return ctx
			},
			want: routes.RestoreThreadArchived,
		},
		{
			name: "expired session",
			mutate: func(ctx routes.RestorationContext) routes.RestorationContext {
				ctx.Authenticated = false
				return ctx
			},
			want: routes.RestoreUnauthorized,
		},
		{
			name: "incompatible client",
			mutate: func(ctx routes.RestorationContext) routes.RestorationContext {
				ctx.Compatible = false
				return ctx
			},
			want: routes.RestoreIncompatible,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, restoreErr := preferences.Restore(payload, test.mutate(base))
			if !errors.Is(restoreErr, preferences.ErrRestorationRejected) {
				t.Fatalf("Restore error = %v", restoreErr)
			}
			var rejection *preferences.RestorationError
			if !errors.As(restoreErr, &rejection) || rejection.Reason != test.want {
				t.Fatalf("rejection = %+v, want %s", rejection, test.want)
			}
		})
	}
}

func TestStoreRoundTripClearAndCancellation(t *testing.T) {
	backend := &memoryBackend{values: make(map[string]string)}
	store, err := preferences.NewStore(backend)
	if err != nil {
		t.Fatal(err)
	}
	record, err := preferences.New("/diagnostics", state.DefaultLayoutPreferences())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.values[preferences.StorageKey]; !ok {
		t.Fatal("Save did not use the namespaced storage key")
	}
	got, err := store.Load(context.Background())
	if err != nil || got.LastRoute != record.LastRoute {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	if err := store.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, preferences.ErrNotFound) {
		t.Fatalf("Load after Clear error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Save(canceled, record); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Save error = %v", err)
	}
}

type memoryBackend struct {
	values map[string]string
}

func (b *memoryBackend) GetItem(key string) (string, bool, error) {
	value, ok := b.values[key]
	return value, ok, nil
}

func (b *memoryBackend) SetItem(key, value string) error {
	b.values[key] = value
	return nil
}

func (b *memoryBackend) RemoveItem(key string) error {
	delete(b.values, key)
	return nil
}
