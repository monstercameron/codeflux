package atomname

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrInvalidNamingException classifies a malformed naming-exception record.
var ErrInvalidNamingException = errors.New("invalid atom naming exception")

// NamingExceptionKind is one of the exact four kinds AGENTS.md "Atom
// Documentation Style" declares for the
// "//codeflux:atom-name-exception <kind>: <reason>" directive.
type NamingExceptionKind string

const (
	NamingExceptionKindActionVerb       NamingExceptionKind = "action-verb"
	NamingExceptionKindAbbreviation     NamingExceptionKind = "abbreviation"
	NamingExceptionKindProviderSpecific NamingExceptionKind = "provider-specific"
	NamingExceptionKindGuaranteeClaim   NamingExceptionKind = "guarantee-claim"
)

// IsValid reports whether kind is one of the four declared exception kinds.
func (kind NamingExceptionKind) IsValid() bool {
	switch kind {
	case NamingExceptionKindActionVerb, NamingExceptionKindAbbreviation, NamingExceptionKindProviderSpecific, NamingExceptionKindGuaranteeClaim:
		return true
	default:
		return false
	}
}

// NamingException is one reviewed, narrow bypass of a single
// ValidateNamingGrammar rule, modeled after an already-isolated
// "//codeflux:atom-name-exception <kind>: <reason>" directive body. It is a
// review record, not a blanket bypass: it waives exactly one rule for one
// name, and Reason must be non-empty.
type NamingException struct {
	Kind   NamingExceptionKind
	Token  string
	Reason string
}

// Validate rejects a structurally invalid naming exception: an unknown
// kind, an empty reason, a missing abbreviation token, or a token supplied
// for a kind that does not take one.
func (exception NamingException) Validate() error {
	if !exception.Kind.IsValid() {
		return fmt.Errorf("%w: unknown exception kind %q", ErrInvalidNamingException, exception.Kind)
	}
	if strings.TrimSpace(exception.Reason) == "" {
		return fmt.Errorf("%w: exception reason must not be empty", ErrInvalidNamingException)
	}
	if exception.Kind == NamingExceptionKindAbbreviation {
		if !acronymPattern.MatchString(exception.Token) {
			return fmt.Errorf("%w: abbreviation exception token %q must be an uppercase acronym", ErrInvalidNamingException, exception.Token)
		}
	} else if exception.Token != "" {
		return fmt.Errorf("%w: exception kind %q does not take a token", ErrInvalidNamingException, exception.Kind)
	}
	return nil
}

// acronymPattern matches one all-uppercase (letters and digits, at least
// one letter, starting with a letter) abbreviation token.
var acronymPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// ErrMalformedNamingExceptionDirective classifies a directive body that does
// not match the required "<kind>[ <TOKEN>]: <reason>" shape.
var ErrMalformedNamingExceptionDirective = errors.New("malformed atom naming-exception directive")

// directivePattern parses the exact
// "codeflux:atom-name-exception <kind>[ <TOKEN>]: <reason>" body already
// isolated from its "//" comment prefix (AGENTS.md "Atom Documentation
// Style"). It does not locate the directive inside Go source: source
// location and comment-group extraction remain internal/atomdoc's AST
// responsibility (internal/atomdoc/schema.go explicitly tolerates and skips
// this directive during field extraction and defers validating its content
// to "the separate atom-naming milestone tasks" — this package is that
// milestone's validator).
var directivePattern = regexp.MustCompile(`^codeflux:atom-name-exception\s+([a-z-]+)(?:\s+([A-Z0-9]+))?\s*:\s*(.+)$`)

// ParseNamingExceptionDirective parses one already-isolated
// "codeflux:atom-name-exception ..." directive body into a validated
// NamingException.
func ParseNamingExceptionDirective(directiveBody string) (NamingException, error) {
	trimmed := strings.TrimSpace(directiveBody)
	matches := directivePattern.FindStringSubmatch(trimmed)
	if matches == nil {
		return NamingException{}, fmt.Errorf("%w: %q", ErrMalformedNamingExceptionDirective, directiveBody)
	}
	exception := NamingException{
		Kind:   NamingExceptionKind(matches[1]),
		Token:  matches[2],
		Reason: strings.TrimSpace(matches[3]),
	}
	if err := exception.Validate(); err != nil {
		return NamingException{}, err
	}
	return exception, nil
}

// ErrInvalidNamingLexicon classifies a rejected project lexicon extension.
var ErrInvalidNamingLexicon = errors.New("invalid atom naming lexicon extension")

// NamingLexicon is the immutable set of recognized action verbs, approved
// abbreviations, known provider names, and guarantee-claim words that
// ValidateNamingGrammar checks a canonical name against (M21-151). Values
// are constructed only through DefaultNamingLexicon and
// WithProjectAbbreviations, never mutated in place, so a NamingLexicon
// handed to one caller cannot be silently changed by another.
type NamingLexicon struct {
	abbreviations  map[string]string
	actionVerbs    map[string]struct{}
	providerNames  map[string]struct{}
	guaranteeWords map[string]struct{}
}

// DefaultNamingLexicon returns the built-in lexicon of established domain
// abbreviations, recognized action verbs, known provider names, and
// guarantee-claim words.
func DefaultNamingLexicon() NamingLexicon {
	return NamingLexicon{
		abbreviations:  cloneStringMap(defaultAbbreviations),
		actionVerbs:    cloneSet(defaultActionVerbs),
		providerNames:  cloneSet(defaultProviderNames),
		guaranteeWords: cloneSet(defaultGuaranteeClaimWords),
	}
}

// WithProjectAbbreviations returns a new NamingLexicon that also recognizes
// the given project-specific abbreviations, without modifying lexicon
// (M21-151's "project extension mechanism"). Each token must be an
// uppercase acronym and carry a non-empty expansion; invalid entries are
// rejected in sorted-key order so the reported error is deterministic
// regardless of Go map iteration order.
func (lexicon NamingLexicon) WithProjectAbbreviations(extra map[string]string) (NamingLexicon, error) {
	next := NamingLexicon{
		abbreviations:  cloneStringMap(lexicon.abbreviations),
		actionVerbs:    cloneSet(lexicon.actionVerbs),
		providerNames:  cloneSet(lexicon.providerNames),
		guaranteeWords: cloneSet(lexicon.guaranteeWords),
	}
	for _, token := range sortedStringKeys(extra) {
		expansion := extra[token]
		if !acronymPattern.MatchString(token) {
			return NamingLexicon{}, fmt.Errorf("%w: abbreviation token %q must be an uppercase acronym", ErrInvalidNamingLexicon, token)
		}
		if strings.TrimSpace(expansion) == "" {
			return NamingLexicon{}, fmt.Errorf("%w: abbreviation %q requires a non-empty expansion", ErrInvalidNamingLexicon, token)
		}
		next.abbreviations[token] = expansion
	}
	return next, nil
}

func (lexicon NamingLexicon) isRecognizedActionVerb(word string) bool {
	_, ok := lexicon.actionVerbs[word]
	return ok
}

func (lexicon NamingLexicon) isApprovedAbbreviation(word string) bool {
	_, ok := lexicon.abbreviations[word]
	return ok
}

func (lexicon NamingLexicon) isKnownProviderName(word string) bool {
	_, ok := lexicon.providerNames[word]
	return ok
}

func (lexicon NamingLexicon) isGuaranteeClaimWord(word string) bool {
	_, ok := lexicon.guaranteeWords[word]
	return ok
}

func cloneStringMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func cloneSet(source map[string]struct{}) map[string]struct{} {
	clone := make(map[string]struct{}, len(source))
	for key := range source {
		clone[key] = struct{}{}
	}
	return clone
}

func sortedStringKeys(source map[string]string) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// defaultAbbreviations lists established domain abbreviations clearer to
// intended readers than their expanded phrase (AGENTS.md "Atom Naming
// Style": "Use established domain abbreviations only when they are clearer
// to intended users than the expanded phrase.").
var defaultAbbreviations = map[string]string{
	"ID":    "identifier",
	"URL":   "uniform resource locator",
	"URI":   "uniform resource identifier",
	"HTTP":  "hypertext transfer protocol",
	"HTTPS": "hypertext transfer protocol secure",
	"API":   "application programming interface",
	"SQL":   "structured query language",
	"JSON":  "javascript object notation",
	"UUID":  "universally unique identifier",
	"JWT":   "json web token",
	"TLS":   "transport layer security",
	"SSH":   "secure shell",
	"CPU":   "central processing unit",
	"GPU":   "graphics processing unit",
	"IO":    "input/output",
	"OS":    "operating system",
	"DB":    "database",
	"UI":    "user interface",
	"UTC":   "coordinated universal time",
	"CSV":   "comma-separated values",
	"XML":   "extensible markup language",
	"IP":    "internet protocol",
	"DNS":   "domain name system",
	"TCP":   "transmission control protocol",
	"WASM":  "webassembly",
	"AST":   "abstract syntax tree",
	"LLM":   "large language model",
	"SCC":   "strongly connected component",
	"WS":    "websocket",
	"RPC":   "remote procedure call",
}

// defaultActionVerbs lists concrete action verbs recognized as satisfying
// the "begin with a concrete action verb for executable atoms" rule. It
// deliberately omits the generic verbs AGENTS.md's "Avoid names such as"
// list calls out (Process, Handle, Execute, Run, Do, Check) so a bare
// generic verb cannot be laundered through the recognized-verb allowlist.
var defaultActionVerbs = stringSet(
	"Derive", "Reserve", "Reconcile", "Load", "Validate", "Persist",
	"Create", "Update", "Delete", "Compute", "Classify", "Generate",
	"Invalidate", "Include", "Exclude", "Require", "Render", "Truncate",
	"Search", "Record", "Parse", "Normalize", "Scrub", "Hash", "Bind",
	"Admit", "Extract", "Flag", "Detect", "Compose", "Resolve", "Enforce",
	"Determine", "Apply", "Assign", "Build", "Convert", "Emit", "Ensure",
	"Establish", "Evaluate", "Fetch", "Filter", "Find", "Format", "Grant",
	"Import", "Export", "Issue", "Link", "Lock", "Map", "Merge", "Migrate",
	"Notify", "Open", "Publish", "Queue", "Read", "Rebuild", "Refresh",
	"Register", "Reject", "Release", "Remove", "Rename", "Replay", "Revoke",
	"Rollback", "Route", "Save", "Schedule", "Select", "Send", "Serialize",
	"Set", "Sign", "Sort", "Split", "Start", "Stop", "Store", "Submit",
	"Suspend", "Sync", "Track", "Transfer", "Translate", "Unlock", "Verify",
	"Write", "Cancel", "Capture", "Authorize", "Authenticate", "Redact",
)

// defaultProviderNames lists common external-provider names. AGENTS.md
// permits a provider name only when the atom's semantics are genuinely
// provider-specific rather than an adapter binding, so any occurrence
// requires a "provider-specific" naming exception.
var defaultProviderNames = stringSet(
	"Stripe", "PayPal", "GitHub", "GitLab", "OpenAI", "Anthropic",
	"AWS", "Azure", "Slack", "Twilio", "Discord", "Firebase",
	"Cloudflare", "Auth0", "Okta", "Datadog", "Sentry", "Google",
	"Microsoft",
)

// defaultGuaranteeClaimWords lists words that assert an assurance level the
// name alone cannot support (AGENTS.md: "Do not claim guarantees in the
// name that the contract and evidence do not support.").
var defaultGuaranteeClaimWords = stringSet(
	"Guaranteed", "Guarantees", "Always", "Never", "Certified",
	"Infallible", "Perfect", "Bulletproof", "Foolproof",
)

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
