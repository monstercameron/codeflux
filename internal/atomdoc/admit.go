package atomdoc

import (
	"context"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/redact"
)

// AdmissionStatus is the outcome of one atom-documentation admission attempt.
type AdmissionStatus string

const (
	AdmissionStatusAdmitted AdmissionStatus = "admitted"
	AdmissionStatusRejected AdmissionStatus = "rejected"
)

// RejectionReason classifies why one admission attempt was rejected, for a
// later lane to persist the exact reason rather than a generic failure
// (M21-123).
type RejectionReason string

const (
	RejectionReasonNone                     RejectionReason = ""
	RejectionReasonMalformedSource          RejectionReason = "malformed-source-comment"
	RejectionReasonOpeningSentenceInvalid   RejectionReason = "opening-sentence-invalid"
	RejectionReasonSchemaPolicyViolation    RejectionReason = "schema-policy-violation"
	RejectionReasonSensitiveContentDetected RejectionReason = "sensitive-content-detected"
)

// DocumentationRevision is the immutable, admitted atom-documentation record
// this package returns for a later lane to persist. It binds all three
// distinct identities from M21-102 (atom, atom version, and this
// documentation revision itself) together with both hashes from M21-120 and
// M21-121, and the contract/dependency binding in effect at admission time
// (M21-122).
type DocumentationRevision struct {
	RevisionID               DocumentationRevisionID
	AtomID                   domain.AtomID
	AtomVersion              AtomVersionID
	SchemaVersion            uint32
	Authoring                AuthoringMode
	SourceRepositoryRevision string
	SourceCommentHash        SourceCommentHash
	NormalizedInputHash      NormalizedInputHash
	ContractHash             ContractHash
	DependencyBindings       []DependencyBinding
	Document                 Document
	Flags                    []QualityFlag
	CreatedAt                time.Time
}

// AdmissionResult is the typed outcome of one admission attempt (M21-123).
// This package never writes SQLite; the schema-owning lane persists this
// result as a row (or a rejection record) in atom_documentation_revisions.
type AdmissionResult struct {
	Status          AdmissionStatus
	Revision        *DocumentationRevision
	RejectionReason RejectionReason
	RejectionDetail string
	RedactionReport redact.Report
}

// SourceAdmissionInput binds one located source candidate to the identity,
// contract, and redaction facts an admission attempt requires.
type SourceAdmissionInput struct {
	Candidate                SourceCandidate
	AtomID                   domain.AtomID
	AtomVersion              AtomVersionID
	SourceRepositoryRevision string
	ContractHash             ContractHash
	DependencyBindings       []DependencyBinding
	RedactionPipeline        *redact.Pipeline
}

// AdmitSourceAtomDocumentation runs the complete source-authored admission
// pipeline for one located atom declaration:
//
//   - parse the schema header and fields without losing list structure
//     (M21-108..111);
//   - normalize insignificant whitespace while preserving meaningful content
//     (M21-112, M21-113);
//   - enforce the field-completeness/order/None policy and the opening-
//     sentence convention (M21-114..116);
//   - flag, without dropping, likely keyword stuffing or repeated
//     boilerplate (M21-117);
//   - scrub every field with the shared redaction pipeline and reject
//     documentation carrying known-sensitive content (M21-118, M21-119);
//   - compute the exact source-comment hash before normalization and the
//     normalized-input hash after normalization and scrubbing (M21-120,
//     M21-121); and
//   - bind the result to the current atom contract hash and dependency
//     bindings (M21-122).
//
// It never writes to SQLite. It returns a typed AdmissionResult describing
// success or the exact rejection reason for a later lane to persist
// (M21-123).
func AdmitSourceAtomDocumentation(ctx context.Context, input SourceAdmissionInput) (AdmissionResult, error) {
	if err := ctx.Err(); err != nil {
		return AdmissionResult{}, err
	}
	if input.AtomID.IsZero() {
		return AdmissionResult{}, fmt.Errorf("%w: atom identity is required", ErrInvalidAtomDocumentation)
	}
	if input.AtomVersion.IsZero() {
		return AdmissionResult{}, fmt.Errorf("%w: atom-version identity is required", ErrInvalidAtomDocumentation)
	}
	if input.ContractHash.IsZero() {
		return AdmissionResult{}, fmt.Errorf("%w: contract hash is required", ErrInvalidAtomDocumentation)
	}
	if len(input.DependencyBindings) > MaximumDependencyBindings {
		return AdmissionResult{}, fmt.Errorf("%w: dependency bindings exceed the maximum bound of %d", ErrInvalidAtomDocumentation, MaximumDependencyBindings)
	}
	for _, binding := range input.DependencyBindings {
		if err := binding.Validate(); err != nil {
			return AdmissionResult{}, fmt.Errorf("%w: %s", ErrInvalidAtomDocumentation, err)
		}
	}

	parsed, err := ParseAtomDocumentationComment(input.Candidate)
	if err != nil {
		return AdmissionResult{
			Status:          AdmissionStatusRejected,
			RejectionReason: RejectionReasonMalformedSource,
			RejectionDetail: err.Error(),
		}, nil
	}
	sourceHash := ComputeSourceCommentHash(parsed.RawText)

	if err := ValidateAtomDocumentationOpeningSentence(parsed.Identifier, parsed.OpeningSentence); err != nil {
		return AdmissionResult{
			Status:          AdmissionStatusRejected,
			RejectionReason: RejectionReasonOpeningSentenceInvalid,
			RejectionDetail: err.Error(),
		}, nil
	}

	doc, err := ValidateAtomDocumentationSchema(parsed.Fields)
	if err != nil {
		return AdmissionResult{
			Status:          AdmissionStatusRejected,
			RejectionReason: RejectionReasonSchemaPolicyViolation,
			RejectionDetail: err.Error(),
		}, nil
	}
	flags := FlagAtomDocumentationQualityIssues(doc)

	scrubbed, report, err := ScrubAtomDocumentationForPersistence(input.RedactionPipeline, doc)
	if err != nil {
		return AdmissionResult{}, err
	}
	if ContainsSensitiveContent(report) {
		return AdmissionResult{
			Status:          AdmissionStatusRejected,
			RejectionReason: RejectionReasonSensitiveContentDetected,
			RejectionDetail: fmt.Sprintf("redaction pipeline matched %d sensitive value(s)", report.Redactions),
			RedactionReport: report,
		}, nil
	}

	normalizedHash := ComputeNormalizedDocumentationInputHash(parsed.SchemaVersion, scrubbed)
	revisionID := deriveDocumentationRevisionID(input.AtomID, input.AtomVersion, parsed.SchemaVersion, normalizedHash, input.ContractHash)

	revision := &DocumentationRevision{
		RevisionID:               revisionID,
		AtomID:                   input.AtomID,
		AtomVersion:              input.AtomVersion,
		SchemaVersion:            parsed.SchemaVersion,
		Authoring:                AuthoringSourceAuthored,
		SourceRepositoryRevision: input.SourceRepositoryRevision,
		SourceCommentHash:        sourceHash,
		NormalizedInputHash:      normalizedHash,
		ContractHash:             input.ContractHash,
		DependencyBindings:       append([]DependencyBinding(nil), input.DependencyBindings...),
		Document:                 scrubbed,
		Flags:                    flags,
		CreatedAt:                time.Now().UTC(),
	}
	return AdmissionResult{
		Status:          AdmissionStatusAdmitted,
		Revision:        revision,
		RedactionReport: report,
	}, nil
}
