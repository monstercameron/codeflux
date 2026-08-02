package storage

import (
	"strings"
	"testing"
)

// TestAUDIT005_DeletingAProjectWithGraphAndVectorDescendantsIsRefused covers
// AUDIT-005, reconciling M03-067.
//
// M03-067 recorded that deletion leaves no unreferenced graph or vector rows.
// The evidence behind it was DefaultDeletionPolicy — a declared type with no
// executor anywhere in the repository — and ValidateNoOrphans, which runs
// PRAGMA foreign_key_check against whatever happens to be in the database. A
// check that passes on an empty database is not evidence about deletion.
//
// This seeds a real graph descendant and a real vector descendant, then runs
// the deletion the schema actually supports and asserts what it does: the
// database refuses it, and no orphan is produced by the refusal.
func TestAUDIT005_DeletingAProjectWithGraphAndVectorDescendantsIsRefused(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 5100)
	repositoryID := testRepositoryID(t, 5101)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	// A real graph descendant. graphs is the root of the graph subtree and the
	// row every other graph table joins back to.
	const graphID = "audit005-graph-0000000000000000000000000000"
	if _, err := repositories.database.sql.ExecContext(ctx,
		`INSERT INTO graphs (id, project_id, repository_id, created_at_unix_micros)
		 VALUES (?, ?, ?, ?)`,
		graphID, projectID.String(), repositoryID.String(), 1,
	); err != nil {
		t.Fatalf("seed graph: %v", err)
	}

	// A real vector descendant, through the supported repository API rather
	// than raw SQL, so the embedding carries its true bindings.
	atomID := testAtomID(t, 5102)
	revision := mustAdmittedAtomDocumentationRevision(t, atomID, 1, testAtomContractHash(t, '5'))
	if _, err := repositories.CreateAtomDocumentationRevision(ctx, CreateAtomDocumentationRevision{
		ProjectID: projectID, Revision: revision,
	}); err != nil {
		t.Fatalf("seed atom documentation revision: %v", err)
	}

	assertNoOrphans(t, repositories, "after seeding")

	// The deletion the schema supports. No migration declares ON DELETE for
	// these relations, so with foreign keys enforced the intended behaviour is
	// restriction, and this is the assertion M03-067 never made.
	_, err := repositories.database.sql.ExecContext(ctx,
		`DELETE FROM projects WHERE id = ?`, projectID.String())
	if err == nil {
		t.Fatal("deleting a project with graph and vector descendants was accepted; " +
			"the schema declares no cascade, so this would strand every descendant")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("the refusal is not a foreign-key constraint: %v", err)
	}

	// The refusal must be atomic. A partially applied delete would leave the
	// orphans the ticket is about.
	assertNoOrphans(t, repositories, "after the refused project deletion")

	var graphs, revisions int
	if err := repositories.database.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM graphs WHERE project_id = ?`, projectID.String(),
	).Scan(&graphs); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM atom_documentation_revisions WHERE project_id = ?`,
		projectID.String(),
	).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if graphs != 1 || revisions != 1 {
		t.Fatalf("the refused deletion still removed rows: %d graphs, %d documentation revisions",
			graphs, revisions)
	}
}

// TestAUDIT005_DeletingAGraphWithRevisionsIsRefused proves the restriction
// holds one level down, where a cascade would be most tempting to assume.
func TestAUDIT005_DeletingAGraphWithRevisionsIsRefused(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 5110)
	repositoryID := testRepositoryID(t, 5111)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	const (
		graphID    = "audit005-graph-1111111111111111111111111111"
		revisionID = "audit005-revision-11111111111111111111111111"
	)
	if _, err := repositories.database.sql.ExecContext(ctx,
		`INSERT INTO graphs (id, project_id, repository_id, created_at_unix_micros)
		 VALUES (?, ?, ?, ?)`,
		graphID, projectID.String(), repositoryID.String(), 1,
	); err != nil {
		t.Fatalf("seed graph: %v", err)
	}
	if _, err := repositories.database.sql.ExecContext(ctx,
		`INSERT INTO graph_revisions
		   (id, graph_id, revision, graph_schema_version, metadata_policy, created_at_unix_micros)
		 VALUES (?, ?, 1, 1, 'typed-fields-only', 1)`,
		revisionID, graphID,
	); err != nil {
		t.Fatalf("seed graph revision: %v", err)
	}

	if _, err := repositories.database.sql.ExecContext(ctx,
		`DELETE FROM graphs WHERE id = ?`, graphID); err == nil {
		t.Fatal("deleting a graph with revisions was accepted, stranding every revision")
	}
	assertNoOrphans(t, repositories, "after the refused graph deletion")
}

// TestAUDIT005_TheOrphanCheckDetectsAnActualOrphan proves ValidateNoOrphans is
// capable of failing.
//
// This is the control M03-067 lacked. ValidateNoOrphans passes trivially on
// any database whose constraints were never violated, so citing a passing run
// as evidence about deletion says nothing until the check is shown to fail on
// a database that does contain an orphan.
func TestAUDIT005_TheOrphanCheckDetectsAnActualOrphan(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 5120)
	repositoryID := testRepositoryID(t, 5121)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	const graphID = "audit005-graph-2222222222222222222222222222"
	if _, err := repositories.database.sql.ExecContext(ctx,
		`INSERT INTO graphs (id, project_id, repository_id, created_at_unix_micros)
		 VALUES (?, ?, ?, ?)`,
		graphID, projectID.String(), repositoryID.String(), 1,
	); err != nil {
		t.Fatalf("seed graph: %v", err)
	}
	assertNoOrphans(t, repositories, "before the orphan is created")

	// Suspend enforcement to manufacture exactly the state a cascade-free
	// deletion would produce if anything ever bypassed the constraint.
	if _, err := repositories.database.sql.ExecContext(ctx,
		`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("suspend foreign keys: %v", err)
	}
	if _, err := repositories.database.sql.ExecContext(ctx,
		`DELETE FROM projects WHERE id = ?`, projectID.String()); err != nil {
		t.Fatalf("delete project with enforcement off: %v", err)
	}
	if _, err := repositories.database.sql.ExecContext(ctx,
		`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("restore foreign keys: %v", err)
	}

	if err := repositories.database.ValidateNoOrphans(ctx); err == nil {
		t.Fatal("ValidateNoOrphans passed on a database holding a graph whose project is gone")
	}
}

func assertNoOrphans(t *testing.T, repositories *Repositories, when string) {
	t.Helper()
	if err := repositories.database.ValidateNoOrphans(t.Context()); err != nil {
		t.Fatalf("orphan rows exist %s: %v", when, err)
	}
}
