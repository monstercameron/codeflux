package storage

import (
	"testing"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/atomname"
	"codeflux.dev/codeflux/internal/domain"
)

// TestListAtomNamesByProjectReadsEveryValidNameAcrossAtoms proves the M21-167
// wiring seam: unlike GetCurrentAtomName/ListAtomNameRevisions (both of which
// require the caller to already know one atom's identity),
// ListAtomNamesByProject returns every currently valid canonical name across
// every atom in a project, scoped only by the project_id column
// atom_names already carries -- so a search over "every atom name in this
// project" needs no new schema.
func TestListAtomNamesByProjectReadsEveryValidNameAcrossAtoms(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4100)
	repositoryID := testRepositoryID(t, 4101)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	otherProjectID := testProjectID(t, 4110)
	otherRepositoryID := testRepositoryID(t, 4111)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)

	firstAtomID := testAtomID(t, 4102)
	firstAtomVersion, err := atomdoc.NewAtomVersionID(firstAtomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	firstScope := mustNamingScope(t, projectID, "reservations")
	firstCanonical := mustCanonicalName(t, "ReserveAccountFundsUntilAuthorizationExpires")
	firstRecord := mustAtomNameRecord(t, firstAtomID, firstAtomVersion, firstScope, firstCanonical, 1, atomname.AtomNameRevisionID{}, fakeNow(t, repositories))
	if _, err := repositories.CreateAtomNameRevision(ctx, firstRecord); err != nil {
		t.Fatal(err)
	}

	secondAtomID := testAtomID(t, 4103)
	secondAtomVersion, err := atomdoc.NewAtomVersionID(secondAtomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	secondScope := mustNamingScope(t, projectID, "checkout")
	secondCanonical := mustCanonicalName(t, "ReleaseWidgetInventoryHold")
	secondRecord := mustAtomNameRecord(t, secondAtomID, secondAtomVersion, secondScope, secondCanonical, 1, atomname.AtomNameRevisionID{}, fakeNow(t, repositories))
	if _, err := repositories.CreateAtomNameRevision(ctx, secondRecord); err != nil {
		t.Fatal(err)
	}

	// An atom named in a DIFFERENT project must never appear.
	otherAtomID := testAtomID(t, 4112)
	otherAtomVersion, err := atomdoc.NewAtomVersionID(otherAtomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	otherScope := mustNamingScope(t, otherProjectID, "reservations")
	otherCanonical := mustCanonicalName(t, "ReserveAccountFundsUntilAuthorizationExpires")
	otherRecord := mustAtomNameRecord(t, otherAtomID, otherAtomVersion, otherScope, otherCanonical, 1, atomname.AtomNameRevisionID{}, fakeNow(t, repositories))
	if _, err := repositories.CreateAtomNameRevision(ctx, otherRecord); err != nil {
		t.Fatal(err)
	}

	revisions, err := repositories.ListAtomNamesByProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 {
		t.Fatalf("revisions = %#v, want exactly the 2 names scoped to projectID", revisions)
	}
	seen := map[domain.AtomID]bool{}
	for _, revision := range revisions {
		if !revision.Valid {
			t.Fatalf("revision %#v must be valid", revision)
		}
		seen[revision.AtomID] = true
		if revision.AtomID == otherAtomID {
			t.Fatal("a name scoped to a different project must never appear")
		}
	}
	if !seen[firstAtomID] || !seen[secondAtomID] {
		t.Fatalf("seen = %#v, want both in-project atoms present", seen)
	}
}

// TestListActiveAtomNameAliasesByProjectExcludesInvalidatedAndOtherProjects
// proves the alias-side companion to ListAtomNamesByProject: only active
// (non-invalidated) aliases within the given project are returned.
func TestListActiveAtomNameAliasesByProjectExcludesInvalidatedAndOtherProjects(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4200)
	repositoryID := testRepositoryID(t, 4201)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	atomID := testAtomID(t, 4202)
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveAccountFundsUntilAuthorizationExpires")
	record := mustAtomNameRecord(t, atomID, atomVersion, scope, canonical, 1, atomname.AtomNameRevisionID{}, fakeNow(t, repositories))
	if _, err := repositories.CreateAtomNameRevision(ctx, record); err != nil {
		t.Fatal(err)
	}

	activeAlias, err := atomname.NewReviewedManualAlias(atomID, "hold funds pending authorization", fakeNow(t, repositories))
	if err != nil {
		t.Fatal(err)
	}
	persistedActive, err := repositories.RecordAtomNameAlias(ctx, activeAlias)
	if err != nil {
		t.Fatal(err)
	}

	invalidatedAlias, err := atomname.NewReviewedManualAlias(atomID, "lock funds for authorization", fakeNow(t, repositories))
	if err != nil {
		t.Fatal(err)
	}
	persistedInvalidated, err := repositories.RecordAtomNameAlias(ctx, invalidatedAlias)
	if err != nil {
		t.Fatal(err)
	}
	if err := repositories.InvalidateAtomNameAlias(ctx, persistedInvalidated.ID, "superseded by a clearer alias"); err != nil {
		t.Fatal(err)
	}

	aliases, err := repositories.ListActiveAtomNameAliasesByProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 1 || aliases[0].ID != persistedActive.ID {
		t.Fatalf("aliases = %#v, want exactly the one still-active alias %v", aliases, persistedActive.ID)
	}
}
