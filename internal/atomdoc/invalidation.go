package atomdoc

// EmbeddingLifecycleTrigger names one reason a documentation-lane,
// naming-lane, or embedding-configuration change requires the (currently
// gated) vector-runtime lane to act on a previously derived embedding, once
// that runtime exists (M21-133, M21-134, M21-164). This package never
// regenerates, invalidates, or otherwise touches a vector itself:
// DetermineDocumentationEmbeddingLifecycleTriggers only names which
// triggers fired, for that gated lane to act on — mirroring the shape of
// atomname.EmbeddingInvalidationTrigger and
// atomname.DetermineEmbeddingInvalidationTriggers, extended here to cover
// the documentation lane and embedding configuration.
type EmbeddingLifecycleTrigger string

const (
	// EmbeddingLifecycleTriggerNormalizedInputChanged fires when a semantic
	// (not formatting-only) comment edit changed NormalizedInputHash
	// (M21-121). It requires both queuing a new embedding (M21-133) and
	// immediately invalidating the stale one's retrieval influence
	// (M21-134), because the vector now describes outdated semantics.
	EmbeddingLifecycleTriggerNormalizedInputChanged EmbeddingLifecycleTrigger = "normalized-input-changed"
	// EmbeddingLifecycleTriggerEmbeddingConfigChanged fires when the
	// embedding model, dimensions, normalization rule, or embedding-input
	// schema version changed (M21-133). It requires queuing a new embedding
	// only: the prior vector still describes unchanged, currently valid
	// semantics, so it may continue to influence retrieval until the
	// replacement lands.
	EmbeddingLifecycleTriggerEmbeddingConfigChanged EmbeddingLifecycleTrigger = "embedding-config-changed"
	// EmbeddingLifecycleTriggerContractChanged fires when the atom's bound
	// ContractHash changed. It requires immediately invalidating retrieval
	// influence (M21-134), with no automatic regeneration assumption.
	EmbeddingLifecycleTriggerContractChanged EmbeddingLifecycleTrigger = "contract-changed"
	// EmbeddingLifecycleTriggerDependencyBindingChanged fires when the
	// bound DependencyBinding set changed. Immediate invalidation (M21-134).
	EmbeddingLifecycleTriggerDependencyBindingChanged EmbeddingLifecycleTrigger = "dependency-binding-changed"
	// EmbeddingLifecycleTriggerEvidenceValidityChanged fires when the
	// documentation revision's persisted validation/evidence-validity
	// status changed, for example from admitted to invalidated. Immediate
	// invalidation (M21-134). This package never stores that status itself;
	// the caller supplies the comparison.
	EmbeddingLifecycleTriggerEvidenceValidityChanged EmbeddingLifecycleTrigger = "evidence-validity-changed"
	// EmbeddingLifecycleTriggerNamingLaneChanged fires when the atom's
	// naming lane changed (canonical name, normalized phrase, or active
	// alias set) rather than its documentation lane (M21-164). It requires
	// both queuing a new embedding and immediately invalidating the stale
	// one, the same combined effect as a semantic comment change, because a
	// renamed atom's retrieval-relevant description changed even though its
	// documentation did not. internal/atomname owns detecting this trigger
	// (DetermineAtomEmbeddingLifecycleTriggers extends
	// atomname.DetermineEmbeddingInvalidationTriggers rather than
	// duplicating it); this package only declares the shared trigger
	// vocabulary and its effects.
	EmbeddingLifecycleTriggerNamingLaneChanged EmbeddingLifecycleTrigger = "naming-lane-changed"
)

// RequiresQueuedReembedding reports whether trigger requires queuing a new
// embedding (M21-133): the underlying content or embedding configuration
// changed, so a fresh vector should eventually replace the current one.
func (trigger EmbeddingLifecycleTrigger) RequiresQueuedReembedding() bool {
	switch trigger {
	case EmbeddingLifecycleTriggerNormalizedInputChanged,
		EmbeddingLifecycleTriggerEmbeddingConfigChanged,
		EmbeddingLifecycleTriggerNamingLaneChanged:
		return true
	default:
		return false
	}
}

// RequiresImmediateInvalidation reports whether trigger requires
// immediately invalidating the prior vector's retrieval influence, before
// any replacement embedding exists (M21-134). A pure embedding-configuration
// change is deliberately excluded: the prior vector still describes
// currently valid semantics under the prior configuration and may continue
// to influence retrieval until its replacement lands.
func (trigger EmbeddingLifecycleTrigger) RequiresImmediateInvalidation() bool {
	switch trigger {
	case EmbeddingLifecycleTriggerNormalizedInputChanged,
		EmbeddingLifecycleTriggerContractChanged,
		EmbeddingLifecycleTriggerDependencyBindingChanged,
		EmbeddingLifecycleTriggerEvidenceValidityChanged,
		EmbeddingLifecycleTriggerNamingLaneChanged:
		return true
	default:
		return false
	}
}

// EmbeddingLifecycleChangeSurface is the documentation-lane and
// embedding-configuration change surface
// DetermineDocumentationEmbeddingLifecycleTriggers inspects. Every field
// compares the same atom/version's before/after state; this package never
// compares across different atoms.
type EmbeddingLifecycleChangeSurface struct {
	// NormalizedInputHashChanged is true when a semantic (not
	// formatting-only) comment edit changed NormalizedInputHash (M21-121,
	// M21-133, M21-134). A formatting-only edit that changes only
	// SourceCommentHash must be reported as false here: comparing
	// NormalizedInputHash values rather than SourceCommentHash values is
	// what keeps the two hashes' distinction meaningful for embedding-input
	// purposes (M21-140).
	NormalizedInputHashChanged bool
	// EmbeddingConfigChanged is true when the embedding model, dimensions,
	// normalization rule, or embedding-input schema version changed
	// (M21-133).
	EmbeddingConfigChanged bool
	// ContractHashChanged is true when the atom's bound ContractHash
	// changed (M21-134).
	ContractHashChanged bool
	// DependencyBindingsChanged is true when the bound DependencyBinding
	// set changed (M21-134).
	DependencyBindingsChanged bool
	// EvidenceValidityChanged is true when the persisted documentation-
	// revision validation/evidence-validity status changed (M21-134).
	EvidenceValidityChanged bool
}

// DetermineDocumentationEmbeddingLifecycleTriggers reports which
// EmbeddingLifecycleTrigger values fired for surface (M21-133, M21-134). It
// performs no regeneration, invalidation, or similarity computation.
func DetermineDocumentationEmbeddingLifecycleTriggers(surface EmbeddingLifecycleChangeSurface) []EmbeddingLifecycleTrigger {
	var triggers []EmbeddingLifecycleTrigger
	if surface.NormalizedInputHashChanged {
		triggers = append(triggers, EmbeddingLifecycleTriggerNormalizedInputChanged)
	}
	if surface.EmbeddingConfigChanged {
		triggers = append(triggers, EmbeddingLifecycleTriggerEmbeddingConfigChanged)
	}
	if surface.ContractHashChanged {
		triggers = append(triggers, EmbeddingLifecycleTriggerContractChanged)
	}
	if surface.DependencyBindingsChanged {
		triggers = append(triggers, EmbeddingLifecycleTriggerDependencyBindingChanged)
	}
	if surface.EvidenceValidityChanged {
		triggers = append(triggers, EmbeddingLifecycleTriggerEvidenceValidityChanged)
	}
	return triggers
}

// RequiresQueuedReembedding reports whether any trigger in triggers requires
// queuing a new embedding (M21-133).
func RequiresQueuedReembedding(triggers []EmbeddingLifecycleTrigger) bool {
	for _, trigger := range triggers {
		if trigger.RequiresQueuedReembedding() {
			return true
		}
	}
	return false
}

// RequiresImmediateInvalidation reports whether any trigger in triggers
// requires immediately invalidating the prior vector's retrieval influence
// (M21-134).
func RequiresImmediateInvalidation(triggers []EmbeddingLifecycleTrigger) bool {
	for _, trigger := range triggers {
		if trigger.RequiresImmediateInvalidation() {
			return true
		}
	}
	return false
}

// EmbeddingRetrievalEligibility classifies whether one specific
// derived-vector snapshot may currently influence ACTIVE retrieval,
// independent of whether Codeflux retains it (M21-135: "retention and
// eligibility are different questions"). This package defines no delete or
// prune function for a derived vector anywhere: retention is therefore an
// unconditional property every snapshot keeps forever, never a decision
// this function makes.
type EmbeddingRetrievalEligibility string

const (
	// EmbeddingRetrievalEligibilityActive marks the one snapshot (if any)
	// currently permitted to influence active retrieval for its atom.
	EmbeddingRetrievalEligibilityActive EmbeddingRetrievalEligibility = "active"
	// EmbeddingRetrievalEligibilityHistorical marks a retained snapshot
	// excluded from active retrieval: either it is not the atom's current
	// snapshot, or an immediate-invalidation trigger fired against it.
	EmbeddingRetrievalEligibilityHistorical EmbeddingRetrievalEligibility = "historical"
)

// DetermineEmbeddingRetrievalEligibility decides whether one derived-vector
// snapshot may influence active retrieval right now (M21-135). isCurrent
// reports whether snapshot is the atom's current (most recently produced)
// embedding; every non-current snapshot is always historical regardless of
// triggers, because a newer snapshot has already superseded it. A current
// snapshot is active only while none of triggers requires immediate
// invalidation (M21-134); it stops being active immediately even though it
// is not deleted and remains queryable as historical lineage.
func DetermineEmbeddingRetrievalEligibility(isCurrent bool, triggers []EmbeddingLifecycleTrigger) EmbeddingRetrievalEligibility {
	if isCurrent && !RequiresImmediateInvalidation(triggers) {
		return EmbeddingRetrievalEligibilityActive
	}
	return EmbeddingRetrievalEligibilityHistorical
}
