package i18n

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// MessageID is a stable semantic identity, independent from English copy.
type MessageID string

type Entry struct {
	ID    MessageID
	Text  string
	One   string
	Other string
}

var (
	ErrUnknownMessage  = errors.New("frontend i18n: unknown message")
	ErrMissingArgument = errors.New("frontend i18n: missing message argument")
	ErrInvalidCatalog  = errors.New("frontend i18n: invalid catalog")
	ErrNotPlural       = errors.New("frontend i18n: message has no plural forms")
)

// Catalog is immutable after construction.
type Catalog struct {
	locale   Locale
	messages map[MessageID]Entry
}

func NewCatalog(locale Locale, entries []Entry) (Catalog, error) {
	canonicalLocale, err := CanonicalLocale(string(locale))
	if err != nil {
		return Catalog{}, err
	}
	locale = canonicalLocale
	messages := make(map[MessageID]Entry, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			return Catalog{}, fmt.Errorf("%w: empty message id", ErrInvalidCatalog)
		}
		if !validMessageID(entry.ID) {
			return Catalog{}, fmt.Errorf("%w: unstable message id %q", ErrInvalidCatalog, entry.ID)
		}
		if _, duplicate := messages[entry.ID]; duplicate {
			return Catalog{}, fmt.Errorf("%w: duplicate message id %q", ErrInvalidCatalog, entry.ID)
		}
		hasText := entry.Text != ""
		hasPlural := entry.One != "" && entry.Other != ""
		if hasText == hasPlural {
			return Catalog{}, fmt.Errorf("%w: %q must have text or complete plural forms", ErrInvalidCatalog, entry.ID)
		}
		if err := validatePlaceholders(entry); err != nil {
			return Catalog{}, err
		}
		messages[entry.ID] = entry
	}
	return Catalog{locale: locale, messages: messages}, nil
}

func validMessageID(id MessageID) bool {
	previousSeparator := true
	for _, character := range id {
		switch {
		case character >= 'a' && character <= 'z',
			character >= '0' && character <= '9':
			previousSeparator = false
		case character == '.', character == '-':
			if previousSeparator {
				return false
			}
			previousSeparator = true
		default:
			return false
		}
	}
	return !previousSeparator
}

func validatePlaceholders(entry Entry) error {
	forms := []string{entry.Text, entry.One, entry.Other}
	var reference map[string]struct{}
	for _, form := range forms {
		if form == "" {
			continue
		}
		names, err := placeholderNames(form)
		if err != nil {
			return fmt.Errorf("%w: %q: %v", ErrInvalidCatalog, entry.ID, err)
		}
		if reference == nil {
			reference = names
			continue
		}
		if !maps.Equal(reference, names) {
			return fmt.Errorf("%w: %q plural placeholders differ", ErrInvalidCatalog, entry.ID)
		}
	}
	if entry.One != "" {
		if _, found := reference["count"]; !found {
			return fmt.Errorf("%w: %q plural forms require {count}", ErrInvalidCatalog, entry.ID)
		}
	}
	return nil
}

func placeholderNames(template string) (map[string]struct{}, error) {
	names := map[string]struct{}{}
	for cursor := 0; cursor < len(template); {
		closeBeforeOpen := strings.IndexByte(template[cursor:], '}')
		open := strings.IndexByte(template[cursor:], '{')
		if open < 0 {
			if closeBeforeOpen >= 0 {
				return nil, errors.New("unexpected closing brace")
			}
			break
		}
		if closeBeforeOpen >= 0 && closeBeforeOpen < open {
			return nil, errors.New("unexpected closing brace")
		}
		open += cursor
		close := strings.IndexByte(template[open+1:], '}')
		if close < 0 {
			return nil, errors.New("unclosed placeholder")
		}
		close += open + 1
		name := template[open+1 : close]
		if name == "" || strings.ContainsAny(name, "{} \t\r\n") {
			return nil, fmt.Errorf("invalid placeholder %q", name)
		}
		names[name] = struct{}{}
		cursor = close + 1
	}
	return names, nil
}

func (catalog Catalog) Locale() Locale { return catalog.locale }
func (catalog Catalog) Len() int       { return len(catalog.messages) }

func (catalog Catalog) Has(id MessageID) bool {
	_, found := catalog.messages[id]
	return found
}

// Entries returns a copy suitable for completeness checks and tooling.
func (catalog Catalog) Entries() map[MessageID]Entry {
	return maps.Clone(catalog.messages)
}

type Selection struct {
	Requested    Locale
	Resolved     Locale
	UsedFallback bool
}

type Registry struct {
	defaultLocale Locale
	catalogs      map[Locale]Catalog
}

func NewRegistry(defaultLocale Locale, catalogs ...Catalog) (Registry, error) {
	canonicalDefault, err := CanonicalLocale(string(defaultLocale))
	if err != nil {
		return Registry{}, err
	}
	defaultLocale = canonicalDefault
	available := make(map[Locale]Catalog, len(catalogs))
	for _, catalog := range catalogs {
		if catalog.locale == "" {
			return Registry{}, fmt.Errorf("%w: catalog has no locale", ErrInvalidCatalog)
		}
		available[catalog.locale] = catalog
	}
	if _, found := available[defaultLocale]; !found {
		return Registry{}, fmt.Errorf("%w: default locale %q has no catalog", ErrInvalidCatalog, defaultLocale)
	}
	return Registry{defaultLocale: defaultLocale, catalogs: available}, nil
}

// Resolve selects an exact catalog, then a same-language catalog, then the
// registry default. Invalid requested tags are skipped.
func (registry Registry) Resolve(preferred ...string) Translator {
	var requested Locale
	for _, raw := range preferred {
		locale, err := CanonicalLocale(raw)
		if err != nil {
			continue
		}
		if requested == "" {
			requested = locale
		}
		if catalog, found := registry.catalogs[locale]; found {
			return newTranslator(catalog, registry.catalogs[registry.defaultLocale], Selection{
				Requested: locale, Resolved: locale,
			})
		}
		availableLocales := make([]Locale, 0, len(registry.catalogs))
		for available := range registry.catalogs {
			availableLocales = append(availableLocales, available)
		}
		slices.Sort(availableLocales)
		for _, available := range availableLocales {
			catalog := registry.catalogs[available]
			if available.Language() == locale.Language() {
				return newTranslator(catalog, registry.catalogs[registry.defaultLocale], Selection{
					Requested: locale, Resolved: available, UsedFallback: available != locale,
				})
			}
		}
	}
	fallback := registry.catalogs[registry.defaultLocale]
	return newTranslator(fallback, fallback, Selection{
		Requested: requested, Resolved: registry.defaultLocale,
		UsedFallback: requested != "" && requested != registry.defaultLocale,
	})
}

type Translator struct {
	selected  Catalog
	fallback  Catalog
	selection Selection
}

func newTranslator(selected, fallback Catalog, selection Selection) Translator {
	return Translator{selected: selected, fallback: fallback, selection: selection}
}

func (translator Translator) Selection() Selection { return translator.selection }

func (translator Translator) DocumentLanguage() DocumentLanguage {
	return DocumentLanguage{
		LanguageTag: string(translator.selection.Resolved),
		Direction:   directionFor(translator.selection.Resolved),
	}
}

func directionFor(locale Locale) string {
	switch locale.Language() {
	case "ar", "fa", "he", "ur":
		return "rtl"
	default:
		return "ltr"
	}
}

func (translator Translator) Text(id MessageID, arguments ...map[string]any) (string, error) {
	entry, err := translator.entry(id)
	if err != nil {
		return "", err
	}
	if entry.Text == "" {
		return "", fmt.Errorf("%w: %q", ErrNotPlural, id)
	}
	return interpolate(entry.Text, firstArguments(arguments))
}

func (translator Translator) MustText(id MessageID, arguments ...map[string]any) string {
	message, err := translator.Text(id, arguments...)
	if err != nil {
		return "[" + string(id) + "]"
	}
	return message
}

// TextNode returns an escaped GWC text node rather than HTML.
func (translator Translator) TextNode(id MessageID, arguments ...map[string]any) ui.Node {
	return ui.Text(translator.MustText(id, arguments...))
}

func (translator Translator) Plural(id MessageID, count int64, arguments ...map[string]any) (string, error) {
	entry, err := translator.entry(id)
	if err != nil {
		return "", err
	}
	if entry.One == "" || entry.Other == "" {
		return "", fmt.Errorf("%w: %q", ErrNotPlural, id)
	}
	template := entry.Other
	if count == 1 {
		template = entry.One
	}
	values := maps.Clone(firstArguments(arguments))
	if values == nil {
		values = map[string]any{}
	}
	values["count"] = count
	return interpolate(template, values)
}

func (translator Translator) MustPlural(id MessageID, count int64, arguments ...map[string]any) string {
	message, err := translator.Plural(id, count, arguments...)
	if err != nil {
		return "[" + string(id) + "]"
	}
	return message
}

func (translator Translator) entry(id MessageID) (Entry, error) {
	if entry, found := translator.selected.messages[id]; found {
		return entry, nil
	}
	if entry, found := translator.fallback.messages[id]; found {
		return entry, nil
	}
	return Entry{}, fmt.Errorf("%w: %q", ErrUnknownMessage, id)
}

func firstArguments(arguments []map[string]any) map[string]any {
	if len(arguments) == 0 {
		return nil
	}
	return arguments[0]
}

func interpolate(template string, arguments map[string]any) (string, error) {
	var output strings.Builder
	for cursor := 0; cursor < len(template); {
		open := strings.IndexByte(template[cursor:], '{')
		if open < 0 {
			output.WriteString(template[cursor:])
			break
		}
		open += cursor
		output.WriteString(template[cursor:open])
		close := strings.IndexByte(template[open+1:], '}')
		if close < 0 {
			return "", errors.New("frontend i18n: unclosed placeholder")
		}
		close += open + 1
		name := template[open+1 : close]
		value, found := arguments[name]
		if !found {
			return "", fmt.Errorf("%w: %q", ErrMissingArgument, name)
		}
		formatted, err := formatArgument(value)
		if err != nil {
			return "", fmt.Errorf("frontend i18n: argument %q: %w", name, err)
		}
		output.WriteString(formatted)
		cursor = close + 1
	}
	return output.String(), nil
}

func formatArgument(value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case fmt.Stringer:
		return value.String(), nil
	case int:
		return strconv.Itoa(value), nil
	case int8, int16, int32, int64:
		return strconv.FormatInt(reflect.ValueOf(value).Int(), 10), nil
	case uint, uint8, uint16, uint32, uint64:
		return strconv.FormatUint(reflect.ValueOf(value).Uint(), 10), nil
	case bool:
		return strconv.FormatBool(value), nil
	default:
		return "", fmt.Errorf("unsupported value type %T", value)
	}
}
