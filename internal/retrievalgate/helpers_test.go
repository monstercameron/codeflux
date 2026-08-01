package retrievalgate

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
)

func mustProjectID(t *testing.T) domain.ProjectID {
	t.Helper()
	id, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustRepositoryID(t *testing.T) domain.RepositoryID {
	t.Helper()
	id, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustMemoryArtifactRevisionID(t *testing.T) domain.MemoryArtifactRevisionID {
	t.Helper()
	id, err := domain.NewMemoryArtifactRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// sampleFingerprint builds one valid, current-schema task fingerprint for
// tests: project prj, repository repo, requiring domain.AssuranceLevelModelVerified,
// bound to go@1.26.0, with two affected paths under internal/retrievalgate/.
func sampleFingerprint(t *testing.T, project domain.ProjectID, repository domain.RepositoryID) fingerprint.ExactFingerprint {
	t.Helper()
	value, err := fingerprint.BuildExactFingerprint(fingerprint.ExactFingerprintInput{
		Project:    project,
		Repository: repository,
		BaseRevision: domain.RevisionBinding{
			Known:         true,
			ExactRevision: "abc123def456",
		},
		TaskClass:        fingerprint.TaskClassBugFix,
		AffectedPaths:    []string{"internal/retrievalgate/gates.go"},
		AffectedPackages: []string{"codeflux.dev/codeflux/internal/retrievalgate"},
		AffectedSymbols:  []string{"EvaluateEligibility"},
		Bindings: []fingerprint.ToolchainBinding{
			{Name: "go", Version: "1.26.0"},
		},
		Risk:              domain.RiskLevelElevated,
		RequiredAssurance: domain.AssuranceLevelModelVerified,
		RequestedAuthority: []fingerprint.AuthorityClass{
			fingerprint.AuthorityClassTaskWrite,
		},
	})
	if err != nil {
		t.Fatalf("BuildExactFingerprint: %v", err)
	}
	return value
}

// eligibleCandidate builds one EligibilityCandidate that passes every gate
// against task/boundary, for tests that need a legitimate baseline before
// perturbing exactly one field to prove the matching gate rejects it (and
// nothing else does).
func eligibleCandidate(t *testing.T, scope domain.MemoryProjectScope, task fingerprint.ExactFingerprint) EligibilityCandidate {
	t.Helper()
	return EligibilityCandidate{
		RevisionID:                 mustMemoryArtifactRevisionID(t),
		Scope:                      scope,
		RequiredToolchainBindings:  []fingerprint.ToolchainBinding{{Name: "go", Version: "1.26.0"}},
		RequiredDependencyBindings: nil,
		Applicability:              nil,
		Maturity:                   domain.MaturityStateValidated,
		SupportingEvidenceAssurance: []domain.AssuranceLevel{
			domain.AssuranceLevelFullyEvaluated,
		},
	}
}
