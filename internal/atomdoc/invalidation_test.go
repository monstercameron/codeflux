package atomdoc

import (
	"context"
	"strings"
	"testing"
)

func containsTrigger(triggers []EmbeddingLifecycleTrigger, want EmbeddingLifecycleTrigger) bool {
	for _, trigger := range triggers {
		if trigger == want {
			return true
		}
	}
	return false
}

// TestDetermineDocumentationEmbeddingLifecycleTriggersNoChangeProducesNoTriggers
// proves an unchanged change surface produces neither a queue nor an
// invalidation decision.
func TestDetermineDocumentationEmbeddingLifecycleTriggersNoChangeProducesNoTriggers(t *testing.T) {
	triggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{})
	if len(triggers) != 0 {
		t.Fatalf("expected no triggers for an unchanged surface, got %v", triggers)
	}
	if RequiresQueuedReembedding(triggers) {
		t.Error("expected RequiresQueuedReembedding == false")
	}
	if RequiresImmediateInvalidation(triggers) {
		t.Error("expected RequiresImmediateInvalidation == false")
	}
}

// TestEmbeddingConfigChangeOnlyQueuesWithoutImmediateInvalidation is
// M21-133's config half: an embedding-configuration change alone queues a
// new embedding but does not require the prior vector to stop serving
// immediately, since the semantics it captured have not changed.
func TestEmbeddingConfigChangeOnlyQueuesWithoutImmediateInvalidation(t *testing.T) {
	triggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{EmbeddingConfigChanged: true})
	if !containsTrigger(triggers, EmbeddingLifecycleTriggerEmbeddingConfigChanged) {
		t.Fatalf("expected EmbeddingLifecycleTriggerEmbeddingConfigChanged, got %v", triggers)
	}
	if !RequiresQueuedReembedding(triggers) {
		t.Error("expected RequiresQueuedReembedding == true")
	}
	if RequiresImmediateInvalidation(triggers) {
		t.Error("expected RequiresImmediateInvalidation == false for a config-only change")
	}
}

// TestNormalizedInputChangeQueuesAndInvalidatesImmediately proves a semantic
// comment change requires BOTH queuing a new embedding (M21-133) and
// immediately invalidating the stale vector's retrieval influence (M21-134),
// unlike a config-only change.
func TestNormalizedInputChangeQueuesAndInvalidatesImmediately(t *testing.T) {
	triggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{NormalizedInputHashChanged: true})
	if !RequiresQueuedReembedding(triggers) {
		t.Error("expected RequiresQueuedReembedding == true")
	}
	if !RequiresImmediateInvalidation(triggers) {
		t.Error("expected RequiresImmediateInvalidation == true")
	}
}

// TestContractDependencyEvidenceChangesInvalidateWithoutAutomaticRegeneration
// is M21-134: contract, dependency-binding, and evidence-validity changes
// each require immediate invalidation, with no automatic
// queue-reembedding assumption (a comment-unchanged atom's contract may need
// human re-authoring before any new vector should be produced at all).
func TestContractDependencyEvidenceChangesInvalidateWithoutAutomaticRegeneration(t *testing.T) {
	cases := []struct {
		name    string
		surface EmbeddingLifecycleChangeSurface
		want    EmbeddingLifecycleTrigger
	}{
		{"contract", EmbeddingLifecycleChangeSurface{ContractHashChanged: true}, EmbeddingLifecycleTriggerContractChanged},
		{"dependency binding", EmbeddingLifecycleChangeSurface{DependencyBindingsChanged: true}, EmbeddingLifecycleTriggerDependencyBindingChanged},
		{"evidence validity", EmbeddingLifecycleChangeSurface{EvidenceValidityChanged: true}, EmbeddingLifecycleTriggerEvidenceValidityChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			triggers := DetermineDocumentationEmbeddingLifecycleTriggers(tc.surface)
			if !containsTrigger(triggers, tc.want) {
				t.Fatalf("expected trigger %q, got %v", tc.want, triggers)
			}
			if !RequiresImmediateInvalidation(triggers) {
				t.Error("expected RequiresImmediateInvalidation == true")
			}
			if RequiresQueuedReembedding(triggers) {
				t.Error("expected RequiresQueuedReembedding == false: comment/config are unchanged, so no automatic regeneration is implied")
			}
		})
	}
}

// TestDetermineEmbeddingRetrievalEligibilityRetainsHistoricalButExcludesFromActive
// is M21-135: retention and active-retrieval eligibility are different
// questions. A non-current snapshot is always historical regardless of
// triggers (it has already been superseded); a current snapshot is active
// only while no immediate-invalidation trigger has fired.
func TestDetermineEmbeddingRetrievalEligibilityRetainsHistoricalButExcludesFromActive(t *testing.T) {
	noTriggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{})
	invalidatingTriggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{ContractHashChanged: true})

	cases := []struct {
		name      string
		isCurrent bool
		triggers  []EmbeddingLifecycleTrigger
		want      EmbeddingRetrievalEligibility
	}{
		{"current, no triggers -> active", true, noTriggers, EmbeddingRetrievalEligibilityActive},
		{"current, invalidating trigger -> historical, still retained", true, invalidatingTriggers, EmbeddingRetrievalEligibilityHistorical},
		{"superseded (non-current), no triggers -> historical, still retained", false, noTriggers, EmbeddingRetrievalEligibilityHistorical},
		{"superseded (non-current), invalidating trigger -> historical, still retained", false, invalidatingTriggers, EmbeddingRetrievalEligibilityHistorical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetermineEmbeddingRetrievalEligibility(tc.isCurrent, tc.triggers)
			if got != tc.want {
				t.Errorf("DetermineEmbeddingRetrievalEligibility(%v, %v) = %s, want %s", tc.isCurrent, tc.triggers, got, tc.want)
			}
		})
	}
	// This package defines no function anywhere that deletes, prunes, or
	// otherwise removes an EmbeddingProvenance/vector snapshot: retention is
	// structural (there is nothing to call to lose lineage), not a policy
	// this test can defeat by choosing bad inputs.
}

// --- M21-139..142: end-to-end lifecycle-trigger scenarios driven through
// the real AdmitSourceAtomDocumentation pipeline. ---

// TestSemanticCommentChangeCreatesNewRevisionAndQueuesPendingVector is
// M21-139: a semantic (not formatting-only) comment edit creates a new
// atom-documentation revision (a different NormalizedInputHash and
// RevisionID) and requires queuing a pending re-embedding.
func TestSemanticCommentChangeCreatesNewRevisionAndQueuesPendingVector(t *testing.T) {
	original := mustAdmissionInput(t, validFixtureSource, nil)
	originalResult, err := AdmitSourceAtomDocumentation(context.Background(), original)
	if err != nil || originalResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit original: err=%v result=%#v", err, originalResult)
	}

	semanticallyChangedSource := strings.Replace(validFixtureSource,
		"Hold scarce widget inventory for one checkout session so two shoppers\n//     cannot both complete a sale for the same physical unit.",
		"Hold scarce widget inventory for one checkout session so THREE shoppers\n//     can never simultaneously complete a sale for the same physical unit.",
		1,
	)
	if semanticallyChangedSource == validFixtureSource {
		t.Fatal("test fixture setup failed to introduce a semantic difference")
	}
	changed := mustAdmissionInput(t, semanticallyChangedSource, nil)
	changed.AtomID = original.AtomID
	changed.AtomVersion = original.AtomVersion
	changed.ContractHash = original.ContractHash
	changedResult, err := AdmitSourceAtomDocumentation(context.Background(), changed)
	if err != nil || changedResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit changed: err=%v result=%#v", err, changedResult)
	}

	if originalResult.Revision.NormalizedInputHash == changedResult.Revision.NormalizedInputHash {
		t.Fatal("expected a semantic edit to change the normalized-input hash")
	}
	if originalResult.Revision.RevisionID == changedResult.Revision.RevisionID {
		t.Fatal("expected a semantic edit to create a new documentation-revision identity")
	}

	triggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{
		NormalizedInputHashChanged: originalResult.Revision.NormalizedInputHash != changedResult.Revision.NormalizedInputHash,
	})
	if !RequiresQueuedReembedding(triggers) {
		t.Error("expected the semantic comment change to queue a pending re-embedding (M21-133)")
	}
	if !RequiresImmediateInvalidation(triggers) {
		t.Error("expected the semantic comment change to also immediately invalidate the stale vector (M21-134)")
	}
}

// TestFormattingOnlyCommentChangePreservesNormalizedHashAndRequiresNoAction
// is M21-140: a formatting-only comment edit changes SourceCommentHash but
// preserves NormalizedInputHash, and therefore requires neither queuing a
// re-embedding nor invalidating the existing vector. This is the decisive
// proof that the two hashes' distinction is load-bearing, not incidental.
func TestFormattingOnlyCommentChangePreservesNormalizedHashAndRequiresNoAction(t *testing.T) {
	original := mustAdmissionInput(t, validFixtureSource, nil)
	originalResult, err := AdmitSourceAtomDocumentation(context.Background(), original)
	if err != nil || originalResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit original: err=%v result=%#v", err, originalResult)
	}

	reformatted := strings.Replace(validFixtureSource,
		"//   Purpose:\n//     Hold scarce widget inventory for one checkout session so two shoppers\n//     cannot both complete a sale for the same physical unit.\n",
		"//   Purpose:\n//     Hold scarce widget inventory for one checkout session so two   shoppers\n//     cannot both complete a sale for the same physical unit.\n",
		1,
	)
	if reformatted == validFixtureSource {
		t.Fatal("test fixture setup failed to introduce a formatting difference")
	}
	changed := mustAdmissionInput(t, reformatted, nil)
	changed.AtomID = original.AtomID
	changed.AtomVersion = original.AtomVersion
	changed.ContractHash = original.ContractHash
	changedResult, err := AdmitSourceAtomDocumentation(context.Background(), changed)
	if err != nil || changedResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit changed: err=%v result=%#v", err, changedResult)
	}

	if originalResult.Revision.SourceCommentHash == changedResult.Revision.SourceCommentHash {
		t.Fatal("expected the source-comment hash to change after a formatting-only edit")
	}
	if originalResult.Revision.NormalizedInputHash != changedResult.Revision.NormalizedInputHash {
		t.Fatal("expected the normalized-input hash to stay stable across a whitespace-only edit")
	}

	triggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{
		NormalizedInputHashChanged: originalResult.Revision.NormalizedInputHash != changedResult.Revision.NormalizedInputHash,
	})
	if len(triggers) != 0 {
		t.Fatalf("expected no lifecycle triggers for a formatting-only edit, got %v", triggers)
	}
	if RequiresQueuedReembedding(triggers) {
		t.Error("expected RequiresQueuedReembedding == false: nothing semantic changed")
	}
	if RequiresImmediateInvalidation(triggers) {
		t.Error("expected RequiresImmediateInvalidation == false: the existing vector still accurately describes this atom")
	}
}

// TestContractChangeInvalidatesOtherwiseUnchangedCommentVector is M21-141:
// changing ContractHash alone (comment text identical, so
// NormalizedInputHash is identical) must still invalidate the existing
// vector's retrieval influence immediately.
func TestContractChangeInvalidatesOtherwiseUnchangedCommentVector(t *testing.T) {
	original := mustAdmissionInput(t, validFixtureSource, nil)
	originalResult, err := AdmitSourceAtomDocumentation(context.Background(), original)
	if err != nil || originalResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit original: err=%v result=%#v", err, originalResult)
	}

	changed := mustAdmissionInput(t, validFixtureSource, nil)
	changed.AtomID = original.AtomID
	changed.AtomVersion = original.AtomVersion
	changed.ContractHash, err = ParseContractHash(strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("parse contract hash: %v", err)
	}
	changedResult, err := AdmitSourceAtomDocumentation(context.Background(), changed)
	if err != nil || changedResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit changed: err=%v result=%#v", err, changedResult)
	}

	if originalResult.Revision.NormalizedInputHash != changedResult.Revision.NormalizedInputHash {
		t.Fatal("test fixture setup failed to keep the comment identical: normalized-input hash changed")
	}
	if originalResult.Revision.ContractHash == changedResult.Revision.ContractHash {
		t.Fatal("test fixture setup failed to change the contract hash")
	}

	triggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{
		ContractHashChanged: originalResult.Revision.ContractHash != changedResult.Revision.ContractHash,
	})
	if !RequiresImmediateInvalidation(triggers) {
		t.Fatal("expected a contract change to invalidate the otherwise-unchanged comment's vector immediately")
	}
}

// TestDependencyBindingChangeInvalidatesActiveRetrieval is M21-142: changing
// the bound DependencyBinding set alone must invalidate active retrieval
// immediately, even though the comment is untouched.
func TestDependencyBindingChangeInvalidatesActiveRetrieval(t *testing.T) {
	original := mustAdmissionInput(t, validFixtureSource, nil)
	original.DependencyBindings = []DependencyBinding{{Name: "inventory-ledger", Version: "v3"}}
	originalResult, err := AdmitSourceAtomDocumentation(context.Background(), original)
	if err != nil || originalResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit original: err=%v result=%#v", err, originalResult)
	}

	changed := mustAdmissionInput(t, validFixtureSource, nil)
	changed.AtomID = original.AtomID
	changed.AtomVersion = original.AtomVersion
	changed.ContractHash = original.ContractHash
	changed.DependencyBindings = []DependencyBinding{{Name: "inventory-ledger", Version: "v4"}}
	changedResult, err := AdmitSourceAtomDocumentation(context.Background(), changed)
	if err != nil || changedResult.Status != AdmissionStatusAdmitted {
		t.Fatalf("admit changed: err=%v result=%#v", err, changedResult)
	}

	if originalResult.Revision.NormalizedInputHash != changedResult.Revision.NormalizedInputHash {
		t.Fatal("test fixture setup failed to keep the comment identical")
	}
	bindingsChanged := originalResult.Revision.DependencyBindings[0].Version != changedResult.Revision.DependencyBindings[0].Version
	if !bindingsChanged {
		t.Fatal("test fixture setup failed to change the dependency binding version")
	}

	triggers := DetermineDocumentationEmbeddingLifecycleTriggers(EmbeddingLifecycleChangeSurface{
		DependencyBindingsChanged: bindingsChanged,
	})
	if !RequiresImmediateInvalidation(triggers) {
		t.Fatal("expected a dependency-binding change to invalidate active retrieval immediately")
	}
	eligibility := DetermineEmbeddingRetrievalEligibility(true, triggers)
	if eligibility != EmbeddingRetrievalEligibilityHistorical {
		t.Fatalf("expected the current snapshot to lose active eligibility after a dependency-binding change, got %s", eligibility)
	}
}
