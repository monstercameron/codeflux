package storage

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	approvalRevision = "1111111111111111111111111111111111111111"
	approvalDigest   = "2222222222222222222222222222222222222222222222222222222222222222"
	otherDigest      = "3333333333333333333333333333333333333333333333333333333333333333"
)

// TestAUDIT011_AnApprovalResolvesOnlyForItsExactBindings covers the durable
// half of AUDIT-011.
//
// M08-049's approval was a caller-supplied path. Here every binding is part of
// the lookup, so an approval granted for one project, repository, revision,
// path, or digest cannot admit anything else.
func TestAUDIT011_AnApprovalResolvesOnlyForItsExactBindings(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 6100)
	repositoryID := testRepositoryID(t, 6101)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	if _, err := repositories.GrantRepositoryInstructionApproval(t.Context(),
		GrantRepositoryInstructionApproval{
			ID: "approval-1", ProjectID: projectID, RepositoryID: repositoryID,
			RepositoryRevision: approvalRevision, InstructionPath: "AGENTS.md",
			ContentSHA256: approvalDigest, Scope: InstructionApprovalRevision,
			ReasonRedacted: "reviewed by the operator",
		}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// The exact bindings resolve.
	approval, found, err := repositories.FindRepositoryInstructionApproval(
		t.Context(), projectID, repositoryID, approvalRevision, "AGENTS.md", approvalDigest)
	if err != nil || !found {
		t.Fatalf("the granted approval did not resolve: found=%v err=%v", found, err)
	}
	if !approval.Usable() {
		t.Fatal("a fresh revision-scoped approval is not usable")
	}

	for _, testCase := range []struct {
		name     string
		revision string
		path     string
		digest   string
	}{
		{"a different revision", "4444444444444444444444444444444444444444", "AGENTS.md", approvalDigest},
		{"a different path", approvalRevision, "docs/other.md", approvalDigest},
		{"different bytes", approvalRevision, "AGENTS.md", otherDigest},
	} {
		t.Run(testCase.name+" does not resolve", func(t *testing.T) {
			_, found, err := repositories.FindRepositoryInstructionApproval(
				t.Context(), projectID, repositoryID,
				testCase.revision, testCase.path, testCase.digest)
			if err != nil {
				t.Fatal(err)
			}
			if found {
				t.Fatal("an approval resolved for bindings it was not granted against")
			}
		})
	}

	// A different project never sees it, matching the project-boundary
	// predicate every other retrieval query carries.
	otherProject := testProjectID(t, 6110)
	otherRepository := testRepositoryID(t, 6111)
	mustCreateProjectRepository(t, repositories, otherProject, otherRepository)
	if _, found, err := repositories.FindRepositoryInstructionApproval(
		t.Context(), otherProject, otherRepository,
		approvalRevision, "AGENTS.md", approvalDigest,
	); err != nil || found {
		t.Fatalf("an approval crossed a project boundary: found=%v err=%v", found, err)
	}
}

// TestAUDIT011_ASingleUseApprovalCannotBeReplayed covers the consumption
// binding.
func TestAUDIT011_ASingleUseApprovalCannotBeReplayed(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 6120)
	repositoryID := testRepositoryID(t, 6121)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	granted, err := repositories.GrantRepositoryInstructionApproval(t.Context(),
		GrantRepositoryInstructionApproval{
			ID: "approval-single", ProjectID: projectID, RepositoryID: repositoryID,
			RepositoryRevision: approvalRevision, InstructionPath: "AGENTS.md",
			ContentSHA256: approvalDigest, Scope: InstructionApprovalSingleUse,
		})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	if err := repositories.ConsumeRepositoryInstructionApproval(
		t.Context(), "consumption-1", granted, "manifest-1"); err != nil {
		t.Fatalf("first consumption: %v", err)
	}

	spent, found, err := repositories.FindRepositoryInstructionApproval(
		t.Context(), projectID, repositoryID, approvalRevision, "AGENTS.md", approvalDigest)
	if err != nil || !found {
		t.Fatalf("the approval vanished after consumption: found=%v err=%v", found, err)
	}
	if spent.Usable() {
		t.Fatal("a spent single-use approval is still usable")
	}
	if err := repositories.ConsumeRepositoryInstructionApproval(
		t.Context(), "consumption-2", spent, "manifest-2",
	); err == nil {
		t.Fatal("a spent single-use approval was consumed a second time")
	}
}

// TestAUDIT011_ARevokedApprovalStopsAdmittingWithoutLosingItsRecord covers the
// revocation path, which is why the grant is immutable.
func TestAUDIT011_ARevokedApprovalStopsAdmittingWithoutLosingItsRecord(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 6130)
	repositoryID := testRepositoryID(t, 6131)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	if _, err := repositories.GrantRepositoryInstructionApproval(t.Context(),
		GrantRepositoryInstructionApproval{
			ID: "approval-revoked", ProjectID: projectID, RepositoryID: repositoryID,
			RepositoryRevision: approvalRevision, InstructionPath: "AGENTS.md",
			ContentSHA256: approvalDigest, Scope: InstructionApprovalRevision,
		}); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := repositories.RevokeRepositoryInstructionApproval(
		t.Context(), "approval-revoked"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	revoked, found, err := repositories.FindRepositoryInstructionApproval(
		t.Context(), projectID, repositoryID, approvalRevision, "AGENTS.md", approvalDigest)
	if err != nil || !found {
		t.Fatalf("revocation deleted the record: found=%v err=%v", found, err)
	}
	if revoked.Usable() {
		t.Fatal("a revoked approval is still usable")
	}

	// Revoking twice is a not-found rather than a silent success, so a second
	// revocation cannot be mistaken for the first.
	if err := repositories.RevokeRepositoryInstructionApproval(
		t.Context(), "approval-revoked"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revocation = %v, want ErrNotFound", err)
	}
}

// TestAUDIT011_TheResolverRecordsConsumptionWhileAnswering proves the resolver
// cannot be used to replay by declining to record.
func TestAUDIT011_TheResolverRecordsConsumptionWhileAnswering(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 6140)
	repositoryID := testRepositoryID(t, 6141)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	if _, err := repositories.GrantRepositoryInstructionApproval(t.Context(),
		GrantRepositoryInstructionApproval{
			ID: "approval-resolver", ProjectID: projectID, RepositoryID: repositoryID,
			RepositoryRevision: approvalRevision, InstructionPath: "AGENTS.md",
			ContentSHA256: approvalDigest, Scope: InstructionApprovalSingleUse,
		}); err != nil {
		t.Fatalf("grant: %v", err)
	}

	counter := 0
	resolver := repositories.InstructionApprovalResolverFor(
		projectID, repositoryID, "manifest-resolver")
	resolver.newID = func() (string, error) {
		counter++
		return "consumption-" + strings.Repeat("x", counter), nil
	}

	approved, err := resolver.InstructionApproved(
		t.Context(), approvalRevision, "AGENTS.md", approvalDigest)
	if err != nil || !approved {
		t.Fatalf("first answer = %v, %v; want approved", approved, err)
	}
	// Answering spent it. A caller cannot replay by not recording, because the
	// resolver records as part of answering.
	approved, err = resolver.InstructionApproved(
		t.Context(), approvalRevision, "AGENTS.md", approvalDigest)
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatal("the resolver approved a spent single-use approval a second time")
	}
}

// TestAUDIT011_AGrantIsValidatedBeforeItIsStored keeps an unusable approval
// out of the table rather than discovering it at resolution time.
func TestAUDIT011_AGrantIsValidatedBeforeItIsStored(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 6150)
	repositoryID := testRepositoryID(t, 6151)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	base := GrantRepositoryInstructionApproval{
		ID: "approval-invalid", ProjectID: projectID, RepositoryID: repositoryID,
		RepositoryRevision: approvalRevision, InstructionPath: "AGENTS.md",
		ContentSHA256: approvalDigest, Scope: InstructionApprovalRevision,
	}
	for name, mutate := range map[string]func(*GrantRepositoryInstructionApproval){
		"a short revision":     func(g *GrantRepositoryInstructionApproval) { g.RepositoryRevision = "abc" },
		"a short digest":       func(g *GrantRepositoryInstructionApproval) { g.ContentSHA256 = "abc" },
		"an absolute path":     func(g *GrantRepositoryInstructionApproval) { g.InstructionPath = "/etc/passwd" },
		"an escaping path":     func(g *GrantRepositoryInstructionApproval) { g.InstructionPath = "../outside.md" },
		"an unknown scope":     func(g *GrantRepositoryInstructionApproval) { g.Scope = "always" },
		"no project identity":  func(g *GrantRepositoryInstructionApproval) { g.ProjectID = domain.ProjectID{} },
		"no approval identity": func(g *GrantRepositoryInstructionApproval) { g.ID = "" },
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			input := base
			mutate(&input)
			if _, err := repositories.GrantRepositoryInstructionApproval(t.Context(), input); err == nil {
				t.Fatal("an invalid grant was stored")
			}
		})
	}
}
