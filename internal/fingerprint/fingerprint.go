package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"codeflux.dev/codeflux/internal/domain"
)

// SchemaVersion is fingerprint schema version 1 (M21-051 BLOCKER). Every
// ExactFingerprint stores it explicitly in SchemaVersion. Validate and Hash
// refuse any value other than the current SchemaVersion instead of
// reinterpreting an unfamiliar layout under new field semantics: a schema
// change must be detectable, never silent. Advancing the schema means adding
// a new constant, a new field/struct shape, and explicit migration handling
// in the caller (the storage lane's M21-023 table), not editing this
// constant's meaning in place.
const SchemaVersion uint32 = 1

// Explicit bounds on every unbounded-looking exact field. None of these are
// arbitrary: they cap the canonical payload so hashing, storage, and replay
// stay bounded regardless of caller input.
const (
	MaximumAffectedPathHints    = 200
	MaximumAffectedPackageHints = 200
	MaximumAffectedSymbolHints  = 200
	MaximumHintLength           = 512

	MaximumToolchainBindings   = 100
	MaximumBindingFieldLength  = 255
	MaximumDescriptiveSummary  = 8192
	MaximumDescriptiveKeywords = 64
	MaximumDescriptiveKeyword  = 128
)

// ErrInvalidFingerprint classifies a malformed fingerprint field.
var ErrInvalidFingerprint = errors.New("invalid task fingerprint")

// ErrUnsupportedFingerprintSchemaVersion classifies an ExactFingerprint
// stamped with a fingerprint_schema_version other than the current
// SchemaVersion. Validate and Hash return this instead of hashing the value
// under the current field semantics, so a future schema change can never be
// silently reinterpreted as schema version 1.
var ErrUnsupportedFingerprintSchemaVersion = errors.New("unsupported fingerprint schema version")

// FieldError identifies one invalid fingerprint field without exposing
// caller-supplied content in the error's identity.
type FieldError struct {
	Field  string
	Reason string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s is invalid: %s", e.Field, e.Reason)
}

// Unwrap allows errors.Is(err, ErrInvalidFingerprint).
func (e *FieldError) Unwrap() error {
	return ErrInvalidFingerprint
}

func fieldError(field, reason string) error {
	return &FieldError{Field: field, Reason: reason}
}

// TaskClass is the fingerprint's own normalized task-shape classification
// (M21-054). It intentionally does not reuse internal/forecast.TaskClass:
// that type's validity check is unexported (forecast-internal), and forecast
// classifies effort, not retrieval identity. The two vocabularies currently
// share the same words by design; a later change may hoist a single shared
// enum into internal/domain if both lanes want strict identity, but nothing
// here depends on that happening.
type TaskClass string

const (
	TaskClassDocumentation TaskClass = "documentation"
	TaskClassSmallChange   TaskClass = "small-change"
	TaskClassBugFix        TaskClass = "bug-fix"
	TaskClassFeature       TaskClass = "feature"
	TaskClassRefactor      TaskClass = "refactor"
	TaskClassMigration     TaskClass = "migration"
	TaskClassSecurity      TaskClass = "security"
)

var allTaskClasses = []TaskClass{
	TaskClassDocumentation,
	TaskClassSmallChange,
	TaskClassBugFix,
	TaskClassFeature,
	TaskClassRefactor,
	TaskClassMigration,
	TaskClassSecurity,
}

// AllTaskClasses returns the complete declared task-class set.
func AllTaskClasses() []TaskClass {
	return append([]TaskClass(nil), allTaskClasses...)
}

// IsValid reports whether the task class is declared.
func (value TaskClass) IsValid() bool {
	for _, class := range allTaskClasses {
		if class == value {
			return true
		}
	}
	return false
}

// AuthorityClass is the requested effect/authority class of a task
// (M21-058): the non-inferable authority the work is expected to need. It
// deliberately mirrors internal/executor.AuthorityClass's wire vocabulary
// (same string values) without importing that package, because
// internal/executor pulls in OS-specific process management and redaction
// machinery that a near-pure computation package must not depend on. The two
// enums interoperate at the string level; a caller that already has an
// executor.AuthorityClass can convert with a simple string cast.
type AuthorityClass string

const (
	AuthorityClassAutomaticRead         AuthorityClass = "automatic-read"
	AuthorityClassTaskWrite             AuthorityClass = "task-write"
	AuthorityClassNetwork               AuthorityClass = "network"
	AuthorityClassDependencyInstall     AuthorityClass = "dependency-install"
	AuthorityClassExternalWrite         AuthorityClass = "external-write"
	AuthorityClassCredential            AuthorityClass = "credential"
	AuthorityClassDestructive           AuthorityClass = "destructive"
	AuthorityClassPrivileged            AuthorityClass = "privileged"
	AuthorityClassExternalCommunication AuthorityClass = "external-communication"
)

var allAuthorityClasses = []AuthorityClass{
	AuthorityClassAutomaticRead,
	AuthorityClassTaskWrite,
	AuthorityClassNetwork,
	AuthorityClassDependencyInstall,
	AuthorityClassExternalWrite,
	AuthorityClassCredential,
	AuthorityClassDestructive,
	AuthorityClassPrivileged,
	AuthorityClassExternalCommunication,
}

// AllAuthorityClasses returns the complete declared authority-class set.
func AllAuthorityClasses() []AuthorityClass {
	return append([]AuthorityClass(nil), allAuthorityClasses...)
}

// IsValid reports whether the authority class is declared.
func (value AuthorityClass) IsValid() bool {
	for _, class := range allAuthorityClasses {
		if class == value {
			return true
		}
	}
	return false
}

// ToolchainBinding names one language, toolchain, or dependency version that
// the fingerprint is bound to (M21-056). Two bindings differing only in list
// order are the same fingerprint; two bindings naming the same Name with
// different Version values are a real conflict and are rejected rather than
// silently resolved.
type ToolchainBinding struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ExactFingerprintInput is the caller-supplied, order-independent, possibly
// duplicated raw material for BuildExactFingerprint. Every list is
// normalized (trimmed, deduplicated, sorted) before it can influence the
// canonical fingerprint, so callers never need to pre-sort input to get a
// stable hash.
type ExactFingerprintInput struct {
	Project            domain.ProjectID
	Repository         domain.RepositoryID
	BaseRevision       domain.RevisionBinding
	TaskClass          TaskClass
	AffectedPaths      []string
	AffectedPackages   []string
	AffectedSymbols    []string
	Bindings           []ToolchainBinding
	Risk               domain.RiskLevel
	RequiredAssurance  domain.AssuranceLevel
	RequestedAuthority []AuthorityClass
}

// ExactFingerprint is every field that participates in retrieval-gate
// eligibility and the canonical hash (M21-052..058). It structurally
// excludes descriptive prose: there is no field here capable of carrying
// free-form text, so a caller cannot accidentally hash descriptive retrieval
// material by constructing or extending this type (M21-059). Build one with
// BuildExactFingerprint rather than by hand so every list is normalized.
type ExactFingerprint struct {
	SchemaVersion      uint32                 `json:"fingerprint_schema_version"`
	Project            domain.ProjectID       `json:"project"`
	Repository         domain.RepositoryID    `json:"repository"`
	BaseRevision       domain.RevisionBinding `json:"base_revision"`
	TaskClass          TaskClass              `json:"task_class"`
	AffectedPaths      []string               `json:"affected_paths"`
	AffectedPackages   []string               `json:"affected_packages"`
	AffectedSymbols    []string               `json:"affected_symbols"`
	Bindings           []ToolchainBinding     `json:"bindings"`
	Risk               domain.RiskLevel       `json:"risk"`
	RequiredAssurance  domain.AssuranceLevel  `json:"required_assurance"`
	RequestedAuthority []AuthorityClass       `json:"requested_authority"`
}

// BuildExactFingerprint normalizes input and returns a validated
// ExactFingerprint stamped with the current SchemaVersion. Input list order
// and incidental duplication never affect the result: every list is
// deduplicated and sorted before it becomes part of the value that gets
// hashed.
func BuildExactFingerprint(input ExactFingerprintInput) (ExactFingerprint, error) {
	if input.Project.IsZero() {
		return ExactFingerprint{}, fieldError("fingerprint.project", "must not be empty")
	}
	if input.Repository.IsZero() {
		return ExactFingerprint{}, fieldError("fingerprint.repository", "must not be empty")
	}
	if err := input.BaseRevision.Validate(); err != nil {
		return ExactFingerprint{}, err
	}
	if !input.TaskClass.IsValid() {
		return ExactFingerprint{}, fieldError("fingerprint.task_class", "is not declared")
	}
	if !input.Risk.IsValid() {
		return ExactFingerprint{}, fieldError("fingerprint.risk", "is not declared")
	}
	if !input.RequiredAssurance.IsValid() || input.RequiredAssurance == domain.AssuranceLevelInvalidated {
		return ExactFingerprint{}, fieldError("fingerprint.required_assurance", "must be a valid non-invalidated level")
	}

	paths, err := normalizeIdentifierList("fingerprint.affected_paths", input.AffectedPaths, MaximumAffectedPathHints, MaximumHintLength, shapeRepositoryPath)
	if err != nil {
		return ExactFingerprint{}, err
	}
	packages, err := normalizeIdentifierList("fingerprint.affected_packages", input.AffectedPackages, MaximumAffectedPackageHints, MaximumHintLength, shapeImportPath)
	if err != nil {
		return ExactFingerprint{}, err
	}
	symbols, err := normalizeIdentifierList("fingerprint.affected_symbols", input.AffectedSymbols, MaximumAffectedSymbolHints, MaximumHintLength, shapeSymbolPath)
	if err != nil {
		return ExactFingerprint{}, err
	}
	bindings, err := normalizeToolchainBindings(input.Bindings)
	if err != nil {
		return ExactFingerprint{}, err
	}
	authority, err := normalizeAuthorityClasses(input.RequestedAuthority)
	if err != nil {
		return ExactFingerprint{}, err
	}

	value := ExactFingerprint{
		SchemaVersion:      SchemaVersion,
		Project:            input.Project,
		Repository:         input.Repository,
		BaseRevision:       input.BaseRevision,
		TaskClass:          input.TaskClass,
		AffectedPaths:      paths,
		AffectedPackages:   packages,
		AffectedSymbols:    symbols,
		Bindings:           bindings,
		Risk:               input.Risk,
		RequiredAssurance:  input.RequiredAssurance,
		RequestedAuthority: authority,
	}
	if err := value.Validate(); err != nil {
		return ExactFingerprint{}, err
	}
	return value, nil
}

// Validate checks that value is a well-formed, current-schema exact
// fingerprint: every list already normalized (trimmed, deduplicated,
// sorted), every enum declared, and every identity field present. It is safe
// to call on a value read back from storage, not only one just built by
// BuildExactFingerprint.
func (value ExactFingerprint) Validate() error {
	if value.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedFingerprintSchemaVersion, value.SchemaVersion, SchemaVersion)
	}
	if value.Project.IsZero() {
		return fieldError("fingerprint.project", "must not be empty")
	}
	if value.Repository.IsZero() {
		return fieldError("fingerprint.repository", "must not be empty")
	}
	if err := value.BaseRevision.Validate(); err != nil {
		return err
	}
	if !value.TaskClass.IsValid() {
		return fieldError("fingerprint.task_class", "is not declared")
	}
	if !value.Risk.IsValid() {
		return fieldError("fingerprint.risk", "is not declared")
	}
	if !value.RequiredAssurance.IsValid() || value.RequiredAssurance == domain.AssuranceLevelInvalidated {
		return fieldError("fingerprint.required_assurance", "must be a valid non-invalidated level")
	}
	if !normalizedStrings(value.AffectedPaths, MaximumAffectedPathHints, MaximumHintLength, shapeRepositoryPath) {
		return fieldError("fingerprint.affected_paths", "must be non-nil, trimmed, deduplicated, sorted, bounded, NFC-normalized, forward-slash-separated repository-relative paths")
	}
	if !normalizedStrings(value.AffectedPackages, MaximumAffectedPackageHints, MaximumHintLength, shapeImportPath) {
		return fieldError("fingerprint.affected_packages", "must be non-nil, trimmed, deduplicated, sorted, bounded, NFC-normalized Go import paths")
	}
	if !normalizedStrings(value.AffectedSymbols, MaximumAffectedSymbolHints, MaximumHintLength, shapeSymbolPath) {
		return fieldError("fingerprint.affected_symbols", "must be non-nil, trimmed, deduplicated, sorted, bounded, NFC-normalized, dot-qualified Go identifiers")
	}
	if !normalizedBindings(value.Bindings) {
		return fieldError("fingerprint.bindings", "must be well-formed, uniquely named, sorted, and bounded")
	}
	if !normalizedAuthority(value.RequestedAuthority) {
		return fieldError("fingerprint.requested_authority", "must be declared, deduplicated, sorted, and bounded")
	}
	return nil
}

// CanonicalJSON returns the deterministic, order-independent, hazard-free
// serialization of value (M21-060): fixed struct field order (declared by
// this Go type, not runtime map iteration), no maps in the encoded shape, no
// floats, no locale-dependent formatting, and no time zone, because none of
// those exist anywhere in ExactFingerprint. Every list field was already
// sorted by Validate before this is called, so two ExactFingerprint values
// built from the same logical input always encode to the same bytes
// regardless of the order the caller originally supplied.
func (value ExactFingerprint) CanonicalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: encode exact fingerprint: %v", ErrInvalidFingerprint, err)
	}
	return encoded, nil
}

// Hash returns the lowercase hex SHA-256 of value's CanonicalJSON (M21-061).
// This is the retrieval key: identical exact fields, in any input order,
// always produce identical output, and any material field change always
// changes it.
func (value ExactFingerprint) Hash() (string, error) {
	encoded, err := value.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// DescriptiveRetrievalText is free-form retrieval-only material: natural
// language that helps a later embedding or human-review step find a
// fingerprint, never structured identity. It has no Hash method, no
// CanonicalJSON method, and no field of any kind on ExactFingerprint or
// Fingerprint reads from it; the only way descriptive text could influence a
// hash would be to add code that passes DescriptiveRetrievalText into
// ExactFingerprint.Hash, which does not type-check because that method has
// no parameter to receive it (M21-059).
type DescriptiveRetrievalText struct {
	Summary  string   `json:"summary"`
	Keywords []string `json:"keywords"`
}

// Validate checks only size bounds. Whitespace, wording, ordering, and
// duplication in descriptive text are deliberately unconstrained: none of it
// can affect identity, so there is nothing to normalize for hashing
// purposes.
func (value DescriptiveRetrievalText) Validate() error {
	if len(value.Summary) > MaximumDescriptiveSummary {
		return fieldError("fingerprint.descriptive.summary", "exceeds the maximum length")
	}
	if len(value.Keywords) > MaximumDescriptiveKeywords {
		return fieldError("fingerprint.descriptive.keywords", "exceeds the maximum count")
	}
	for _, keyword := range value.Keywords {
		if len(keyword) > MaximumDescriptiveKeyword {
			return fieldError("fingerprint.descriptive.keywords", "keyword exceeds the maximum length")
		}
	}
	return nil
}

// Fingerprint pairs the hash-and-eligibility-bearing ExactFingerprint with
// retrieval-only DescriptiveRetrievalText. Hash and CanonicalJSON are
// defined only on ExactFingerprint, never on Fingerprint or
// DescriptiveRetrievalText, so Fingerprint.Hash below is the only way to
// hash a Fingerprint value and it can only ever read Exact.
type Fingerprint struct {
	Exact       ExactFingerprint
	Descriptive DescriptiveRetrievalText
}

// NewFingerprint validates and pairs an exact fingerprint with its
// descriptive retrieval text.
func NewFingerprint(exact ExactFingerprint, descriptive DescriptiveRetrievalText) (Fingerprint, error) {
	if err := exact.Validate(); err != nil {
		return Fingerprint{}, err
	}
	if err := descriptive.Validate(); err != nil {
		return Fingerprint{}, err
	}
	return Fingerprint{Exact: exact, Descriptive: descriptive}, nil
}

// Hash returns value.Exact.Hash(). Descriptive is never read: this is the
// structural guarantee that retrieval-only prose can never change a
// fingerprint's identity (M21-059).
func (value Fingerprint) Hash() (string, error) {
	return value.Exact.Hash()
}

// exactFieldShape names the documented syntax an exact-identity string field
// must satisfy (M21-055 / adversarial review Defect 3). Accepting any
// trimmed non-empty string let up to ~100KB of arbitrary prose enter fields
// advertised as structured exact identity and directly determine the hash,
// while the genuinely free-text DescriptiveRetrievalText is provably inert.
// Each shape rejects content that cannot be the kind of value the field
// documents, rather than attempting a full parser.
type exactFieldShape int

const (
	// shapeRepositoryPath is a repository-relative file path: forward-slash
	// separated, non-empty segments, no leading slash, no "." or ".."
	// segment (both a syntax constraint and a path-traversal guard).
	shapeRepositoryPath exactFieldShape = iota
	// shapeImportPath is a Go import path: forward-slash separated,
	// non-empty segments drawn from the conventional import-path character
	// set (letters, digits, ".", "_", "-").
	shapeImportPath
	// shapeSymbolPath is a dot-qualified Go identifier (a bare identifier,
	// or "Type.Method"/"pkg.Type.Method"-shaped qualification): every
	// dot-separated component must be a valid Go identifier per go/token.
	shapeSymbolPath
)

// normalizeExactString applies the canonicalization every exact string field
// receives before it can participate in the hash (adversarial review
// Defect 2): Unicode NFC normalization, so two differently-composed
// spellings of the same identifier are the same value, and — for
// repository-relative paths only — separator normalization to forward
// slash, so a backslash-spelled Windows path and a forward-slash-spelled
// path are the same value. golang.org/x/text/unicode/norm was already an
// indirect dependency of this module (pulled in transitively by
// GoWebComponents/x/net) before this change, so this does not add a new
// dependency.
func normalizeExactString(value string, shape exactFieldShape) string {
	if shape == shapeRepositoryPath {
		value = strings.ReplaceAll(value, "\\", "/")
	}
	return norm.NFC.String(value)
}

var (
	pathSegmentPattern       = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._+-]*[A-Za-z0-9])?$`)
	importPathSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)
)

// shapeIsValid reports whether the already-normalized value has the syntax
// its exactFieldShape documents.
func shapeIsValid(value string, shape exactFieldShape) bool {
	switch shape {
	case shapeRepositoryPath:
		return isRepositoryRelativePath(value)
	case shapeImportPath:
		return isGoImportPath(value)
	case shapeSymbolPath:
		return isGoSymbolPath(value)
	default:
		return false
	}
}

// isRepositoryRelativePath rejects prose, absolute paths, and traversal
// segments while accepting ordinary forward-slash-separated repository
// paths.
func isRepositoryRelativePath(value string) bool {
	if strings.HasPrefix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		if !pathSegmentPattern.MatchString(segment) {
			return false
		}
	}
	return true
}

// isGoImportPath rejects prose and accepts the conventional Go import-path
// character set.
func isGoImportPath(value string) bool {
	if strings.HasPrefix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" {
			return false
		}
		if !importPathSegmentPattern.MatchString(segment) {
			return false
		}
	}
	return true
}

// isGoSymbolPath accepts a bare Go identifier or a dot-qualified sequence of
// Go identifiers (for example "Type.Method" or "pkg.Type.Method"), reusing
// the standard library's own identifier grammar rather than reimplementing
// Unicode letter/digit classification.
func isGoSymbolPath(value string) bool {
	for _, segment := range strings.Split(value, ".") {
		if !token.IsIdentifier(segment) {
			return false
		}
	}
	return true
}

func normalizeIdentifierList(field string, values []string, maximum, maxLength int, shape exactFieldShape) ([]string, error) {
	if len(values) > maximum {
		return nil, fieldError(field, fmt.Sprintf("must not exceed %d entries", maximum))
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, raw := range values {
		value := normalizeExactString(raw, shape)
		if strings.TrimSpace(value) != value || value == "" || len(value) > maxLength {
			return nil, fieldError(field, "entries must be non-empty, trimmed, and bounded")
		}
		if !shapeIsValid(value, shape) {
			return nil, fieldError(field, "entries must match the field's documented syntax")
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func normalizedStrings(values []string, maximum, maxLength int, shape exactFieldShape) bool {
	if values == nil {
		return false
	}
	if len(values) > maximum {
		return false
	}
	for index, value := range values {
		if strings.TrimSpace(value) != value || value == "" || len(value) > maxLength {
			return false
		}
		if shape == shapeRepositoryPath && strings.Contains(value, "\\") {
			return false
		}
		if norm.NFC.String(value) != value {
			return false
		}
		if !shapeIsValid(value, shape) {
			return false
		}
		if index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func normalizeToolchainBindings(values []ToolchainBinding) ([]ToolchainBinding, error) {
	if len(values) > MaximumToolchainBindings {
		return nil, fieldError("fingerprint.bindings", fmt.Sprintf("must not exceed %d entries", MaximumToolchainBindings))
	}
	seenVersion := make(map[string]string, len(values))
	normalized := make([]ToolchainBinding, 0, len(values))
	for _, raw := range values {
		binding := ToolchainBinding{
			Name:    norm.NFC.String(raw.Name),
			Version: norm.NFC.String(raw.Version),
		}
		if strings.TrimSpace(binding.Name) != binding.Name || binding.Name == "" || len(binding.Name) > MaximumBindingFieldLength {
			return nil, fieldError("fingerprint.bindings.name", "must be non-empty, trimmed, and bounded")
		}
		if strings.TrimSpace(binding.Version) != binding.Version || binding.Version == "" || len(binding.Version) > MaximumBindingFieldLength {
			return nil, fieldError("fingerprint.bindings.version", "must be non-empty, trimmed, and bounded")
		}
		if existing, found := seenVersion[binding.Name]; found {
			if existing != binding.Version {
				return nil, fieldError("fingerprint.bindings", "must not bind the same name to conflicting versions")
			}
			continue
		}
		seenVersion[binding.Name] = binding.Version
		normalized = append(normalized, binding)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Name != normalized[j].Name {
			return normalized[i].Name < normalized[j].Name
		}
		return normalized[i].Version < normalized[j].Version
	})
	return normalized, nil
}

func normalizedBindings(values []ToolchainBinding) bool {
	if values == nil {
		return false
	}
	if len(values) > MaximumToolchainBindings {
		return false
	}
	seenNames := make(map[string]struct{}, len(values))
	for index, binding := range values {
		if strings.TrimSpace(binding.Name) != binding.Name || binding.Name == "" || len(binding.Name) > MaximumBindingFieldLength {
			return false
		}
		if strings.TrimSpace(binding.Version) != binding.Version || binding.Version == "" || len(binding.Version) > MaximumBindingFieldLength {
			return false
		}
		if norm.NFC.String(binding.Name) != binding.Name || norm.NFC.String(binding.Version) != binding.Version {
			return false
		}
		if _, found := seenNames[binding.Name]; found {
			return false
		}
		seenNames[binding.Name] = struct{}{}
		if index > 0 {
			previous := values[index-1]
			if previous.Name > binding.Name || (previous.Name == binding.Name && previous.Version >= binding.Version) {
				return false
			}
		}
	}
	return true
}

func normalizeAuthorityClasses(values []AuthorityClass) ([]AuthorityClass, error) {
	if len(values) > len(allAuthorityClasses) {
		return nil, fieldError("fingerprint.requested_authority", "must not exceed the declared authority-class set")
	}
	seen := make(map[AuthorityClass]struct{}, len(values))
	normalized := make([]AuthorityClass, 0, len(values))
	for _, value := range values {
		if !value.IsValid() {
			return nil, fieldError("fingerprint.requested_authority", "is not declared")
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized, nil
}

func normalizedAuthority(values []AuthorityClass) bool {
	if values == nil {
		return false
	}
	if len(values) > len(allAuthorityClasses) {
		return false
	}
	for index, value := range values {
		if !value.IsValid() {
			return false
		}
		if index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}
