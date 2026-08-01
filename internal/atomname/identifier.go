package atomname

import (
	"errors"
	"fmt"
	"go/token"
	"strings"
	"unicode"
)

// ErrInvalidCanonicalName classifies a rejected canonical atom identifier.
var ErrInvalidCanonicalName = errors.New("invalid atom canonical name")

// CanonicalName is one validated, exported Go identifier used as an atom's
// canonical name (M21-147). Construction enforces Go identifier syntax and
// the small set of unconditional AGENTS.md "Atom Naming Style" prohibitions
// (filler words, generic bare verbs); it deliberately imposes no short
// semantic length limit. Broader grammar policy that a reviewed naming
// exception can bypass — action-verb start, abbreviation allowlisting,
// provider names, and guarantee claims — is enforced separately by
// ValidateNamingGrammar, not by this constructor.
type CanonicalName struct{ value string }

// NewCanonicalName validates raw as a canonical atom identifier.
func NewCanonicalName(raw string) (CanonicalName, error) {
	if raw == "" {
		return CanonicalName{}, fmt.Errorf("%w: must not be empty", ErrInvalidCanonicalName)
	}
	if len(raw) > MaximumCanonicalNameBytes {
		return CanonicalName{}, fmt.Errorf("%w: %q exceeds the maximum bound of %d bytes", ErrInvalidCanonicalName, raw, MaximumCanonicalNameBytes)
	}
	if !token.IsIdentifier(raw) {
		return CanonicalName{}, fmt.Errorf("%w: %q is not a valid Go identifier", ErrInvalidCanonicalName, raw)
	}
	if !token.IsExported(raw) {
		return CanonicalName{}, fmt.Errorf("%w: %q must be exported (start with an uppercase letter)", ErrInvalidCanonicalName, raw)
	}
	if _, generic := genericBareNames[strings.ToLower(raw)]; generic {
		return CanonicalName{}, fmt.Errorf(
			"%w: %q is a generic bare name; AGENTS.md \"Atom Naming Style\" requires enough domain context to distinguish it from realistic alternatives",
			ErrInvalidCanonicalName, raw)
	}
	for _, word := range splitIdentifierWordsRaw(raw) {
		if _, filler := fillerWords[strings.ToLower(word)]; filler {
			return CanonicalName{}, fmt.Errorf(
				"%w: %q contains filler word %q, which AGENTS.md \"Atom Naming Style\" forbids",
				ErrInvalidCanonicalName, raw, word)
		}
	}
	return CanonicalName{value: raw}, nil
}

// String returns the canonical Go identifier.
func (name CanonicalName) String() string { return name.value }

// IsZero reports whether no canonical name has been assigned.
func (name CanonicalName) IsZero() bool { return name.value == "" }

// genericBareNames is the exact AGENTS.md "Atom Naming Style" "Avoid names
// such as" list, matched as a complete canonical name rather than as one
// component among several (that narrower, component-level prohibition is
// fillerWords below).
var genericBareNames = map[string]struct{}{
	"process":     {},
	"handle":      {},
	"execute":     {},
	"runtask":     {},
	"dopayment":   {},
	"checkdata":   {},
	"updatestate": {},
}

// fillerWords is the exact AGENTS.md "Atom Naming Style" filler-word list.
// Unlike the grammar-policy checks in ValidateNamingGrammar, there is no
// naming-exception kind that waives this prohibition, so it is enforced
// unconditionally at construction.
var fillerWords = map[string]struct{}{
	"helper":    {},
	"utility":   {},
	"manager":   {},
	"processor": {},
	"handler":   {},
	"thing":     {},
	"impl":      {},
}

// DisplayName is the human-readable name derived deterministically from a
// CanonicalName's semantic phrase (M21-148).
type DisplayName struct{ value string }

// String returns the display name.
func (name DisplayName) String() string { return name.value }

// IsZero reports whether no display name has been assigned.
func (name DisplayName) IsZero() bool { return name.value == "" }

// NormalizedPhrase is the deterministic, lowercase, word-split phrase
// derived from a CanonicalName (M21-149), used for canonical-name collision
// detection, alias comparison, and embedding input.
type NormalizedPhrase struct{ value string }

// String returns the normalized phrase.
func (phrase NormalizedPhrase) String() string { return phrase.value }

// IsZero reports whether no normalized phrase has been assigned.
func (phrase NormalizedPhrase) IsZero() bool { return phrase.value == "" }

// SplitIdentifierWords splits a canonical Go identifier into its
// constituent words, preserving multi-letter initialisms (such as HTTP, ID,
// or URL) as a single token rather than splitting each capital letter
// individually (M21-149, M21-150). The split is a pure function of the
// identifier's characters, so it is deterministic and independent of locale
// or map iteration.
func SplitIdentifierWords(name CanonicalName) []string {
	return splitIdentifierWordsRaw(name.String())
}

// identifierRunClass classifies one maximal run of same-kind characters
// inside a raw identifier, the unit splitIdentifierWordsRaw reasons about
// before deciding word boundaries.
type identifierRunClass int

const (
	identifierRunUpper identifierRunClass = iota
	identifierRunLower
	identifierRunDigit
)

// identifierRun is one maximal run of characters sharing an
// identifierRunClass, in original left-to-right order.
type identifierRun struct {
	class identifierRunClass
	text  string
}

// splitIdentifierWordsRaw implements the word-boundary rule (M21-149,
// M21-150). It first groups raw into maximal upper/lower/digit runs, then
// resolves each uppercase run's word boundaries against the built-in
// established-abbreviation allowlist (defaultAbbreviations; deliberately not
// the caller-extensible project lexicon, so DisplayName and
// NormalizedPhrase stay a pure, project-independent function of the
// canonical name):
//
//   - a single uppercase letter followed by a lowercase run is an ordinary
//     capitalized word ("Reserve", "Token");
//   - an uppercase run of two or more letters immediately followed by a
//     lowercase run is resolved by the longest known-abbreviation prefix
//     match: a proper-prefix match ("HTTP" out of "HTTPR...") splits off
//     the abbreviation and folds the remaining letter into the following
//     word ("Request"); a whole-run match ("IP" out of "IPv6...") splits
//     off the abbreviation and starts a new word at the lowercase run
//     ("v6"); an unmatched two-letter run ("OA" in "OAuth") is not split at
//     all — with no abbreviation evidence either way, splitting it apart
//     silently corrupts a real word, so it stays attached to its lowercase
//     continuation as one stylized word; an unmatched run of three or more
//     letters falls back to "the last uppercase letter starts the next
//     word" (M21-150's original heuristic, still correct when no
//     abbreviation boundary is known);
//   - an uppercase run NOT followed by a lowercase run (end of identifier,
//     or followed by a digit) is greedily decomposed left to right against
//     the abbreviation allowlist (longest match first), falling back to a
//     single-character token wherever no known abbreviation matches, so
//     that adjacent allowlisted initialisms such as "URL"+"ID" delimit
//     against each other instead of merging into one nonsense "URLID"
//     token;
//   - a digit run always attaches to the immediately preceding word
//     (an acronym-before-digit transition such as "OAuth"+"2" becomes
//     "OAuth2", not a lone floating digit token);
//   - a lone lowercase "s" run immediately following a word that is itself
//     an established abbreviation is an acronym-plural transition and
//     merges into that word ("ID"+"s" becomes "IDs"), rather than
//     splitting the plural marker off as its own token.
func splitIdentifierWordsRaw(raw string) []string {
	runs := groupIdentifierRuns(raw)
	if len(runs) == 0 {
		return nil
	}

	var words []string
	appendDigits := func(text string) {
		if len(words) > 0 {
			words[len(words)-1] += text
		} else {
			words = append(words, text)
		}
	}
	appendPluralS := func() bool {
		if len(words) == 0 {
			return false
		}
		last := words[len(words)-1]
		if !isEstablishedAbbreviation(last) {
			return false
		}
		words[len(words)-1] = last + "s"
		return true
	}

	for i := 0; i < len(runs); i++ {
		run := runs[i]
		switch run.class {
		case identifierRunDigit:
			appendDigits(run.text)
		case identifierRunLower:
			if run.text == "s" && appendPluralS() {
				continue
			}
			words = append(words, run.text)
		case identifierRunUpper:
			followingLower := ""
			hasFollowingLower := i+1 < len(runs) && runs[i+1].class == identifierRunLower
			if hasFollowingLower {
				followingLower = runs[i+1].text
			}

			switch {
			case len(run.text) == 1:
				if hasFollowingLower {
					words = append(words, run.text+followingLower)
					i++
				} else {
					words = append(words, run.text)
				}
			case !hasFollowingLower:
				words = append(words, decomposeUpperRun(run.text)...)
			default:
				switch match := longestAbbreviationPrefix(run.text); {
				case match == "":
					if len(run.text) == 2 {
						// No abbreviation evidence either way for a bare
						// two-letter run: splitting it apart (the original
						// M21-150 heuristic) silently corrupts real words
						// such as "OAuth" or "IPv6"-shaped identifiers with
						// no known abbreviation, so keep it attached to its
						// lowercase continuation as one word.
						words = append(words, run.text+followingLower)
					} else {
						// Three or more unmatched uppercase letters: fall
						// back to "the last uppercase letter starts the
						// next word."
						words = append(words, run.text[:len(run.text)-1])
						words = append(words, run.text[len(run.text)-1:]+followingLower)
					}
				case len(match) == len(run.text):
					// The whole run is itself one established
					// abbreviation; the lowercase run starts its own word,
					// unless it is exactly the plural marker "s", which
					// merges onto the abbreviation instead.
					words = append(words, match)
					if followingLower != "s" || !appendPluralS() {
						words = append(words, followingLower)
					}
				default:
					words = append(words, match)
					words = append(words, run.text[len(match):]+followingLower)
				}
				i++
			}
		}
	}
	return words
}

// groupIdentifierRuns splits raw into maximal runs of uppercase letters,
// lowercase-or-other letters, and digits, in original order.
func groupIdentifierRuns(raw string) []identifierRun {
	runes := []rune(raw)
	if len(runes) == 0 {
		return nil
	}
	var runs []identifierRun
	start := 0
	classOf := func(r rune) identifierRunClass {
		switch {
		case unicode.IsUpper(r):
			return identifierRunUpper
		case unicode.IsDigit(r):
			return identifierRunDigit
		default:
			return identifierRunLower
		}
	}
	current := classOf(runes[0])
	for i := 1; i < len(runes); i++ {
		class := classOf(runes[i])
		if class != current {
			runs = append(runs, identifierRun{class: current, text: string(runes[start:i])})
			start = i
			current = class
		}
	}
	runs = append(runs, identifierRun{class: current, text: string(runes[start:])})
	return runs
}

// splitterTokens is the vocabulary the word splitter uses to break a run of
// adjacent capitals into real words. It deliberately unions the established
// abbreviations with the known provider names: "AWS" is a genuine word unit
// inside AWSS3Bucket even though it is a provider name rather than a domain
// abbreviation, and without it the run "AWSS" matches nothing and decomposes
// into meaningless single letters.
//
// This affects SPLITTING only. Grammar validation
// (NamingLexicon.isApprovedAbbreviation / isKnownProviderName) still reads
// the two sets separately, so a provider name in a name still requires the
// provider-specific naming exception it always did.
var splitterTokens = func() map[string]struct{} {
	tokens := map[string]struct{}{}
	for token := range defaultAbbreviations {
		tokens[token] = struct{}{}
	}
	for token := range defaultProviderNames {
		tokens[strings.ToUpper(token)] = struct{}{}
	}
	return tokens
}()

// maxAbbreviationRunLength is the longest token the splitter can match,
// computed once so the longest-match scans below never probe past a length
// no token could satisfy.
var maxAbbreviationRunLength = func() int {
	max := 0
	for token := range splitterTokens {
		if len(token) > max {
			max = len(token)
		}
	}
	return max
}()

// isEstablishedAbbreviation reports whether word is a token the splitter
// treats as one indivisible word unit.
func isEstablishedAbbreviation(word string) bool {
	_, ok := splitterTokens[word]
	return ok
}

// longestAbbreviationPrefix returns the longest prefix of text (at least
// two letters) that exactly matches a built-in established abbreviation, or
// "" if no such prefix exists.
func longestAbbreviationPrefix(text string) string {
	limit := len(text)
	if limit > maxAbbreviationRunLength {
		limit = maxAbbreviationRunLength
	}
	for length := limit; length >= 2; length-- {
		candidate := text[:length]
		if isEstablishedAbbreviation(candidate) {
			return candidate
		}
	}
	return ""
}

// decomposeUpperRun greedily tokenizes an uppercase run that is not
// followed by a lowercase run (so there is no natural "last letter starts
// the next word" boundary to fall back on): it repeatedly consumes the
// longest known-abbreviation prefix of what remains, and falls back to a
// single-character token wherever no known abbreviation matches at the
// current position. This is what delimits adjacent allowlisted initialisms
// such as "URL"+"ID" instead of leaving them merged as one "URLID" token.
func decomposeUpperRun(text string) []string {
	var tokens []string
	for len(text) > 0 {
		if match := longestAbbreviationPrefix(text); match != "" {
			tokens = append(tokens, match)
			text = text[len(match):]
			continue
		}
		tokens = append(tokens, text[:1])
		text = text[1:]
	}
	return tokens
}

// DeriveDisplayName derives the human-readable display name by inserting
// word-boundary spaces into the canonical Go identifier, preserving its
// original casing (including initialisms) exactly (M21-148). Calling it
// twice on the same CanonicalName always produces the same DisplayName.
func DeriveDisplayName(name CanonicalName) DisplayName {
	return DisplayName{value: strings.Join(SplitIdentifierWords(name), " ")}
}

// DeriveNormalizedPhrase derives the deterministic, lowercase, space-joined
// word-split phrase from the canonical Go identifier (M21-149). Calling it
// twice on the same CanonicalName always produces the same NormalizedPhrase.
func DeriveNormalizedPhrase(name CanonicalName) NormalizedPhrase {
	words := SplitIdentifierWords(name)
	lowered := make([]string, len(words))
	for index, word := range words {
		lowered[index] = strings.ToLower(word)
	}
	return NormalizedPhrase{value: strings.Join(lowered, " ")}
}
