package retrieval

import (
	"context"
	"strings"
	"unicode"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// MinimumDocumentationMatchTermLength ignores very short hint tokens, which
// would otherwise match almost any prose and turn documentation discovery
// into noise rather than signal.
const MinimumDocumentationMatchTermLength = 4

// discoverByAtomDocumentation matches the task's own affected-symbol and
// affected-package hints against the retrieval-bearing text of every
// admitted atom documentation revision in the project (M21-G07: "rich atom
// comments improve candidate discovery").
//
// It runs AFTER exact-identity and structured-field discovery and after the
// name/alias channel, and BEFORE any similarity seam, keeping the ordering
// M21-167 requires. Matching is deterministic, case-insensitive whole-term
// containment over already-loaded text — never a similarity score.
//
// The gate half of M21-G07 is upheld by construction rather than by care
// here: this function only ever calls add(), which records a DISCOVERY
// channel. Every candidate it names still passes through the identical
// retrievalgate.EvaluateEligibility chain as every other candidate, and
// retrievalgate's own structural tests prove no eligibility input can carry
// a discovery signal. Richer documentation therefore changes only what is
// FOUND, never what is permitted.
//
// Like discoverByAtomName, this refines attribution for
// ExecutableAtomReference candidates that structured-field discovery already
// admitted for the task's repository; it does not admit atoms from outside
// that repository's own candidate set.
func (service *Service) discoverByAtomDocumentation(
	ctx context.Context,
	input PreWorkGateInput,
	atomReferenceRevisions []storage.MemoryArtifactRevisionRecord,
	add func(storage.MemoryArtifactRevisionRecord, storage.MemoryRetrievalCandidateSource, string),
) error {
	terms := documentationMatchTerms(input)
	if len(terms) == 0 || len(atomReferenceRevisions) == 0 {
		return nil
	}

	byAtom := map[domain.AtomID][]storage.MemoryArtifactRevisionRecord{}
	for _, revision := range atomReferenceRevisions {
		if revision.Content.AtomReference == nil {
			continue
		}
		byAtom[revision.Content.AtomReference.Atom] = append(
			byAtom[revision.Content.AtomReference.Atom], revision,
		)
	}
	if len(byAtom) == 0 {
		return nil
	}

	documents, err := service.store.ListAtomDocumentationDiscoveryTextByProject(ctx, input.ProjectID)
	if err != nil {
		return err
	}

	for _, document := range documents {
		revisions, relevant := byAtom[document.AtomID]
		if !relevant {
			continue
		}
		label, matched := matchDocumentationTerms(document, terms)
		if !matched {
			continue
		}
		for _, revision := range revisions {
			// Documentation text is descriptive prose, so it is a
			// structured-field discovery signal, never an exact-identity
			// one: the atom's identity did not match, its description did.
			add(revision, storage.RetrievalCandidateApplicabilityPass, label)
		}
	}
	return nil
}

// documentationMatchTerms collects the deduplicated, lowercased hint terms a
// documentation match may use. Only the task's own structured affected-symbol
// and affected-package hints are used; free-text descriptive fields never
// enter, preserving fingerprint's exact/descriptive split.
func documentationMatchTerms(input PreWorkGateInput) []string {
	seen := map[string]bool{}
	var terms []string
	for _, hint := range append(
		append([]string{}, input.Task.AffectedSymbols...),
		input.Task.AffectedPackages...,
	) {
		for _, term := range splitHintTerms(hint) {
			if len(term) < MinimumDocumentationMatchTermLength || seen[term] {
				continue
			}
			seen[term] = true
			terms = append(terms, term)
		}
	}
	return terms
}

// splitHintTerms breaks a hint into lowercased word terms, so a hint such as
// "ReserveAccountFunds" or "internal/storage" contributes its parts rather
// than only matching verbatim.
func splitHintTerms(hint string) []string {
	fields := strings.FieldsFunc(hint, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var terms []string
	for _, field := range fields {
		terms = append(terms, strings.ToLower(field))
		for _, part := range splitCamelHint(field) {
			if part != field {
				terms = append(terms, strings.ToLower(part))
			}
		}
	}
	return terms
}

// splitCamelHint splits a camel or Pascal identifier into its words.
func splitCamelHint(field string) []string {
	var parts []string
	runes := []rune(field)
	start := 0
	for index := 1; index < len(runes); index++ {
		if unicode.IsUpper(runes[index]) && !unicode.IsUpper(runes[index-1]) {
			parts = append(parts, string(runes[start:index]))
			start = index
		}
	}
	parts = append(parts, string(runes[start:]))
	return parts
}

// matchDocumentationTerms reports the first documentation field whose text
// contains any hint term, returning a label naming that field so a reviewer
// can see why the atom surfaced (M21-168's "which text caused entry").
func matchDocumentationTerms(document storage.AtomDocumentationDiscoveryText, terms []string) (string, bool) {
	for _, label := range atomDocumentationMatchOrder {
		text, present := document.Fields[label]
		if !present || text == "" {
			continue
		}
		lowered := strings.ToLower(text)
		for _, term := range terms {
			if strings.Contains(lowered, term) {
				return label + ": " + term, true
			}
		}
	}
	return "", false
}

// atomDocumentationMatchOrder fixes the field-scan order so the reported
// match label is deterministic for identical inputs.
var atomDocumentationMatchOrder = []string{
	"Purpose",
	"Retrieval concepts",
	"Use when",
	"Semantics",
	"Do not use when",
}
