package retrieval

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/atomname"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
)

// newTestRepositories opens a real, temporary, file-backed SQLite database
// and applies every migration, including migrations/000028 (M21-137's
// multi-channel table and M21-076's fallback table), matching this
// package's requirement to test against real SQLite rather than a mock.
func newTestRepositories(t *testing.T) *storage.Repositories {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "retrieval-test.sqlite")
	database, err := storage.Open(ctx, storage.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := database.Close(closeCtx); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if _, err := database.Migrate(ctx, storage.MigrationOptions{
		ApplicationVersion: "retrieval-test",
		BackupDirectory:    filepath.Join(t.TempDir(), "backups"),
	}); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	return repositories
}

func mustCreateProjectAndRepository(t *testing.T, repositories *storage.Repositories) (domain.ProjectID, domain.RepositoryID) {
	t.Helper()
	ctx := context.Background()
	projectID, err := domain.NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{ID: projectID, Name: "Fixture project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: "/fixture/" + repositoryID.String(),
		GitIdentity:   "git-" + repositoryID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	return projectID, repositoryID
}

func mustBuildTaskFingerprint(
	t *testing.T,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	requiredAssurance domain.AssuranceLevel,
) fingerprint.ExactFingerprint {
	t.Helper()
	exact, err := fingerprint.BuildExactFingerprint(fingerprint.ExactFingerprintInput{
		Project:           projectID,
		Repository:        repositoryID,
		BaseRevision:      domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
		TaskClass:         fingerprint.TaskClassBugFix,
		Risk:              domain.RiskLevelRoutine,
		RequiredAssurance: requiredAssurance,
	})
	if err != nil {
		t.Fatal(err)
	}
	return exact
}

func mustCreateRepositoryFactArtifact(
	t *testing.T,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	statement string,
) (domain.MemoryArtifactID, domain.MemoryArtifactRevisionID) {
	t.Helper()
	ctx := context.Background()
	artifactID, err := domain.NewMemoryArtifactID()
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := domain.NewMemoryArtifactRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	content, err := domain.NewRepositoryFactMemoryContent(domain.RepositoryFactContent{
		Repository: repositoryID, Category: domain.RepositoryFactCategoryTestCommand,
		Statement: statement, Binding: domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryArtifact(ctx, storage.CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionID, ProjectID: projectID,
		Content: content, IdempotencyKey: revisionID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	return artifactID, revisionID
}

func mustCreateExecutionRecipeArtifact(
	t *testing.T,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
) (domain.MemoryArtifactID, domain.MemoryArtifactRevisionID) {
	t.Helper()
	ctx := context.Background()
	artifactID, err := domain.NewMemoryArtifactID()
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := domain.NewMemoryArtifactRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	content, err := domain.NewExecutionRecipeMemoryContent(domain.ExecutionRecipeContent{
		Repository: repositoryID, ApplicabilityStatement: "reuse for a similarly-scoped bug fix",
		PlanSummary: "apply the same patch shape", RequiredValidationProfile: "standard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryArtifact(ctx, storage.CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionID, ProjectID: projectID,
		Content: content, IdempotencyKey: revisionID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	return artifactID, revisionID
}

// mustCreateExecutableAtomReferenceArtifact stores an executable-atom-
// reference memory artifact bound to repositoryID and atomID, for
// discoverByAtomName's M21-167/M21-168 fixtures.
func mustCreateExecutableAtomReferenceArtifact(
	t *testing.T,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	atomID domain.AtomID,
	canonicalName string,
) (domain.MemoryArtifactID, domain.MemoryArtifactRevisionID) {
	t.Helper()
	ctx := context.Background()
	artifactID, err := domain.NewMemoryArtifactID()
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := domain.NewMemoryArtifactRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	content, err := domain.NewExecutableAtomReferenceMemoryContent(domain.ExecutableAtomReferenceContent{
		Repository: repositoryID, Atom: atomID, CanonicalName: canonicalName,
		Binding: domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryArtifact(ctx, storage.CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionID, ProjectID: projectID,
		Content: content, IdempotencyKey: revisionID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	return artifactID, revisionID
}

// mustCanonicalNameFixture constructs a validated atomname.CanonicalName.
func mustCanonicalNameFixture(t *testing.T, raw string) atomname.CanonicalName {
	t.Helper()
	name, err := atomname.NewCanonicalName(raw)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

// mustCreateAtomNameFixture persists atomID's first (and only) naming
// revision, scoped to projectID, with the given canonical name.
func mustCreateAtomNameFixture(
	t *testing.T,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	atomID domain.AtomID,
	canonical atomname.CanonicalName,
) {
	t.Helper()
	ctx := context.Background()
	atomVersion, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := atomname.NewNamingScope(projectID, "retrieval-fixture")
	if err != nil {
		t.Fatal(err)
	}
	rationale, err := atomname.NewNamingRationale(
		"a differently scoped atom performing a similar-sounding operation",
		"this qualifier distinguishes the exact fixture atom under test",
	)
	if err != nil {
		t.Fatal(err)
	}
	record, err := atomname.NewAtomNameRecord(atomID, atomVersion, scope, canonical, rationale, 1, atomname.AtomNameRevisionID{}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateAtomNameRevision(ctx, record); err != nil {
		t.Fatal(err)
	}
}

// mustCreateAtomAliasFixture records one active, reviewed manual alias for
// atomID.
func mustCreateAtomAliasFixture(
	t *testing.T,
	repositories *storage.Repositories,
	atomID domain.AtomID,
	aliasText string,
) storage.AtomNameAlias {
	t.Helper()
	ctx := context.Background()
	alias, err := atomname.NewReviewedManualAlias(atomID, aliasText, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repositories.RecordAtomNameAlias(ctx, alias)
	if err != nil {
		t.Fatal(err)
	}
	return persisted
}

// mustCreateRepositoryMatchPredicate stores a repository-match applicability
// predicate for revisionID, encoded in this package's own canonical JSON
// shape (see applicability.go), pinned to the same fingerprint schema
// version the task under test uses.
func mustCreateRepositoryMatchPredicate(
	t *testing.T,
	repositories *storage.Repositories,
	revisionID domain.MemoryArtifactRevisionID,
	schemaVersion uint32,
	requiredRepository domain.RepositoryID,
) {
	t.Helper()
	if _, err := repositories.CreateMemoryArtifactApplicabilityPredicate(context.Background(), storage.CreateMemoryArtifactApplicabilityPredicate{
		RevisionID: revisionID, FingerprintSchemaVersion: int(schemaVersion),
		PredicateKind:        storage.ApplicabilityPredicateRepositoryMatch,
		Predicate:            map[string]any{"repository": requiredRepository.String()},
		RequiredFields:       []string{"repository"},
		UnknownFieldBehavior: storage.ApplicabilityUnknownFieldReject,
	}); err != nil {
		t.Fatal(err)
	}
}

// mustCreateDependencyBindingPredicate stores a dependency-binding
// applicability predicate for revisionID, requiring exactly one
// name/version toolchain binding that the task fingerprint must satisfy
// (see internal/retrieval/applicability.go's dependencyBindingPredicatePayload
// wire shape), pinned to the same fingerprint schema version the task under
// test uses.
func mustCreateDependencyBindingPredicate(
	t *testing.T,
	repositories *storage.Repositories,
	revisionID domain.MemoryArtifactRevisionID,
	schemaVersion uint32,
	requiredName string,
	requiredVersion string,
) {
	t.Helper()
	if _, err := repositories.CreateMemoryArtifactApplicabilityPredicate(context.Background(), storage.CreateMemoryArtifactApplicabilityPredicate{
		RevisionID: revisionID, FingerprintSchemaVersion: int(schemaVersion),
		PredicateKind: storage.ApplicabilityPredicateDependencyBinding,
		Predicate: map[string]any{
			"required_bindings": []map[string]string{{"name": requiredName, "version": requiredVersion}},
		},
		RequiredFields:       []string{"required_bindings"},
		UnknownFieldBehavior: storage.ApplicabilityUnknownFieldReject,
	}); err != nil {
		t.Fatal(err)
	}
}

// mustCreateClosedEpisode opens and closes an episode, within projectID and
// repositoryID, stamped with task's exact fingerprint hash -- so a later
// RunPreWorkGate call built from the SAME task fingerprint discovers it
// through M21-064's exact-identity channel.
func mustCreateClosedEpisode(
	t *testing.T,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	task fingerprint.ExactFingerprint,
) domain.EpisodeID {
	t.Helper()
	ctx := context.Background()
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Episode fixture thread",
	}); err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := repositories.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "episode-fixture-" + taskID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := task.Hash()
	if err != nil {
		t.Fatal(err)
	}
	episodeID, err := domain.NewEpisodeID()
	if err != nil {
		t.Fatal(err)
	}
	episode, err := repositories.OpenEpisode(ctx, storage.OpenEpisode{
		ID: episodeID, ProjectID: projectID, RepositoryID: repositoryID, TaskID: createdTask.ID,
		FingerprintSchemaVersion: task.SchemaVersion, FingerprintHash: hash,
		StartingRevision: "deadbeefcafefeed", IdempotencyKey: "episode-fixture-open-" + episodeID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CloseEpisode(ctx, storage.CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "deadbeefcafefeed", Outcome: domain.EpisodeOutcomeAccepted,
	}); err != nil {
		t.Fatal(err)
	}
	return episode.ID
}

// mustLinkAssuranceEvidence creates a full validation/evidence chain and
// links it to revisionID at the given assurance level, so
// buildEligibilityCandidate's SupportingEvidenceAssurance reflects it.
func mustLinkAssuranceEvidence(
	t *testing.T,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	revisionID domain.MemoryArtifactRevisionID,
	level domain.AssuranceLevel,
) {
	t.Helper()
	ctx := context.Background()
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Evidence fixture thread",
	}); err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	task, err := repositories.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "evidence-fixture-" + taskID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	validationID, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	validation, err := repositories.CreateValidation(ctx, storage.CreateValidation{
		ID: validationID, TaskID: task.ID, State: domain.ValidationStatePassed,
		Severity: domain.ValidationSeverityAdvisory, ProfileName: "fixture-profile",
	})
	if err != nil {
		t.Fatal(err)
	}
	evidenceID, err := domain.NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := repositories.CreateEvidence(ctx, storage.CreateEvidence{
		ID: evidenceID, ValidationID: validation.ID, TaskID: task.ID,
		AssuranceLevel: level, EvidenceType: "fixture", ContentHash: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repositories.RecordMemoryArtifactSupportingEvidence(ctx, revisionID, domain.SupportingEvidenceRecord{
		Evidence: evidence.ID, Source: domain.EvidenceSourceKindAutomatedValidationRun, Strength: domain.EvidenceStrengthCorroborated,
	}); err != nil {
		t.Fatal(err)
	}
}
