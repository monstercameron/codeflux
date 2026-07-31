// Package i18n provides typed, Go-owned frontend localization contracts.
package i18n

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

// Locale is a canonical language tag supported by a message catalog.
type Locale string

const LocaleEnglishUnitedStates Locale = "en-US"

var ErrInvalidLocale = errors.New("frontend i18n: invalid locale")

// CanonicalLocale validates and normalizes the small BCP-47 subset required by
// the frontend. It accepts language, script, region, and variant-like subtags.
func CanonicalLocale(raw string) (Locale, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "_", "-"))
	if raw == "" {
		return "", ErrInvalidLocale
	}
	parts := strings.Split(raw, "-")
	if len(parts) > 8 {
		return "", fmt.Errorf("%w: too many subtags", ErrInvalidLocale)
	}
	for index, part := range parts {
		if len(part) < 2 || len(part) > 8 || !lettersOrDigits(part) {
			return "", fmt.Errorf("%w: %q", ErrInvalidLocale, raw)
		}
		switch {
		case index == 0:
			if !allLetters(part) {
				return "", fmt.Errorf("%w: language subtag %q", ErrInvalidLocale, part)
			}
			parts[index] = strings.ToLower(part)
		case len(part) == 2 && allLetters(part):
			parts[index] = strings.ToUpper(part)
		case len(part) == 4 && allLetters(part):
			parts[index] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		default:
			parts[index] = strings.ToLower(part)
		}
	}
	return Locale(strings.Join(parts, "-")), nil
}

func lettersOrDigits(value string) bool {
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') &&
			!(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func allLetters(value string) bool {
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') &&
			!(character >= 'A' && character <= 'Z') {
			return false
		}
	}
	return true
}

// Language returns the primary language subtag.
func (locale Locale) Language() string {
	language, _, _ := strings.Cut(string(locale), "-")
	return strings.ToLower(language)
}

type preference struct {
	locale Locale
	weight float64
	order  int
}

// ParseAcceptLanguage converts a bounded Accept-Language header into preference
// order. Invalid ranges and q=0 entries are ignored.
func ParseAcceptLanguage(header string) []string {
	if len(header) > 4096 {
		header = header[:4096]
	}
	parts := strings.Split(header, ",")
	if len(parts) > 32 {
		parts = parts[:32]
	}
	preferences := make([]preference, 0, len(parts))
	for index, part := range parts {
		fields := strings.Split(part, ";")
		tag := strings.TrimSpace(fields[0])
		if tag == "" || tag == "*" {
			continue
		}
		locale, err := CanonicalLocale(tag)
		if err != nil {
			continue
		}
		weight := 1.0
		for _, parameter := range fields[1:] {
			key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if !found || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 || parsed > 1 {
				weight = 0
			} else {
				weight = parsed
			}
		}
		if weight > 0 {
			preferences = append(preferences, preference{locale: locale, weight: weight, order: index})
		}
	}
	slices.SortStableFunc(preferences, func(left, right preference) int {
		if order := cmp.Compare(right.weight, left.weight); order != 0 {
			return order
		}
		return cmp.Compare(left.order, right.order)
	})
	result := make([]string, 0, len(preferences))
	for _, item := range preferences {
		result = append(result, string(item.locale))
	}
	return result
}

// DocumentLanguage is the contract the GWC document root applies to its html
// element after locale resolution.
type DocumentLanguage struct {
	LanguageTag string
	Direction   string
}

// HTMLProps returns typed GWC properties for the document root.
func (document DocumentLanguage) HTMLProps() html.Props {
	return html.Props{Lang: document.LanguageTag, Dir: document.Direction}
}
