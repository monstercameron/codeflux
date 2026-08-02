package atomcatalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

const (
	testRepositoryRevision = "0000000000000000000000000000000000000000"
	expectedEntryCount     = 15
)

func testTime(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
}

func testProject(t *testing.T) domain.ProjectID {
	t.Helper()
	id, err := domain.NewProjectID()
	if err != nil {
		t.Fatalf("new project ID: %v", err)
	}
	return id
}

// recordingWriter captures what seeding attempted to persist, so a test can
// assert on the records themselves rather than only on the returned summary.
type recordingWriter struct {
	inputs []storage.CreateAtomDocumentationRevision
	err    error
	// storedAt overrides the returned creation time, which is how the real
	// repository signals that an identical revision already existed.
	storedAt *time.Time
}

func (writer *recordingWriter) CreateAtomDocumentationRevision(
	_ context.Context,
	input storage.CreateAtomDocumentationRevision,
) (storage.AtomDocumentationRevision, error) {
	if writer.err != nil {
		return storage.AtomDocumentationRevision{}, writer.err
	}
	writer.inputs = append(writer.inputs, input)
	created := input.Revision.CreatedAt
	if writer.storedAt != nil {
		created = *writer.storedAt
	}
	return storage.AtomDocumentationRevision{
		RevisionID: input.Revision.RevisionID,
		AtomID:     input.Revision.AtomID,
		CreatedAt:  created,
	}, nil
}

// The catalog is the control-flow surface the design enumerates. A count that
// silently drifts from the document is the failure this guards: an entry
// dropped in an edit would otherwise disappear from retrieval without anything
// noticing.
func TestControlFlowEntriesCoverTheDeclaredSurface(t *testing.T) {
	entries := ControlFlowEntries()
	if len(entries) != expectedEntryCount {
		t.Fatalf("catalog holds %d entries, expected %d", len(entries), expectedEntryCount)
	}

	seenOrdinal := make(map[int]bool, len(entries))
	seenName := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seenOrdinal[entry.Ordinal] {
			t.Errorf("ordinal %d appears more than once", entry.Ordinal)
		}
		if seenName[entry.Name] {
			t.Errorf("name %q appears more than once", entry.Name)
		}
		seenOrdinal[entry.Ordinal] = true
		seenName[entry.Name] = true
	}
	for ordinal := 1; ordinal <= expectedEntryCount; ordinal++ {
		if !seenOrdinal[ordinal] {
			t.Errorf("ordinal %d is missing, so the table has a gap", ordinal)
		}
	}
}

// ControlFlowEntries must hand back a copy. A caller mutating the package's
// declarations would change what every later retrieval returns.
func TestControlFlowEntriesAreCopied(t *testing.T) {
	first := ControlFlowEntries()
	first[0].Name = "MutatedByCaller"
	if ControlFlowEntries()[0].Name == "MutatedByCaller" {
		t.Fatal("mutating a returned entry changed the package's declarations")
	}
}

// Every field must carry substantive text or an explained absence. A silently
// empty field is never valid, and an entry that produced one would be admitted
// while saying nothing.
func TestEveryDocumentFieldIsSubstantiveOrExplained(t *testing.T) {
	for _, entry := range ControlFlowEntries() {
		document := entry.Document()
		for _, field := range documentFields(document) {
			if field.value.IsEmpty() {
				t.Errorf("entry %d %q: field %s is silently empty",
					entry.Ordinal, entry.Name, field.label)
			}
			if field.value.None && field.value.Reason == "" {
				t.Errorf("entry %d %q: field %s claims absence without a reason",
					entry.Ordinal, entry.Name, field.label)
			}
		}
	}
}

// An entry with an open question must surface it in the documentation a
// retrieval returns. A construct whose semantics are unsettled reading as
// settled is the precise failure the readiness vocabulary exists to prevent.
func TestOpenQuestionsReachTheDocumentation(t *testing.T) {
	for _, entry := range ControlFlowEntries() {
		if entry.OpenQuestion == "" {
			continue
		}
		failure := entry.Document().FailureSemantics
		if len(failure.Items) == 0 {
			t.Fatalf("entry %d %q: no failure semantics to carry its open question",
				entry.Ordinal, entry.Name)
		}
		if !contains(failure.Items[0], entry.OpenQuestion) {
			t.Errorf("entry %d %q: open question is absent from its documentation",
				entry.Ordinal, entry.Name)
		}
	}
}

// Identity derivation must be stable, because seeding is idempotent only if
// re-running resolves to the same atoms. A generated identity would create a
// duplicate atom on every run.
func TestIdentityDerivationIsStableAcrossRuns(t *testing.T) {
	for _, entry := range ControlFlowEntries() {
		first, err := entry.AtomID()
		if err != nil {
			t.Fatalf("entry %d %q: derive atom ID: %v", entry.Ordinal, entry.Name, err)
		}
		second, err := entry.AtomID()
		if err != nil {
			t.Fatalf("entry %d %q: re-derive atom ID: %v", entry.Ordinal, entry.Name, err)
		}
		if first != second {
			t.Errorf("entry %d %q: atom identity is not stable", entry.Ordinal, entry.Name)
		}
	}
}

// Two entries deriving one identity would silently merge two constructs into
// one retrievable atom.
func TestIdentitiesAreDistinctAcrossEntries(t *testing.T) {
	seen := make(map[domain.AtomID]string)
	for _, entry := range ControlFlowEntries() {
		id, err := entry.AtomID()
		if err != nil {
			t.Fatalf("entry %d %q: derive atom ID: %v", entry.Ordinal, entry.Name, err)
		}
		if previous, collided := seen[id]; collided {
			t.Errorf("entries %q and %q derive one atom identity", previous, entry.Name)
		}
		seen[id] = entry.Name
	}
}

// A revision must be complete enough for the storage lane to accept it. The
// repository rejects a missing identity, version, hash, or authoring mode, so
// producing one here means seeding would fail at the last step.
func TestRevisionCarriesEverySeedingRequirement(t *testing.T) {
	created := testTime(t)
	for _, entry := range ControlFlowEntries() {
		revision, err := entry.Revision(testRepositoryRevision, created)
		if err != nil {
			t.Fatalf("entry %d %q: derive revision: %v", entry.Ordinal, entry.Name, err)
		}
		switch {
		case revision.RevisionID.IsZero():
			t.Errorf("entry %q: revision identity is empty", entry.Name)
		case revision.AtomID.IsZero():
			t.Errorf("entry %q: atom identity is empty", entry.Name)
		case revision.AtomVersion.IsZero():
			t.Errorf("entry %q: atom version is empty", entry.Name)
		case revision.AtomVersion.AtomID() != revision.AtomID:
			t.Errorf("entry %q: atom version belongs to a different atom", entry.Name)
		case revision.SchemaVersion != atomdoc.SchemaVersion:
			t.Errorf("entry %q: schema version %d is not the supported one",
				entry.Name, revision.SchemaVersion)
		case revision.SourceCommentHash.IsZero():
			t.Errorf("entry %q: source comment hash is empty", entry.Name)
		case revision.NormalizedInputHash.IsZero():
			t.Errorf("entry %q: normalized input hash is empty", entry.Name)
		case revision.ContractHash.IsZero():
			t.Errorf("entry %q: contract hash is empty", entry.Name)
		}
		// Tier 1 graph-native has no Go implementation of its own, so its
		// documentation is authored as a structured record. Extracting it from
		// a source comment would claim a declaration that does not exist.
		if revision.Authoring != atomdoc.AuthoringSQLiteAuthored {
			t.Errorf("entry %q: authoring is %q, expected the SQLite-authored mode",
				entry.Name, revision.Authoring)
		}
	}
}

// Re-deriving a revision from unchanged entries must produce the same
// content-addressed identity, which is what makes seeding a no-op rather than
// a duplicate write.
func TestRevisionIdentityIsContentAddressed(t *testing.T) {
	created := testTime(t)
	entry := ControlFlowEntries()[0]

	first, err := entry.Revision(testRepositoryRevision, created)
	if err != nil {
		t.Fatalf("derive revision: %v", err)
	}
	// A different creation time must not change identity: the revision is
	// addressed by content, and when it was written is not content.
	later, err := entry.Revision(testRepositoryRevision, created.Add(72*time.Hour))
	if err != nil {
		t.Fatalf("re-derive revision: %v", err)
	}
	if first.RevisionID != later.RevisionID {
		t.Error("revision identity changed with the creation time, so re-seeding would duplicate")
	}

	// Changed content must change identity, or an edit would silently reuse a
	// revision that describes something else.
	edited := entry
	edited.Purpose = entry.Purpose + " And one more sentence that changes the content."
	changed, err := edited.Revision(testRepositoryRevision, created)
	if err != nil {
		t.Fatalf("derive edited revision: %v", err)
	}
	if changed.RevisionID == first.RevisionID {
		t.Error("edited content kept the same revision identity")
	}
}

func TestSeedWritesEveryEntry(t *testing.T) {
	writer := &recordingWriter{}
	result, err := SeedControlFlowCatalog(
		context.Background(), writer, testProject(t), testRepositoryRevision, testTime(t))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if len(writer.inputs) != expectedEntryCount {
		t.Fatalf("seeding persisted %d revisions, expected %d",
			len(writer.inputs), expectedEntryCount)
	}
	if len(result.Written) != expectedEntryCount {
		t.Fatalf("seeding reported %d written, expected %d",
			len(result.Written), expectedEntryCount)
	}
	if len(result.Unchanged) != 0 {
		t.Errorf("a first seeding reported %d unchanged entries", len(result.Unchanged))
	}
}

// A repeat run against a store that already holds the entries must report them
// as unchanged rather than as fresh writes, or an operator cannot tell a
// no-op from a real seeding.
func TestSeedReportsAlreadyPresentEntriesAsUnchanged(t *testing.T) {
	earlier := testTime(t).Add(-24 * time.Hour)
	writer := &recordingWriter{storedAt: &earlier}
	result, err := SeedControlFlowCatalog(
		context.Background(), writer, testProject(t), testRepositoryRevision, testTime(t))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if len(result.Unchanged) != expectedEntryCount {
		t.Fatalf("seeding reported %d unchanged, expected %d",
			len(result.Unchanged), expectedEntryCount)
	}
	if len(result.Written) != 0 {
		t.Errorf("a repeat seeding reported %d fresh writes", len(result.Written))
	}
}

func TestSeedRejectsIncompleteInput(t *testing.T) {
	valid := testTime(t)
	project := testProject(t)

	cases := []struct {
		name     string
		writer   AtomDocumentationWriter
		project  domain.ProjectID
		revision string
		created  time.Time
	}{
		{"no writer", nil, project, testRepositoryRevision, valid},
		{"no project", &recordingWriter{}, domain.ProjectID{}, testRepositoryRevision, valid},
		{"no repository revision", &recordingWriter{}, project, "", valid},
		{"zero time", &recordingWriter{}, project, testRepositoryRevision, time.Time{}},
		{"non-UTC time", &recordingWriter{}, project, testRepositoryRevision,
			valid.In(time.FixedZone("elsewhere", 3600))},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := SeedControlFlowCatalog(
				context.Background(), testCase.writer, testCase.project,
				testCase.revision, testCase.created)
			if err == nil {
				t.Fatal("incomplete input was accepted")
			}
		})
	}
}

// A storage failure must stop the run and name the entry, rather than
// returning a partial summary a caller would read as success.
func TestSeedStopsAndNamesTheFailingEntry(t *testing.T) {
	writer := &recordingWriter{err: errors.New("database is closed")}
	result, err := SeedControlFlowCatalog(
		context.Background(), writer, testProject(t), testRepositoryRevision, testTime(t))
	if err == nil {
		t.Fatal("a storage failure was reported as success")
	}
	if !contains(err.Error(), ControlFlowEntries()[0].Name) {
		t.Errorf("failure did not name the entry it stopped on: %v", err)
	}
	if len(result.Written) != 0 {
		t.Errorf("a failed run reported %d written entries", len(result.Written))
	}
}

// Every entry must record its provenance, so a retrieval result cannot be
// mistaken for an authorised kernel declaration.
func TestEveryEntryCarriesItsProposalProvenance(t *testing.T) {
	for _, entry := range ControlFlowEntries() {
		document := entry.Document()
		if !contains(document.FailureSemantics.Items[0], ProposalSource) {
			t.Errorf("entry %d %q: failure semantics omit the proposal source",
				entry.Ordinal, entry.Name)
		}
	}
}

type labelledField struct {
	label string
	value atomdoc.Field
}

func documentFields(document atomdoc.Document) []labelledField {
	return []labelledField{
		{"Purpose", document.Purpose},
		{"Use when", document.UseWhen},
		{"Do not use when", document.DoNotUseWhen},
		{"Semantics", document.Semantics},
		{"Inputs", document.Inputs},
		{"Outputs", document.Outputs},
		{"Preconditions", document.Preconditions},
		{"Postconditions", document.Postconditions},
		{"Effects", document.Effects},
		{"Failure semantics", document.FailureSemantics},
		{"Determinism", document.Determinism},
		{"Idempotency and retry", document.IdempotencyAndRetry},
		{"Reconciliation and compensation", document.ReconciliationAndCompensation},
		{"Security and privacy", document.SecurityAndPrivacy},
		{"Dependencies and bindings", document.DependenciesAndBindings},
		{"Complexity and limits", document.ComplexityAndLimits},
		{"Examples", document.Examples},
		{"Verification", document.Verification},
		{"Retrieval concepts", document.RetrievalConcepts},
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for start := 0; start+len(needle) <= len(haystack); start++ {
		if haystack[start:start+len(needle)] == needle {
			return start
		}
	}
	return -1
}
