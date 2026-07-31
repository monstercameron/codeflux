package i18n_test

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/i18n"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestEnglishCatalogCoversCurrentShellIDs(t *testing.T) {
	catalog := i18n.EnglishCatalog()
	ids := i18n.CurrentShellMessageIDs()
	if len(ids) < 120 {
		t.Fatalf("shell catalog unexpectedly small: %d", len(ids))
	}
	if len(ids) != catalog.Len() {
		t.Fatalf("shell ids = %d, catalog entries = %d", len(ids), catalog.Len())
	}
	seen := map[i18n.MessageID]bool{}
	for _, id := range ids {
		if id == "" || strings.TrimSpace(string(id)) != string(id) || strings.Contains(string(id), " ") {
			t.Errorf("unstable message id %q", id)
		}
		if seen[id] {
			t.Errorf("duplicate current shell id %q", id)
		}
		seen[id] = true
		if !catalog.Has(id) {
			t.Errorf("English catalog missing %q", id)
		}
	}
	for id, entry := range catalog.Entries() {
		if entry.Text == "" && (entry.One == "" || entry.Other == "") {
			t.Errorf("English entry %q has incomplete copy", id)
		}
	}
}

func TestEveryEnglishShellMessageFormatsWithRepresentativeArguments(t *testing.T) {
	translator := i18n.EnglishRegistry().Resolve("en-US")
	arguments := map[string]any{
		"branch": "main", "state": "Live", "subject": "messages",
		"component": "graph", "message": "not ready", "label": "Save",
	}
	for id, entry := range i18n.EnglishCatalog().Entries() {
		var err error
		if entry.Text != "" {
			_, err = translator.Text(id, arguments)
		} else {
			_, err = translator.Plural(id, 2, arguments)
		}
		if err != nil {
			t.Errorf("format %q: %v", id, err)
		}
	}
}

func TestLocaleCanonicalizationAndFallback(t *testing.T) {
	tests := map[string]i18n.Locale{
		"EN_us":      "en-US",
		"pt-br":      "pt-BR",
		"zh-hant-tw": "zh-Hant-TW",
	}
	for raw, want := range tests {
		got, err := i18n.CanonicalLocale(raw)
		if err != nil || got != want {
			t.Errorf("CanonicalLocale(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, invalid := range []string{"", "e", "123-US", "éé-FR", "en-@", "en-a-b-c-d-e-f-g-h-i"} {
		if _, err := i18n.CanonicalLocale(invalid); !errors.Is(err, i18n.ErrInvalidLocale) {
			t.Errorf("CanonicalLocale(%q) error = %v", invalid, err)
		}
	}

	registry := i18n.EnglishRegistry()
	sameLanguage := registry.Resolve("en-GB")
	if got := sameLanguage.Selection(); got.Resolved != i18n.LocaleEnglishUnitedStates || !got.UsedFallback {
		t.Fatalf("same-language selection = %+v", got)
	}
	unsupported := registry.Resolve("fr-CA")
	if got := unsupported.Selection(); got.Resolved != i18n.LocaleEnglishUnitedStates || !got.UsedFallback {
		t.Fatalf("unsupported selection = %+v", got)
	}
	defaulted := registry.Resolve()
	if got := defaulted.Selection(); got.Resolved != i18n.LocaleEnglishUnitedStates || got.UsedFallback {
		t.Fatalf("default selection = %+v", got)
	}
}

func TestAcceptLanguageUsesQualityAndBounds(t *testing.T) {
	got := i18n.ParseAcceptLanguage("fr-CA;q=0.5, en_GB;q=0.9, de;q=0, invalid@;q=1")
	want := []string{"en-GB", "fr-CA"}
	if !slicesEqual(got, want) {
		t.Fatalf("ParseAcceptLanguage = %v, want %v", got, want)
	}
}

func TestInterpolationPluralAndMissingArguments(t *testing.T) {
	translator := i18n.EnglishRegistry().Resolve("en-US")
	connection, err := translator.Text(i18n.MsgShellConnection, map[string]any{"state": "Live"})
	if err != nil || connection != "Connection: Live" {
		t.Fatalf("connection = %q, %v", connection, err)
	}
	if _, err := translator.Text(i18n.MsgShellConnection); !errors.Is(err, i18n.ErrMissingArgument) {
		t.Fatalf("missing argument error = %v", err)
	}
	if _, err := translator.Text("not.real"); !errors.Is(err, i18n.ErrUnknownMessage) {
		t.Fatalf("unknown message error = %v", err)
	}
	one, err := translator.Plural(i18n.MsgDataCount, 1, map[string]any{"subject": "message"})
	if err != nil || one != "1 message" {
		t.Fatalf("one = %q, %v", one, err)
	}
	many, err := translator.Plural(i18n.MsgDataCount, 0, map[string]any{
		"count": int64(999), "subject": "messages",
	})
	if err != nil || many != "0 messages" {
		t.Fatalf("many = %q, %v", many, err)
	}
	if _, err := translator.Plural(i18n.MsgActionRetry, 2); !errors.Is(err, i18n.ErrNotPlural) {
		t.Fatalf("non-plural error = %v", err)
	}
}

func TestGWCTextNodeEscapesInterpolatedValues(t *testing.T) {
	translator := i18n.EnglishRegistry().Resolve("en-US")
	node := translator.TextNode(i18n.MsgShellConnection, map[string]any{
		"state": `<script src="https://evil.example"></script>`,
	})
	markup, err := ui.RenderToString(html.P(html.Props{}, node))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "<script") {
		t.Fatalf("interpolated text escaped incorrectly: %s", markup)
	}
	if !strings.Contains(markup, "&lt;script") {
		t.Fatalf("escaped text missing: %s", markup)
	}
}

func TestSelectedCatalogFallsBackPerMessageToEnglish(t *testing.T) {
	partial, err := i18n.NewCatalog("es-ES", []i18n.Entry{
		{ID: i18n.MsgActionRetry, Text: "Reintentar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := i18n.NewRegistry(i18n.LocaleEnglishUnitedStates, i18n.EnglishCatalog(), partial)
	if err != nil {
		t.Fatal(err)
	}
	translator := registry.Resolve("es-ES")
	if got := translator.MustText(i18n.MsgActionRetry); got != "Reintentar" {
		t.Fatalf("selected message = %q", got)
	}
	if got := translator.MustText(i18n.MsgActionStop); got != "Stop" {
		t.Fatalf("fallback message = %q", got)
	}
}

func TestDocumentLanguageProducesTypedGWCProps(t *testing.T) {
	translator := i18n.EnglishRegistry().Resolve("fr")
	document := translator.DocumentLanguage()
	if document.LanguageTag != "en-US" || document.Direction != "ltr" {
		t.Fatalf("document language = %+v", document)
	}
	markup, err := ui.RenderToString(html.Tag("html", document.HTMLProps(), html.Text("CodeFlux")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `lang="en-US"`) || !strings.Contains(markup, `dir="ltr"`) {
		t.Fatalf("document attributes missing: %s", markup)
	}
}

func TestCatalogConstructionRejectsInvalidEntries(t *testing.T) {
	tests := [][]i18n.Entry{
		{{ID: "Unstable ID", Text: "bad"}},
		{{ID: "duplicate", Text: "one"}, {ID: "duplicate", Text: "two"}},
		{{ID: "mixed", Text: "text", One: "{count} one", Other: "{count} many"}},
		{{ID: "plural-mismatch", One: "{count} {subject}", Other: "{count}"}},
		{{ID: "plural-no-count", One: "one item", Other: "many items"}},
		{{ID: "broken", Text: "Hello {name"}},
	}
	for index, entries := range tests {
		if _, err := i18n.NewCatalog("en-US", entries); !errors.Is(err, i18n.ErrInvalidCatalog) {
			t.Errorf("case %d error = %v", index, err)
		}
	}
}

func TestDocumentDirectionSupportsFutureRTLCatalogs(t *testing.T) {
	arabic, err := i18n.NewCatalog("ar", []i18n.Entry{
		{ID: i18n.MsgActionRetry, Text: "إعادة المحاولة"},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := i18n.NewRegistry(i18n.LocaleEnglishUnitedStates, i18n.EnglishCatalog(), arabic)
	if err != nil {
		t.Fatal(err)
	}
	document := registry.Resolve("ar").DocumentLanguage()
	if document.LanguageTag != "ar" || document.Direction != "rtl" {
		t.Fatalf("RTL document language = %+v", document)
	}
}

func TestCatalogEntriesAreCopied(t *testing.T) {
	catalog := i18n.EnglishCatalog()
	first := catalog.Entries()
	delete(first, i18n.MsgProductName)
	second := catalog.Entries()
	if second[i18n.MsgProductName].Text != "CodeFlux" || !catalog.Has(i18n.MsgProductName) {
		t.Fatal("caller mutated immutable catalog")
	}
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
