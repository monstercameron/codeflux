package storage

import (
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/atomname"
	"codeflux.dev/codeflux/internal/domain"
)

// fakeNow returns the repository's own (fake, monotonically increasing test)
// clock reading rather than time.Now(), so a value used to construct an
// atomname record (whose ActiveFrom/CreatedAt a later repository call does
// not independently re-stamp) can never race ahead of or fall behind a
// later timestamp openTestRepositories' shared fake clock produces.
func fakeNow(t *testing.T, repositories *Repositories) time.Time {
	t.Helper()
	now, _ := repositories.timestamp()
	return now
}

func mustNamingScope(t *testing.T, projectID domain.ProjectID, semanticScope string) atomname.NamingScope {
	t.Helper()
	scope, err := atomname.NewNamingScope(projectID, semanticScope)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func mustCanonicalName(t *testing.T, raw string) atomname.CanonicalName {
	t.Helper()
	name, err := atomname.NewCanonicalName(raw)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func mustNamingRationale(t *testing.T) atomname.NamingRationale {
	t.Helper()
	rationale, err := atomname.NewNamingRationale(
		"ReleaseWidgetInventoryHold releases a hold rather than reserving one",
		"Reserve creates a hold; this atom never mutates existing inventory holds",
	)
	if err != nil {
		t.Fatal(err)
	}
	return rationale
}

func mustAtomNameRecord(
	t *testing.T,
	atomID domain.AtomID,
	atomVersion atomdoc.AtomVersionID,
	scope atomname.NamingScope,
	canonical atomname.CanonicalName,
	sequence uint32,
	supersedes atomname.AtomNameRevisionID,
	createdAt time.Time,
) atomname.AtomNameRecord {
	t.Helper()
	record, err := atomname.NewAtomNameRecord(atomID, atomVersion, scope, canonical, mustNamingRationale(t), sequence, supersedes, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestCreateAtomNameRevisionPersistsIdentityIdempotentlyAndSupersedes(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4000)
	repositoryID := testRepositoryID(t, 4001)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 4002)
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	firstRecord := mustAtomNameRecord(t, atomID, atomVersion, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())

	first, err := repositories.CreateAtomNameRevision(ctx, firstRecord)
	if err != nil {
		t.Fatal(err)
	}
	if first.RevisionID != firstRecord.RevisionID || first.AtomID != atomID || first.AtomVersion != atomVersion ||
		!first.Valid || first.RevisionSequence != 1 || !first.SupersedesRevisionID.IsZero() ||
		first.CanonicalName != canonical || first.DisplayName.String() != atomname.DeriveDisplayName(canonical).String() ||
		first.NormalizedPhrase.String() != atomname.DeriveNormalizedPhrase(canonical).String() {
		t.Fatalf("first revision = %#v", first)
	}

	retried, err := repositories.CreateAtomNameRevision(ctx, firstRecord)
	if err != nil {
		t.Fatal(err)
	}
	if retried.RevisionID != first.RevisionID {
		t.Fatalf("idempotent retry = %#v, want %#v", retried, first)
	}

	// Semantic-preserving rename: new canonical name, same atom + version,
	// sequence 2 supersedes sequence 1.
	renamed := mustCanonicalName(t, "ReserveWidgetInventoryUntilCartExpires")
	secondRecord := mustAtomNameRecord(t, atomID, atomVersion, scope, renamed, 2, first.RevisionID, time.Now().UTC())
	second, err := repositories.CreateAtomNameRevision(ctx, secondRecord)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Valid || second.RevisionSequence != 2 || second.SupersedesRevisionID != first.RevisionID {
		t.Fatalf("second revision = %#v", second)
	}

	// The superseded revision must now be invalid, atomically with the swap.
	priorAfterSupersede, err := repositories.GetAtomNameRevision(ctx, first.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if priorAfterSupersede.Valid {
		t.Fatal("superseded atom name revision is still marked valid")
	}

	current, err := repositories.GetCurrentAtomName(ctx, atomVersion)
	if err != nil {
		t.Fatal(err)
	}
	if current.RevisionID != second.RevisionID {
		t.Fatalf("current atom name = %#v, want revision %s", current, second.RevisionID)
	}

	revisions, err := repositories.ListAtomNameRevisions(ctx, atomID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[0].RevisionID != first.RevisionID || revisions[1].RevisionID != second.RevisionID {
		t.Fatalf("list revisions = %#v", revisions)
	}
}

func TestCreateAtomNameRevisionRejectsActiveCanonicalNameCollisionWithinScope(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4010)
	repositoryID := testRepositoryID(t, 4011)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")

	atomIDA := testAtomID(t, 4012)
	atomVersionA, err := atomdoc.NewAtomVersionID(atomIDA, 1)
	if err != nil {
		t.Fatal(err)
	}
	recordA := mustAtomNameRecord(t, atomIDA, atomVersionA, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, recordA); err != nil {
		t.Fatal(err)
	}

	atomIDB := testAtomID(t, 4013)
	atomVersionB, err := atomdoc.NewAtomVersionID(atomIDB, 1)
	if err != nil {
		t.Fatal(err)
	}
	recordB := mustAtomNameRecord(t, atomIDB, atomVersionB, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, recordB); !errors.Is(err, ErrConstraint) {
		t.Fatalf("colliding active canonical name error = %v, want constraint", err)
	}

	// A different semantic scope in the same project must not collide
	// (mirrors atomname.DetectCanonicalNameCollision's scope key).
	otherScope := mustNamingScope(t, projectID, "checkout")
	recordC := mustAtomNameRecord(t, atomIDB, atomVersionB, otherScope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, recordC); err != nil {
		t.Fatalf("cross-scope name should not collide: %v", err)
	}

	// A different project must not collide either (project isolation,
	// M21-171-style).
	otherProjectID := testProjectID(t, 4020)
	otherRepositoryID := testRepositoryID(t, 4021)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)
	otherProjectScope := mustNamingScope(t, otherProjectID, "reservations")
	atomIDD := testAtomID(t, 4022)
	atomVersionD, err := atomdoc.NewAtomVersionID(atomIDD, 1)
	if err != nil {
		t.Fatal(err)
	}
	recordD := mustAtomNameRecord(t, atomIDD, atomVersionD, otherProjectScope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, recordD); err != nil {
		t.Fatalf("cross-project name should not collide: %v", err)
	}
}

func TestCreateAtomNameRevisionAllowsSameAtomDifferentVersionsToShareAName(t *testing.T) {
	// DetectCanonicalNameCollision excludes the candidate's own AtomID
	// (atom-level, not atom+version-level): two valid names for two
	// different versions of the SAME atom sharing a phrase is not a
	// collision. Only a different atom's name is.
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4030)
	repositoryID := testRepositoryID(t, 4031)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	atomID := testAtomID(t, 4032)

	atomVersion1, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	record1 := mustAtomNameRecord(t, atomID, atomVersion1, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, record1); err != nil {
		t.Fatal(err)
	}

	atomVersion2, err := atomdoc.NewAtomVersionID(atomID, 2)
	if err != nil {
		t.Fatal(err)
	}
	record2 := mustAtomNameRecord(t, atomID, atomVersion2, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, record2); err != nil {
		t.Fatalf("same atom, different version, same name should not collide: %v", err)
	}
}

func TestCreateAtomNameRevisionRejectsCrossProjectSameAtom(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4040)
	repositoryID := testRepositoryID(t, 4041)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	otherProjectID := testProjectID(t, 4042)
	otherRepositoryID := testRepositoryID(t, 4043)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)

	atomID := testAtomID(t, 4044)
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	record := mustAtomNameRecord(t, atomID, atomVersion, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, record); err != nil {
		t.Fatal(err)
	}

	otherScope := mustNamingScope(t, otherProjectID, "reservations")
	otherName := mustCanonicalName(t, "ReserveWidgetInventoryUntilCartExpires")
	otherAtomVersion, err := atomdoc.NewAtomVersionID(atomID, 2)
	if err != nil {
		t.Fatal(err)
	}
	otherRecord := mustAtomNameRecord(t, atomID, otherAtomVersion, otherScope, otherName, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, otherRecord); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project same-atom naming error = %v, want constraint", err)
	}
}

func TestAtomNameRevisionRawSQLAttacks(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4050)
	repositoryID := testRepositoryID(t, 4051)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 4052)
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	record := mustAtomNameRecord(t, atomID, atomVersion, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	revision, err := repositories.CreateAtomNameRevision(ctx, record)
	if err != nil {
		t.Fatal(err)
	}

	// Attack 1: mutate an immutable content column.
	_, err = repositories.database.sql.ExecContext(
		ctx, `UPDATE atom_names SET canonical_name = 'TamperedName' WHERE id = ?`, revision.RevisionID.String(),
	)
	if !errors.Is(classify("attack: mutate atom name content", err), ErrConstraint) {
		t.Fatalf("content mutation error = %v, want constraint", err)
	}

	// Attack 2: revalidate an invalid row (one-way trigger). First force it
	// invalid via a legitimate supersede, then try to flip it back.
	renamed := mustCanonicalName(t, "ReserveWidgetInventoryUntilCartExpires")
	second := mustAtomNameRecord(t, atomID, atomVersion, scope, renamed, 2, revision.RevisionID, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, second); err != nil {
		t.Fatal(err)
	}
	_, err = repositories.database.sql.ExecContext(
		ctx, `UPDATE atom_names SET valid = 1 WHERE id = ?`, revision.RevisionID.String(),
	)
	if !errors.Is(classify("attack: revalidate superseded atom name", err), ErrConstraint) {
		t.Fatalf("revalidation error = %v, want constraint", err)
	}

	// Attack 3: delete an immutable row.
	_, err = repositories.database.sql.ExecContext(ctx, `DELETE FROM atom_names WHERE id = ?`, revision.RevisionID.String())
	if !errors.Is(classify("attack: delete atom name revision", err), ErrConstraint) {
		t.Fatalf("delete error = %v, want constraint", err)
	}

	// Attack 4: insert a second valid row for the same atom+version,
	// bypassing CreateAtomNameRevision's transactional supersede (partial
	// unique index).
	forgedID, err := atomname.ParseAtomNameRevisionID("anr_" + repeatHex('7', 64))
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO atom_names (
			id, atom_id, atom_version_number, project_id, semantic_scope, schema_version,
			canonical_name, display_name, normalized_phrase,
			rationale_nearest_alternative, rationale_distinguishing_qualifier,
			revision_sequence, supersedes_revision_id, valid, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 1, 1)`,
		forgedID.String(), atomID, atomVersion.Number(), projectID, "reservations", atomname.SchemaVersion,
		"ForgedDuplicateValidName", "Forged Duplicate Valid Name", "forged duplicate valid name",
		"nearest confusing alternative words", "distinguishing qualifier words",
		1,
	)
	if !errors.Is(repositoryWriteError("attack: duplicate valid atom name per atom version", err), ErrConflict) {
		t.Fatalf("duplicate valid name error = %v, want conflict", err)
	}
}

func repeatHex(char byte, count int) string {
	buf := make([]byte, count)
	for i := range buf {
		buf[i] = char
	}
	return string(buf)
}

func TestRecordAtomNameAliasPreservesRenamedCanonicalNameImmutably(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4060)
	repositoryID := testRepositoryID(t, 4061)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 4062)
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := mustNamingScope(t, projectID, "reservations")
	oldCanonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	first := mustAtomNameRecord(t, atomID, atomVersion, scope, oldCanonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, first); err != nil {
		t.Fatal(err)
	}

	newCanonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCartExpires")
	proposal := atomname.RenameProposal{
		OldName: oldCanonical, NewName: newCanonical,
		OldContractHash: testAtomContractHash(t, 'a'), NewContractHash: testAtomContractHash(t, 'a'),
	}
	classification, err := atomname.ClassifyRename(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if classification != atomname.RenameClassificationSemanticPreserving {
		t.Fatalf("classification = %s, want semantic-preserving", classification)
	}
	acceptance, err := atomname.NewRenameAcceptance("reviewer-1")
	if err != nil {
		t.Fatal(err)
	}
	// Use the repository's own (fake, test) clock rather than time.Now() so
	// this alias's ActiveFrom can never race ahead of a later
	// InvalidateAtomNameAlias call's active_until, which is stamped from the
	// same clock.
	occurredAt := fakeNow(t, repositories)
	outcome, err := atomname.AuthorizeCanonicalRename(proposal, acceptance, atomVersion, atomVersion, occurredAt)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.CreatedAlias == nil {
		t.Fatal("expected AuthorizeCanonicalRename to create a prior-name alias")
	}

	secondRecord := mustAtomNameRecord(t, atomID, atomVersion, scope, newCanonical, 2, first.RevisionID, occurredAt)
	if _, err := repositories.CreateAtomNameRevision(ctx, secondRecord); err != nil {
		t.Fatal(err)
	}
	alias, err := repositories.RecordAtomNameAlias(ctx, *outcome.CreatedAlias)
	if err != nil {
		t.Fatal(err)
	}
	if alias.AtomID != atomID || alias.ProjectID != projectID || alias.AliasText != oldCanonical.String() ||
		alias.Source != atomname.AtomNameAliasSourceRename || alias.ActiveUntil != nil {
		t.Fatalf("recorded alias = %#v", alias)
	}

	active, err := repositories.ListActiveAtomNameAliases(ctx, atomID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != alias.ID {
		t.Fatalf("active aliases = %#v", active)
	}

	// The old name still resolves to the atom via the alias, and the alias
	// survives invalidation as immutable lineage (M21-156), it merely
	// leaves active candidate generation.
	if err := repositories.InvalidateAtomNameAlias(ctx, alias.ID, "superseded by a later rename"); err != nil {
		t.Fatal(err)
	}
	activeAfter, err := repositories.ListActiveAtomNameAliases(ctx, atomID)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeAfter) != 0 {
		t.Fatalf("active aliases after invalidation = %#v, want none", activeAfter)
	}
	stillPresent, found, err := findAtomNameAlias(ctx, repositories.database.sql, alias.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found || stillPresent.AliasText != oldCanonical.String() {
		t.Fatalf("invalidated alias lineage = %#v, found=%v", stillPresent, found)
	}
}

func TestRecordAtomNameAliasRejectsRenameSourceNotMatchingRecordedCanonicalName(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4070)
	repositoryID := testRepositoryID(t, 4071)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 4072)
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	record := mustAtomNameRecord(t, atomID, atomVersion, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, record); err != nil {
		t.Fatal(err)
	}

	fabricated, err := atomname.NewAliasFromPriorCanonicalName(atomID, mustCanonicalName(t, "SomeNameThisAtomNeverHeld"), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.RecordAtomNameAlias(ctx, fabricated); !errors.Is(err, ErrConstraint) {
		t.Fatalf("fabricated rename alias error = %v, want constraint", err)
	}
}

func TestRecordAtomNameAliasRejectsUnnamedAtom(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	atomID := testAtomID(t, 4080)
	manual, err := atomname.NewReviewedManualAlias(atomID, "widget hold", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.RecordAtomNameAlias(ctx, manual); !errors.Is(err, ErrConflict) {
		t.Fatalf("alias for unnamed atom error = %v, want conflict", err)
	}
}

func TestAtomNameAliasRawSQLAttacks(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 4090)
	repositoryID := testRepositoryID(t, 4091)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	atomID := testAtomID(t, 4092)
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope := mustNamingScope(t, projectID, "reservations")
	canonical := mustCanonicalName(t, "ReserveWidgetInventoryUntilCheckoutExpires")
	record := mustAtomNameRecord(t, atomID, atomVersion, scope, canonical, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if _, err := repositories.CreateAtomNameRevision(ctx, record); err != nil {
		t.Fatal(err)
	}
	manual, err := atomname.NewReviewedManualAlias(atomID, "widget hold", fakeNow(t, repositories))
	if err != nil {
		t.Fatal(err)
	}
	alias, err := repositories.RecordAtomNameAlias(ctx, manual)
	if err != nil {
		t.Fatal(err)
	}

	// Attack 1: reword an immutable alias.
	_, err = repositories.database.sql.ExecContext(
		ctx, `UPDATE atom_name_aliases SET alias_text = 'tampered' WHERE id = ?`, alias.ID,
	)
	if !errors.Is(classify("attack: reword atom name alias", err), ErrConstraint) {
		t.Fatalf("reword error = %v, want constraint", err)
	}

	// Attack 2: delete an alias.
	_, err = repositories.database.sql.ExecContext(ctx, `DELETE FROM atom_name_aliases WHERE id = ?`, alias.ID)
	if !errors.Is(classify("attack: delete atom name alias", err), ErrConstraint) {
		t.Fatalf("delete error = %v, want constraint", err)
	}

	// Attack 3: reopen a closed active interval.
	if err := repositories.InvalidateAtomNameAlias(ctx, alias.ID, "no longer useful"); err != nil {
		t.Fatal(err)
	}
	_, err = repositories.database.sql.ExecContext(
		ctx, `UPDATE atom_name_aliases SET active_until_unix_micros = NULL, invalidated_reason = NULL WHERE id = ?`, alias.ID,
	)
	if !errors.Is(classify("attack: reopen closed atom name alias interval", err), ErrConstraint) {
		t.Fatalf("reopen error = %v, want constraint", err)
	}

	// Attack 4: insert an alias in a project the atom was never named
	// under.
	otherProjectID := testProjectID(t, 4100)
	otherRepositoryID := testRepositoryID(t, 4101)
	mustCreateProjectRepository(t, repositories, otherProjectID, otherRepositoryID)
	_, err = repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO atom_name_aliases (id, atom_id, project_id, alias_text, normalized_alias_text, source, active_from_unix_micros)
		 VALUES (?, ?, ?, ?, ?, 'reviewed-manual-entry', 1)`,
		repeatHex('4', 64), atomID, otherProjectID, "cross project alias", "cross project alias",
	)
	if !errors.Is(classify("attack: cross-project atom name alias", err), ErrConstraint) {
		t.Fatalf("cross-project alias error = %v, want constraint", err)
	}
}
