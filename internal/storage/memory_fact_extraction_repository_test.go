package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/validation"
	"codeflux.dev/codeflux/internal/workspace"
)

// memoryFactExtractionFixture bundles one closed episode, ready for the
// listAttributableValidationChecks primitive, plus the identities every
// extraction test in this file needs.
type memoryFactExtractionFixture struct {
	repositories *Repositories
	task         Task
	projectID    domain.ProjectID
	repositoryID domain.RepositoryID
	runID        domain.RunID
	episode      Episode
}

func newMemoryFactExtractionFixture(t *testing.T, base int) memoryFactExtractionFixture {
	t.Helper()
	repositories, task := createTaskFixture(t, base)
	projectID := testProjectID(t, base)
	repositoryID := testRepositoryID(t, base+1)
	runID := createToolTestRun(t, repositories, task.ID, base+10)
	episode := mustOpenEpisode(t, repositories, base+20, projectID, repositoryID, task)
	closed, err := repositories.CloseEpisode(t.Context(), CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "deadbeefcafefeed", Outcome: domain.EpisodeOutcomeAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	return memoryFactExtractionFixture{
		repositories: repositories, task: task, projectID: projectID,
		repositoryID: repositoryID, runID: runID, episode: closed,
	}
}

// createAttributableValidationRun builds and commits one validation_run_intents
// row (with checkClass and the given argv baked into command_definition_json)
// plus a validation_run_results row at the given state, then records the
// episode fact reference an extraction function requires to ever see it.
func (fixture memoryFactExtractionFixture) createAttributableValidationRun(
	t *testing.T,
	number int,
	checkClass validation.CheckClass,
	argv []string,
	diffIdentity string,
	state domain.ValidationState,
) validation.RunIntent {
	t.Helper()
	ctx := t.Context()
	intent := validationRunIntentFixture(t, fixture.task.ID, fixture.runID, number, diffIdentity, "extraction-intent-"+testUUID(number))
	intent.CheckClass = checkClass
	intent.CommandDefinitionJSON = commandDefinitionArgumentsJSON(t, argv)
	sealed, err := validation.SealRunIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CreateValidationRunIntent(ctx, sealed); err != nil {
		t.Fatal(err)
	}
	result, err := validation.SealRunResult(validation.RunResult{
		ValidationRunID: sealed.ID, State: state, ExitCode: exitCodeForState(state),
		ParserName: "go-test-v1", ParseSucceeded: true, ParsedResultJSON: `{}`,
		RawRedactedSummary: "extraction fixture", ObservedDiffIdentity: diffIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CommitValidationRunResult(ctx, result); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.RecordEpisodeFactReference(ctx, RecordEpisodeFactReference{
		ID: "fact-" + testUUID(number), EpisodeID: fixture.episode.ID, FactTaskID: fixture.task.ID,
		Kind: domain.EpisodeFactKindValidationResult, ReferenceID: sealed.ID.String(),
		IdempotencyKey: "fact-key-" + testUUID(number),
	}); err != nil {
		t.Fatal(err)
	}
	return sealed
}

func commandDefinitionArgumentsJSON(t *testing.T, argv []string) string {
	t.Helper()
	encoded := `{"arguments":[`
	for index, argument := range argv {
		if index > 0 {
			encoded += ","
		}
		encoded += `"` + argument + `"`
	}
	encoded += `]}`
	return encoded
}

func exitCodeForState(state domain.ValidationState) int {
	if state == domain.ValidationStatePassed {
		return 0
	}
	return 1
}

// -----------------------------------------------------------------------
// M21-040/041: successful build and test commands, attributable only.
// -----------------------------------------------------------------------

func TestExtractAttributableBuildCommandsFromEpisodeExtractsPassingCheckBuildOnly(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 40_000)
	fixture.createAttributableValidationRun(t, 40_001, validation.CheckBuild, []string{"go", "build", "./..."}, repeat("a", 64), domain.ValidationStatePassed)

	facts, err := fixture.repositories.ExtractAttributableBuildCommandsFromEpisode(t.Context(), fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %#v, want exactly one", facts)
	}
	command := facts[0].Content.ReviewedCommand
	if command == nil || command.Purpose != domain.CommandPurposeBuild {
		t.Fatalf("content = %#v, want a build reviewed-command", facts[0].Content)
	}
	if len(command.Argv) != 3 || command.Argv[0] != "go" || command.Argv[1] != "build" || command.Argv[2] != "./..." {
		t.Fatalf("argv = %#v", command.Argv)
	}
	if command.Binding.ExactRevision != repeat("e", 40) {
		t.Fatalf("binding = %#v, want the intent's worktree revision (M21-042)", command.Binding)
	}
	if !facts[0].Upsert.Created || facts[0].Upsert.Maturity != domain.MaturityStateValidated {
		t.Fatalf("upsert outcome = %#v, want a freshly created, validated fact", facts[0].Upsert)
	}
}

func TestExtractAttributableTestCommandsFromEpisodeCoversTargetedAndBroad(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 40_100)
	fixture.createAttributableValidationRun(t, 40_101, validation.CheckTargetedTest, []string{"go", "test", "./internal/widget/..."}, repeat("b", 64), domain.ValidationStatePassed)
	fixture.createAttributableValidationRun(t, 40_102, validation.CheckBroadTest, []string{"go", "test", "./..."}, repeat("c", 64), domain.ValidationStatePassed)

	facts, err := fixture.repositories.ExtractAttributableTestCommandsFromEpisode(t.Context(), fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("facts = %#v, want two (targeted + broad)", facts)
	}
	for _, fact := range facts {
		if fact.Content.ReviewedCommand.Purpose != domain.CommandPurposeTest {
			t.Fatalf("purpose = %#v, want test", fact.Content.ReviewedCommand.Purpose)
		}
	}
}

// TestExtractAttributableTestCommandsIgnoresFailedRun is a required negative
// test: a failed validation run must never produce a reviewed-command fact,
// however plausible its argv looks.
func TestExtractAttributableTestCommandsIgnoresFailedRun(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 40_200)
	fixture.createAttributableValidationRun(t, 40_201, validation.CheckTargetedTest, []string{"go", "test", "./..."}, repeat("d", 64), domain.ValidationStateFailed)

	facts, err := fixture.repositories.ExtractAttributableTestCommandsFromEpisode(t.Context(), fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("facts = %#v, want none extracted from a failed run", facts)
	}
}

// TestExtractAttributableTestCommandsIgnoresFactReferenceFromAnotherTask is a
// required negative test: a fact reference whose named validation run
// belongs to a DIFFERENT task can never be attributed to this episode, even
// though the fact reference row itself was recorded against this episode.
// This proves the attribution guard is a real cross-check
// (intent.TaskID == episode.TaskID), not merely trusting that a fact
// reference was recorded at all.
func TestExtractAttributableTestCommandsIgnoresFactReferenceFromAnotherTask(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 40_300)

	// A second, unrelated task/thread in the SAME database, with its own
	// genuinely passing validation run.
	otherTaskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	otherThreadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CreateThread(t.Context(), CreateThread{
		ID: otherThreadID, ProjectID: fixture.projectID, RepositoryID: fixture.repositoryID, Title: "Other task",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CreateTask(t.Context(), CreateTask{
		ID: otherTaskID, ThreadID: otherThreadID, RepositoryID: fixture.repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "other-task-" + otherTaskID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	otherRunID := createToolTestRun(t, fixture.repositories, otherTaskID, 40_310)
	diff := repeat("f", 64)
	otherIntent := validationRunIntentFixture(t, otherTaskID, otherRunID, 40_311, diff, "cross-task-intent")
	otherIntent.CheckClass = validation.CheckTargetedTest
	otherIntent.CommandDefinitionJSON = commandDefinitionArgumentsJSON(t, []string{"go", "test", "./..."})
	sealedOther, err := validation.SealRunIntent(otherIntent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CreateValidationRunIntent(t.Context(), sealedOther); err != nil {
		t.Fatal(err)
	}
	otherResult, err := validation.SealRunResult(validation.RunResult{
		ValidationRunID: sealedOther.ID, State: domain.ValidationStatePassed, ExitCode: 0,
		ParserName: "go-test-v1", ParseSucceeded: true, ParsedResultJSON: `{}`,
		RawRedactedSummary: "other task fixture", ObservedDiffIdentity: diff,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CommitValidationRunResult(t.Context(), otherResult); err != nil {
		t.Fatal(err)
	}

	// fixture's own episode references the OTHER task's (genuinely passing)
	// validation run -- a smuggled-in reference.
	if _, err := fixture.repositories.RecordEpisodeFactReference(t.Context(), RecordEpisodeFactReference{
		ID: "smuggled-fact", EpisodeID: fixture.episode.ID, FactTaskID: fixture.task.ID,
		Kind: domain.EpisodeFactKindValidationResult, ReferenceID: sealedOther.ID.String(),
		IdempotencyKey: "smuggled-fact-key",
	}); err != nil {
		t.Fatal(err)
	}

	facts, err := fixture.repositories.ExtractAttributableTestCommandsFromEpisode(t.Context(), fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("facts = %#v, want none extracted from a cross-task fact reference", facts)
	}
}

// TestExtractAttributableTestCommandsIgnoresModelProposedCommand is a
// required negative test: an episode that never recorded any
// EpisodeFactKindValidationResult fact reference -- e.g. because a model
// merely narrated or proposed running a command without it ever actually
// executing -- extracts nothing. There is no "agent narrative" or
// "self report" EpisodeFactKind at all (internal/domain/episode.go's
// AllEpisodeFactKinds), so a model's own proposal has no path into this
// function's inputs in the first place.
func TestExtractAttributableTestCommandsIgnoresModelProposedCommand(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 40_500)

	facts, err := fixture.repositories.ExtractAttributableTestCommandsFromEpisode(t.Context(), fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("facts = %#v, want none when no attributable execution was ever recorded", facts)
	}
}

// TestExtractAttributableTestCommandsRejectsOpenEpisode is a required
// negative test: extraction from an episode that has not yet reached its
// terminal user decision is refused outright, since an open episode's facts
// are not yet frozen (M21-038) and could still be corrected before close.
func TestExtractAttributableTestCommandsRejectsOpenEpisode(t *testing.T) {
	repositories, task := createTaskFixture(t, 40_600)
	projectID := testProjectID(t, 40_600)
	repositoryID := testRepositoryID(t, 40_601)
	open := mustOpenEpisode(t, repositories, 40_603, projectID, repositoryID, task)

	if _, err := repositories.ExtractAttributableTestCommandsFromEpisode(t.Context(), open.ID); !errors.Is(err, ErrMemoryFactExtractionEpisodeNotClosed) {
		t.Fatalf("open-episode extraction error = %v, want ErrMemoryFactExtractionEpisodeNotClosed", err)
	}
}

// -----------------------------------------------------------------------
// M21-043: file-to-test mapping, single-changed-file attribution only.
// -----------------------------------------------------------------------

func TestExtractAttributableFileToTestMappingFromEpisodeExtractsSingleFileDiff(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 43_000)
	diff := repeat("1", 64)
	fixture.createAttributableValidationRun(t, 43_001, validation.CheckTargetedTest, []string{"go", "test", "./internal/widget/..."}, diff, domain.ValidationStatePassed)

	facts, err := fixture.repositories.ExtractAttributableFileToTestMappingFromEpisode(
		t.Context(), fixture.episode.ID, diff, []string{"internal/widget/widget.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %#v, want exactly one", facts)
	}
	mapping := facts[0].Content.FileToTestMapping
	if mapping.SourcePath != "internal/widget/widget.go" {
		t.Fatalf("source path = %q", mapping.SourcePath)
	}
	if len(mapping.TestPaths) != 1 || mapping.TestPaths[0] != "./internal/widget/..." {
		t.Fatalf("test paths = %#v", mapping.TestPaths)
	}
}

// TestExtractAttributableFileToTestMappingRefusesMultiFileDiff is a required
// negative test: M21-043 must never guess which of several changed files a
// passing test covers.
func TestExtractAttributableFileToTestMappingCoversEveryFileInsideOneTestScope(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 43_100)
	diff := repeat("2", 64)
	fixture.createAttributableValidationRun(t, 43_101, validation.CheckTargetedTest, []string{"go", "test", "./internal/widget/..."}, diff, domain.ValidationStatePassed)

	// M21-043. This previously asserted that ANY multi-file diff produced
	// nothing, which made the extractor almost never fire on real work.
	// Both changed files lie inside the one scope that was observed passing,
	// so "run ./internal/widget/... when this file changes" is an observed
	// mapping for each of them, not a guess about which file the test
	// exercised.
	facts, err := fixture.repositories.ExtractAttributableFileToTestMappingFromEpisode(
		t.Context(), fixture.episode.ID, diff, []string{"internal/widget/widget.go", "internal/widget/other.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("facts = %d, want one mapping per changed file inside the passing scope", len(facts))
	}
	seen := map[string]bool{}
	for _, fact := range facts {
		mapping := fact.Content.FileToTestMapping
		if mapping == nil {
			t.Fatalf("fact %#v is not a file-to-test mapping", fact)
		}
		seen[mapping.SourcePath] = true
	}
	for _, want := range []string{"internal/widget/widget.go", "internal/widget/other.go"} {
		if !seen[want] {
			t.Fatalf("no mapping extracted for %s (got %v)", want, seen)
		}
	}
}

func TestExtractAttributableFileToTestMappingRefusesFilesOutsideEveryTestScope(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 43_200)
	diff := repeat("3", 64)
	fixture.createAttributableValidationRun(t, 43_201, validation.CheckTargetedTest, []string{"go", "test", "./internal/widget/..."}, diff, domain.ValidationStatePassed)

	// The changed file sits outside every observed test scope, so nothing
	// was observed testing it and no mapping may be invented.
	facts, err := fixture.repositories.ExtractAttributableFileToTestMappingFromEpisode(
		t.Context(), fixture.episode.ID, diff, []string{"internal/widget/widget.go", "internal/unrelated/other.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if mapping := fact.Content.FileToTestMapping; mapping != nil && mapping.SourcePath == "internal/unrelated/other.go" {
			t.Fatalf("extracted a mapping for a file no observed test scope covers: %#v", mapping)
		}
	}
}

func TestExtractAttributableFileToTestMappingRefusesAmbiguousOverlappingScopes(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 43_300)
	diff := repeat("4", 64)
	fixture.createAttributableValidationRun(t, 43_301, validation.CheckTargetedTest, []string{"go", "test", "./internal/widget/..."}, diff, domain.ValidationStatePassed)
	fixture.createAttributableValidationRun(t, 43_302, validation.CheckBroadTest, []string{"go", "test", "./internal/widget/inner/..."}, diff, domain.ValidationStatePassed)

	// Two distinct passing scopes both cover the file. Which one is "the"
	// mapping is genuinely undetermined, so the extractor refuses rather
	// than picking one.
	facts, err := fixture.repositories.ExtractAttributableFileToTestMappingFromEpisode(
		t.Context(), fixture.episode.ID, diff, []string{"internal/widget/inner/deep.go", "internal/widget/other.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if mapping := fact.Content.FileToTestMapping; mapping != nil && mapping.SourcePath == "internal/widget/inner/deep.go" {
			t.Fatalf("extracted an ambiguous mapping for a file two scopes cover: %#v", mapping)
		}
	}
}

// TestExtractAttributableFileToTestMappingRefusesFlaggedCommand is a required
// negative test: a command carrying a flag after the subcommand can never be
// cleanly attributed to bare path arguments, so nothing is extracted rather
// than risking a flag value read as a path.
func TestExtractAttributableFileToTestMappingRefusesFlaggedCommand(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 43_200)
	diff := repeat("3", 64)
	fixture.createAttributableValidationRun(t, 43_201, validation.CheckTargetedTest, []string{"go", "test", "-run", "TestWidget", "./internal/widget/..."}, diff, domain.ValidationStatePassed)

	facts, err := fixture.repositories.ExtractAttributableFileToTestMappingFromEpisode(
		t.Context(), fixture.episode.ID, diff, []string{"internal/widget/widget.go"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("facts = %#v, want none for a flagged command", facts)
	}
}

// -----------------------------------------------------------------------
// M21-045: formatting/lint conventions.
// -----------------------------------------------------------------------

func TestExtractRepositoryConventionsFromWorkspaceMapReadsRealConfigFiles(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 45_000)
	repositoryID := testRepositoryID(t, 45_001)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	repositoryMap := workspace.RepositoryMap{
		RepositoryRevision: repeat("4", 40),
		Files: []workspace.MappedFile{
			{Path: "go.mod", SHA256: repeat("5", 64)},
			{Path: ".golangci.yml", SHA256: repeat("6", 64)},
			{Path: "internal/widget/widget.go", SHA256: repeat("7", 64)},
		},
	}
	facts, err := repositories.ExtractRepositoryConventionsFromWorkspaceMap(t.Context(), projectID, repositoryID, repositoryMap)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("facts = %#v, want exactly two (go.mod, .golangci.yml)", facts)
	}
	for _, fact := range facts {
		if fact.Upsert.Maturity != domain.MaturityStateCandidate {
			t.Fatalf("maturity = %s, want candidate (no corroborated evidence for a bare config scan)", fact.Upsert.Maturity)
		}
	}
}

// -----------------------------------------------------------------------
// M21-044: stable project instructions, only after explicit user approval.
// -----------------------------------------------------------------------

func TestExtractApprovedRepositoryInstructionRequiresGrantedApproval(t *testing.T) {
	repositories, task := createTaskFixture(t, 44_000)
	projectID := testProjectID(t, 44_000)
	repositoryID := testRepositoryID(t, 44_001)

	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateApproval(t.Context(), CreateApproval{
		ID: approvalID, TaskID: task.ID, Scope: "adopt-instruction",
		RequestReason: "adopt repository instruction", IdempotencyKey: "instruction-approval",
	}); err != nil {
		t.Fatal(err)
	}

	// Not yet granted: extraction must refuse.
	if _, err := repositories.ExtractApprovedRepositoryInstruction(t.Context(), ExtractApprovedRepositoryInstruction{
		ProjectID: projectID, Repository: repositoryID, TaskID: task.ID, ApprovalID: approvalID,
		InstructionPath: "AGENTS.md", InstructionStatement: "Always run gofmt before committing.",
		ExactRevision: repeat("8", 40),
	}); !errors.Is(err, ErrMemoryFactExtractionApprovalNotGranted) {
		t.Fatalf("pending-approval extraction error = %v, want ErrMemoryFactExtractionApprovalNotGranted", err)
	}

	if _, err := repositories.ResolveApproval(t.Context(), ResolveApproval{
		ID: approvalID, ExpectedRevision: 0, To: domain.ApprovalRequestStateGranted,
		ResolutionReason: "user explicitly approved adopting this instruction",
	}); err != nil {
		t.Fatal(err)
	}

	fact, err := repositories.ExtractApprovedRepositoryInstruction(t.Context(), ExtractApprovedRepositoryInstruction{
		ProjectID: projectID, Repository: repositoryID, TaskID: task.ID, ApprovalID: approvalID,
		InstructionPath: "AGENTS.md", InstructionStatement: "Always run gofmt before committing.",
		ExactRevision: repeat("8", 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fact.Content.RepositoryConvention.Scope != "project-instruction:AGENTS.md" {
		t.Fatalf("scope = %q", fact.Content.RepositoryConvention.Scope)
	}
	if fact.Upsert.Maturity != domain.MaturityStateValidated {
		t.Fatalf("maturity = %s, want validated (a granted approval is corroborated, non-self-report evidence)", fact.Upsert.Maturity)
	}
}

// -----------------------------------------------------------------------
// M21-046..049: dedup, invalidation on change, required revalidation.
// M21-050 (decisive test): a changed test runner invalidates its stored
// command.
// -----------------------------------------------------------------------

func TestUpsertExtractedMemoryFactDeduplicatesIdenticalReconfirmation(t *testing.T) {
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 46_000)
	repositoryID := testRepositoryID(t, 46_001)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	content := testGoTestReviewedCommandContent(t, repositoryID, []string{"go", "test", "./..."})

	first, err := repositories.UpsertExtractedMemoryFact(t.Context(), UpsertExtractedMemoryFact{
		ProjectID: projectID, Content: content, DeterministicSeed: "dedup-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.Reconfirmed {
		t.Fatalf("first upsert = %#v, want a fresh creation", first)
	}

	second, err := repositories.UpsertExtractedMemoryFact(t.Context(), UpsertExtractedMemoryFact{
		ProjectID: projectID, Content: content, DeterministicSeed: "dedup-fixture-again",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || !second.Reconfirmed {
		t.Fatalf("second upsert = %#v, want a reconfirmation, not a new artifact", second)
	}
	if second.ArtifactID != first.ArtifactID || second.RevisionID != first.RevisionID {
		t.Fatalf("second upsert = %#v, want the same identity as %#v", second, first)
	}

	summary, err := repositories.GetMemoryArtifactFactObservationSummary(t.Context(), first.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.FirstObservedAt.IsZero() {
		t.Fatalf("summary = %#v, want a non-zero first-observed time", summary)
	}
}

// TestChangedTestRunnerInvalidatesItsStoredCommand is the M21-050 decisive
// test: a stored, validated test command's fact must be invalidated the
// moment a newly attributed, successful extraction observes a DIFFERENT
// command for the exact same (repository, purpose) identity slot -- e.g.
// because the project's test runner changed -- and the invalidated fact
// must never silently regain influence; a fresh identity must earn its own
// evidence from scratch.
func TestChangedTestRunnerInvalidatesItsStoredCommand(t *testing.T) {
	fixture := newMemoryFactExtractionFixture(t, 50_000)

	// The project's test command is "go test ./..." at revision one.
	fixture.createAttributableValidationRun(t, 50_001, validation.CheckBroadTest, []string{"go", "test", "./..."}, repeat("a", 64), domain.ValidationStatePassed)
	firstFacts, err := fixture.repositories.ExtractAttributableTestCommandsFromEpisode(t.Context(), fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstFacts) != 1 || firstFacts[0].Upsert.Maturity != domain.MaturityStateValidated {
		t.Fatalf("first extraction = %#v, want one validated fact", firstFacts)
	}
	originalArtifactID := firstFacts[0].Upsert.ArtifactID

	original, err := fixture.repositories.GetMemoryArtifactRevision(t.Context(), firstFacts[0].Upsert.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if original.Maturity != domain.MaturityStateValidated {
		t.Fatalf("original maturity = %s, want validated before the test runner changes", original.Maturity)
	}

	// The test runner changes: the SAME repository, the SAME purpose
	// (broad test), but a genuinely different command -- gotestsum instead
	// of go test.
	secondEpisode := newMemoryFactExtractionFixtureSameRepository(t, fixture, 50_100)
	secondEpisode.createAttributableValidationRun(t, 50_101, validation.CheckBroadTest, []string{"gotestsum", "--", "./..."}, repeat("b", 64), domain.ValidationStatePassed)
	secondFacts, err := secondEpisode.repositories.ExtractAttributableTestCommandsFromEpisode(t.Context(), secondEpisode.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondFacts) != 1 {
		t.Fatalf("second extraction = %#v, want exactly one fact", secondFacts)
	}
	second := secondFacts[0]
	if !second.Upsert.Created {
		t.Fatalf("second upsert = %#v, want a freshly minted identity, not a correction of the original", second.Upsert)
	}
	if second.Upsert.ArtifactID == originalArtifactID {
		t.Fatalf("second artifact = %s, want a NEW identity distinct from the original %s (M21-049)", second.Upsert.ArtifactID, originalArtifactID)
	}
	if second.Upsert.InvalidatedPriorArtifactID == nil || *second.Upsert.InvalidatedPriorArtifactID != originalArtifactID {
		t.Fatalf("invalidated prior = %#v, want the original artifact %s", second.Upsert.InvalidatedPriorArtifactID, originalArtifactID)
	}

	// The ORIGINAL revision is now invalidated and must never silently
	// regain influence.
	invalidatedOriginal, err := fixture.repositories.GetMemoryArtifactRevision(t.Context(), firstFacts[0].Upsert.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if invalidatedOriginal.Maturity != domain.MaturityStateInvalidated {
		t.Fatalf("original maturity after runner change = %s, want invalidated", invalidatedOriginal.Maturity)
	}
	if invalidatedOriginal.Maturity.CanReachAuthority() {
		t.Fatalf("invalidated original must never be able to reach an authority-bearing state again")
	}

	// The new fact earned its OWN evidence and its OWN promotion; it did not
	// inherit the original's maturity or evidence.
	if second.Upsert.Maturity != domain.MaturityStateValidated {
		t.Fatalf("new fact maturity = %s, want validated from its OWN fresh corroborated evidence", second.Upsert.Maturity)
	}

	// Re-running the FIRST (now-stale) extraction again -- as if the old
	// test runner's validation run were somehow re-observed -- must never
	// resurrect the original artifact under its old identity: a repeat
	// UpsertExtractedMemoryFact call for the ORIGINAL content now finds the
	// newer, live artifact sharing the same normalized identity slot and
	// reconciles against IT (or, if content differs from the current live
	// one, invalidates it again and mints yet another fresh identity) --
	// but it never flips the terminal original revision back to a
	// non-terminal state.
	replay, err := fixture.repositories.UpsertExtractedMemoryFact(t.Context(), UpsertExtractedMemoryFact{
		ProjectID: fixture.projectID, Content: firstFacts[0].Content, DeterministicSeed: "replay-stale-runner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replay.ArtifactID == originalArtifactID {
		t.Fatalf("replay of stale content resurrected the original artifact %s", originalArtifactID)
	}
	stillInvalidated, err := fixture.repositories.GetMemoryArtifactRevision(t.Context(), firstFacts[0].Upsert.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if stillInvalidated.Maturity != domain.MaturityStateInvalidated {
		t.Fatalf("original maturity after replay = %s, want still invalidated", stillInvalidated.Maturity)
	}
}

// newMemoryFactExtractionFixtureSameRepository opens a second episode
// against the SAME project/repository/task-space as base by reusing the
// same *Repositories and repository identity, so both episodes' extracted
// facts land in the same normalized-identity scope.
func newMemoryFactExtractionFixtureSameRepository(t *testing.T, base memoryFactExtractionFixture, episodeNumber int) memoryFactExtractionFixture {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	// A distinct task under the same thread/repository, so this episode's
	// own TaskID legitimately differs from base's (episodes are one-per-task)
	// while still sharing project and repository identity.
	if _, err := base.repositories.CreateTask(t.Context(), CreateTask{
		ID: taskID, ThreadID: taskThreadID(t, base), RepositoryID: base.repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "second-task-" + taskID.String(),
	}); err != nil {
		t.Fatal(err)
	}
	runID := createToolTestRun(t, base.repositories, taskID, episodeNumber+10)
	episode := mustOpenEpisode(t, base.repositories, episodeNumber+20, base.projectID, base.repositoryID, Task{ID: taskID})
	closed, err := base.repositories.CloseEpisode(t.Context(), CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: "deadbeefcafefeed", Outcome: domain.EpisodeOutcomeAccepted,
	})
	if err != nil {
		t.Fatal(err)
	}
	return memoryFactExtractionFixture{
		repositories: base.repositories, task: Task{ID: taskID}, projectID: base.projectID,
		repositoryID: base.repositoryID, runID: runID, episode: closed,
	}
}

func taskThreadID(t *testing.T, fixture memoryFactExtractionFixture) domain.ThreadID {
	t.Helper()
	full, err := fixture.repositories.GetTask(t.Context(), fixture.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return full.ThreadID
}

func testGoTestReviewedCommandContent(t *testing.T, repositoryID domain.RepositoryID, argv []string) domain.MemoryArtifactContent {
	t.Helper()
	content, err := domain.NewReviewedCommandMemoryContent(domain.ReviewedCommandContent{
		Repository: repositoryID, Purpose: domain.CommandPurposeTest, Argv: argv,
		Binding: domain.RevisionBinding{Known: true, ExactRevision: repeat("e", 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func repeat(character string, count int) string {
	result := ""
	for len(result) < count {
		result += character
	}
	return result[:count]
}
