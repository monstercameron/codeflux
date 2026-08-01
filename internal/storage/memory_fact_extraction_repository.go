// Package storage: deterministic fact extraction (Milestone 21,
// M21-040..049). See docs/plan.md §31 "Workspace Facts" and the evidence-
// strength ordering above "Learning Artifact Types": every extraction
// function in this file admits a memory artifact ONLY from an already
// durable, attributable record of something that actually happened --
// never from a model's own narrative or proposal. Concretely:
//
//   - M21-040/041 (build/test commands) and M21-045's accepted-work half
//     walk an episode's own EpisodeFactKindValidationResult fact
//     references back to the real validation_run_intents/
//     validation_run_results rows they name (internal/validation,
//     migration 000022), keeping only checks whose referenced intent's
//     TaskID matches the episode's own TaskID (attribution) and whose
//     result state is domain.ValidationStatePassed (success;
//     validation.RunResult.Validate already ties Passed to exit code 0, no
//     timeout, no cancellation).
//   - M21-043 (file-to-test mapping) additionally requires the caller to
//     supply the real, already-computed changed-file set for one specific
//     diff identity and only extracts when that set names exactly one
//     file, so a passing test can never be guessed onto one of several
//     changed files.
//   - M21-044 (stable project instructions) requires a real, granted
//     approvals row scoped to the task; no granted approval, no fact.
//   - M21-045's configuration half reads real, checked-in repository
//     configuration files (internal/workspace's deterministic repository
//     scan), never a model.
//
// M21-046..049 (dedup by normalized identity, first-observed/last-
// confirmed/supporting-episode tracking, invalidation on change, and
// required revalidation before an invalidated fact regains influence) are
// realized once, centrally, by UpsertExtractedMemoryFact, which every
// extraction entry point in this file funnels through.
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/validation"
	"codeflux.dev/codeflux/internal/workspace"
)

// ErrMemoryFactExtractionEpisodeNotClosed classifies an attempt to extract
// facts from an episode that has not yet reached its terminal user decision
// (domain.EpisodeStatusClosed). An open episode is still accumulating
// facts (see internal/domain/episode.go); extracting from it would risk
// admitting a "fact" whose supporting validation result could still be
// invalidated by a later event in the same episode before it ever closes.
var ErrMemoryFactExtractionEpisodeNotClosed = errors.New("deterministic fact extraction requires a closed episode")

// ErrMemoryFactExtractionApprovalNotGranted classifies an attempt to
// extract a stable project instruction (M21-044) without a real, granted
// approvals row scoped to the task. Extraction never proceeds on the
// strength of a model's own claim that the user approved something.
var ErrMemoryFactExtractionApprovalNotGranted = errors.New("stable project instruction extraction requires an explicit granted user approval")

// ErrMemoryFactExtractionScopeTooLarge classifies a normalized-identity
// dedup scan (M21-046) whose (project, kind, repository) scope holds more
// live memory-artifact revisions than memoryFactExtractionScanLimit. Per
// AGENTS.md "Avoid unbounded ... database reads," this fails loudly rather
// than silently truncating the scan and risking a false "no match" that
// would mint a duplicate fact.
var ErrMemoryFactExtractionScopeTooLarge = errors.New("memory fact extraction normalized-identity scan exceeds the bounded read limit")

// memoryFactExtractionScanLimit bounds findLatestMemoryArtifactRevisionByNormalizedIdentity's
// scan. No migration was authorized this pass to add an indexed identity-hash
// column (see this file's package doc and the M21-046 report), so
// deduplication is realized today as a bounded scan over the existing
// (kind, content_repository_id, project_id) columns, decoding and comparing
// domain.NormalizedMemoryFactIdentity in Go. This is adequate at prototype
// scale (matching the plan's own "brute-force ... for prototype-scale data"
// precedent for vector search, M21-083) but would need the indexed column
// to stay bounded as a repository's fact count grows.
const memoryFactExtractionScanLimit = 2000

// -----------------------------------------------------------------------
// Shared attributable-validation-check primitive (M21-040/041/043/045)
// -----------------------------------------------------------------------

// attributableValidationCheck is one attributable, actually-succeeded
// validation check, joined from an episode's own fact references back to
// the durable validation_run_intents/validation_run_results rows.
type attributableValidationCheck struct {
	Intent    validation.RunIntent
	Result    validation.RunResult
	Arguments []string
}

// listAttributableValidationChecks is the shared M21-040/041/043/045
// primitive: it walks episode's recorded EpisodeFactKindValidationResult
// fact references (never any other episode-fact kind, and never the
// model's own narrative -- EpisodeFactKind has no "agent narrative" or
// "self report" variant at all, see internal/domain/episode.go) back to the
// real validation_run_intents/validation_run_results rows they name,
// keeping only checks that are:
//
//   - attributable: the referenced intent's TaskID equals episode.TaskID,
//     so a fact reference can never smuggle in another task's run, and (when
//     requireDiffIdentity is non-empty) the intent's own DiffIdentity
//     equals it exactly;
//   - of a wanted CheckClass;
//   - actually succeeded: validation.RunResult.State ==
//     domain.ValidationStatePassed, which validation.RunResult.Validate
//     already ties to exit code 0, no timeout, and no cancellation.
//
// A malformed or dangling fact reference, a check of the wrong class, or a
// run that merely completed without passing is silently skipped, not
// errored: extraction is expected to often find nothing to extract, and one
// bad reference must never abort every other attributable check in the
// same episode.
func (repositories *Repositories) listAttributableValidationChecks(
	ctx context.Context,
	episode Episode,
	wantClasses map[validation.CheckClass]bool,
	requireDiffIdentity string,
) ([]attributableValidationCheck, error) {
	if episode.Status != domain.EpisodeStatusClosed {
		return nil, fmt.Errorf("%w: episode %s", ErrMemoryFactExtractionEpisodeNotClosed, episode.ID)
	}
	references, err := repositories.ListEpisodeFactReferences(ctx, episode.ID)
	if err != nil {
		return nil, err
	}
	var checks []attributableValidationCheck
	for _, reference := range references {
		if reference.Kind != domain.EpisodeFactKindValidationResult || strings.TrimSpace(reference.ReferenceID) == "" {
			continue
		}
		validationRunID, err := domain.ParseValidationID(reference.ReferenceID)
		if err != nil {
			continue
		}
		intent, err := repositories.GetValidationRunIntent(ctx, validationRunID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if intent.TaskID != episode.TaskID {
			continue
		}
		if requireDiffIdentity != "" && intent.DiffIdentity != requireDiffIdentity {
			continue
		}
		if !wantClasses[intent.CheckClass] {
			continue
		}
		result, err := repositories.GetValidationRunResult(ctx, validationRunID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if result.State != domain.ValidationStatePassed {
			continue
		}
		arguments, err := decodeCommandDefinitionArguments(intent.CommandDefinitionJSON)
		if err != nil {
			continue
		}
		checks = append(checks, attributableValidationCheck{Intent: intent, Result: result, Arguments: arguments})
	}
	return checks, nil
}

// commandDefinitionArguments decodes exactly the "arguments" field
// internal/validation's own commandDefinition function encodes into
// validation_run_intents.command_definition_json; other fields in that
// JSON (class, source, command_fingerprint, timeout_nanoseconds) are
// ignored here.
type commandDefinitionArguments struct {
	Arguments []string `json:"arguments"`
}

func decodeCommandDefinitionArguments(commandDefinitionJSON string) ([]string, error) {
	var decoded commandDefinitionArguments
	if err := json.Unmarshal([]byte(commandDefinitionJSON), &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Arguments) == 0 {
		return nil, errors.New("command definition carries no arguments")
	}
	return decoded.Arguments, nil
}

// -----------------------------------------------------------------------
// M21-040/041: successful build and test commands
// -----------------------------------------------------------------------

// ExtractedMemoryFact is one deterministically extracted, already-admitted
// memory-artifact observation: the typed content that was extracted and the
// M21-046..049 reconciliation outcome recording how it was admitted.
type ExtractedMemoryFact struct {
	Content domain.MemoryArtifactContent
	Upsert  UpsertExtractedMemoryFactOutcome
}

// ExtractAttributableBuildCommandsFromEpisode extracts M21-040 successful
// build commands: reviewed-command facts (an argv array, never a shell
// string; M21-042 binds each one to the exact repository revision the
// check actually ran against) for domain.CommandPurposeBuild, one per
// attributable, actually-succeeded validation.CheckBuild check recorded
// against episodeID.
func (repositories *Repositories) ExtractAttributableBuildCommandsFromEpisode(
	ctx context.Context,
	episodeID domain.EpisodeID,
) ([]ExtractedMemoryFact, error) {
	return repositories.extractAttributableReviewedCommands(
		ctx, episodeID, domain.CommandPurposeBuild,
		map[validation.CheckClass]bool{validation.CheckBuild: true},
	)
}

// ExtractAttributableTestCommandsFromEpisode extracts M21-041 successful
// test commands the same way, for domain.CommandPurposeTest across both
// validation.CheckTargetedTest and validation.CheckBroadTest.
func (repositories *Repositories) ExtractAttributableTestCommandsFromEpisode(
	ctx context.Context,
	episodeID domain.EpisodeID,
) ([]ExtractedMemoryFact, error) {
	return repositories.extractAttributableReviewedCommands(
		ctx, episodeID, domain.CommandPurposeTest,
		map[validation.CheckClass]bool{validation.CheckTargetedTest: true, validation.CheckBroadTest: true},
	)
}

func (repositories *Repositories) extractAttributableReviewedCommands(
	ctx context.Context,
	episodeID domain.EpisodeID,
	purpose domain.CommandPurpose,
	wantClasses map[validation.CheckClass]bool,
) ([]ExtractedMemoryFact, error) {
	episode, err := repositories.GetEpisode(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	checks, err := repositories.listAttributableValidationChecks(ctx, episode, wantClasses, "")
	if err != nil {
		return nil, err
	}
	extracted := make([]ExtractedMemoryFact, 0, len(checks))
	for _, check := range checks {
		content, err := domain.NewReviewedCommandMemoryContent(domain.ReviewedCommandContent{
			Repository: episode.RepositoryID,
			Purpose:    purpose,
			// M21-042: Argv is preserved byte-for-byte as executed -- never
			// rewritten, reordered, or summarized -- which is what carries
			// the relevant path scope (e.g. "./..." or
			// "./internal/foo/...") forward; Binding.ExactRevision is the
			// exact repository revision the check actually ran against.
			Argv:    check.Arguments,
			Binding: domain.RevisionBinding{Known: true, ExactRevision: check.Intent.WorktreeRevision},
		})
		if err != nil {
			continue
		}
		fact, err := repositories.admitAttributableValidationCheckFact(ctx, episode, check, content, "reviewed-command:"+string(purpose))
		if err != nil {
			return nil, err
		}
		extracted = append(extracted, fact)
	}
	return extracted, nil
}

// -----------------------------------------------------------------------
// M21-043: file-to-test mapping from an observed single-file diff
// -----------------------------------------------------------------------

// ExtractAttributableFileToTestMappingFromEpisode extracts M21-043
// file-to-test mapping facts from an OBSERVED successful validation --
// never from a naming convention or a guessed source/test correspondence.
//
// observedChangedFiles must be the exact, real, repository-relative
// changed-file set for diffIdentity, as computed by the repository's real
// diff/gitwork layer (e.g. the same enumeration a sealed final evidence
// report's ChangedFiles carries for that diff) -- this function never
// infers or guesses it. Attribution requires exactly one changed file: when
// zero or more than one file changed under diffIdentity, a passing test
// run cannot be deterministically attributed to any single one of them, so
// this extracts nothing for that diff rather than guessing.
//
// It further requires the attributed check's own invoked command to be a
// clean, flag-free path-only invocation (every argument after the program
// name and subcommand is a bare positional argument, never "-"-prefixed),
// so the extracted TestPaths are exactly the paths the passing command was
// actually given, never a flag's value mistaken for a path.
func (repositories *Repositories) ExtractAttributableFileToTestMappingFromEpisode(
	ctx context.Context,
	episodeID domain.EpisodeID,
	diffIdentity string,
	observedChangedFiles []string,
) ([]ExtractedMemoryFact, error) {
	if len(observedChangedFiles) == 0 {
		return nil, nil
	}
	for _, path := range observedChangedFiles {
		if path == "" || strings.TrimSpace(path) != path {
			return nil, nil
		}
	}
	episode, err := repositories.GetEpisode(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	checks, err := repositories.listAttributableValidationChecks(
		ctx, episode,
		map[validation.CheckClass]bool{validation.CheckTargetedTest: true, validation.CheckBroadTest: true},
		diffIdentity,
	)
	if err != nil {
		return nil, err
	}
	extracted := make([]ExtractedMemoryFact, 0, len(checks))
	for _, sourcePath := range observedChangedFiles {
		for _, check := range attributableChecksForChangedFile(checks, observedChangedFiles, sourcePath) {
			testPaths, ok := cleanPositionalPathArguments(check.Arguments)
			if !ok {
				continue
			}
			content, err := domain.NewFileToTestMappingMemoryContent(domain.FileToTestMappingContent{
				Repository: episode.RepositoryID,
				SourcePath: sourcePath,
				TestPaths:  testPaths,
				Binding:    domain.RevisionBinding{Known: true, ExactRevision: check.Intent.WorktreeRevision},
			})
			if err != nil {
				continue
			}
			fact, err := repositories.admitAttributableValidationCheckFact(ctx, episode, check, content, "file-to-test-mapping")
			if err != nil {
				return nil, err
			}
			extracted = append(extracted, fact)
		}
	}
	return extracted, nil
}

// attributableChecksForChangedFile decides which passing test checks may be
// attributed to one changed file (M21-043).
//
// A single-file diff is unambiguous by construction: only one file changed,
// so every test that passed exercised that change, and every check is
// attributable. This is the original, already-proven rule.
//
// A multi-file diff is not. Attributing every passing test to every changed
// file would manufacture mappings nobody observed. Instead a file is
// attributed only to checks whose declared path scope COVERS it, and only
// when exactly one such check exists — an unambiguous, observed containment
// rather than a guess. A file covered by zero checks, or by several,
// produces no mapping at all.
func attributableChecksForChangedFile(
	checks []attributableValidationCheck,
	observedChangedFiles []string,
	sourcePath string,
) []attributableValidationCheck {
	if len(observedChangedFiles) == 1 {
		return checks
	}
	var covering []attributableValidationCheck
	for _, check := range checks {
		scopes, ok := cleanPositionalPathArguments(check.Arguments)
		if !ok {
			continue
		}
		for _, scope := range scopes {
			if testPathScopeCoversFile(scope, sourcePath) {
				covering = append(covering, check)
				break
			}
		}
	}
	if len(covering) != 1 {
		return nil
	}
	return covering
}

// testPathScopeCoversFile reports whether a test command's positional path
// argument unambiguously contains sourcePath. It understands the Go package
// spellings the repository actually uses ("./internal/storage/...",
// "./internal/storage", "internal/storage") and requires a directory-boundary
// match, so "internal/store" never appears to cover "internal/storage/x.go".
func testPathScopeCoversFile(scope string, sourcePath string) bool {
	normalized := strings.TrimSuffix(strings.TrimPrefix(scope, "./"), "/...")
	normalized = strings.TrimSuffix(normalized, "/")
	if normalized == "" || normalized == "." {
		// A whole-repository scope covers everything, which is exactly the
		// ambiguity this rule exists to reject.
		return false
	}
	file := strings.TrimPrefix(sourcePath, "./")
	if file == normalized {
		return true
	}
	return strings.HasPrefix(file, normalized+"/")
}

// cleanPositionalPathArguments returns arguments[2:] (everything after the
// program name and subcommand, e.g. "go"/"test") with ok=true only when
// that slice is non-empty and contains no "-"-prefixed flag. A command this
// function cannot cleanly attribute to specific path arguments is skipped
// (ok=false), never guessed at.
func cleanPositionalPathArguments(arguments []string) (paths []string, ok bool) {
	if len(arguments) <= 2 {
		return nil, false
	}
	positional := arguments[2:]
	for _, argument := range positional {
		if strings.HasPrefix(argument, "-") {
			return nil, false
		}
	}
	return append([]string(nil), positional...), true
}

// -----------------------------------------------------------------------
// M21-045: formatting/lint conventions from configuration and accepted work
// -----------------------------------------------------------------------

// ExtractRepositoryConventionsFromWorkspaceMap extracts M21-045 formatting
// and static-analysis conventions from real, checked-in repository
// configuration files (workspace.RepositoryMap.Files, produced by
// internal/workspace's deterministic repository scan, never a model),
// bound to repositoryMap.RepositoryRevision. It recognizes exactly the same
// configuration basenames internal/workspace's own discoverCommands maps to
// a lint check (".golangci.yml"/".golangci.yaml") plus a Go module root
// marker ("go.mod") for the always-applicable gofmt convention, and embeds
// each file's real content SHA-256 in the Statement text so a later edit to
// the config file (a changed content hash) changes the extracted content
// itself -- which is what drives M21-048 invalidation on the next
// extraction run over the same repository.
//
// Every extracted convention is admitted at MaturityStateCandidate: mere
// file presence, while deterministic and non-model, is not itself
// corroborated non-self-report evidence under any declared
// EvidenceSourceKind, so this never attempts promotion to Validated.
func (repositories *Repositories) ExtractRepositoryConventionsFromWorkspaceMap(
	ctx context.Context,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	repositoryMap workspace.RepositoryMap,
) ([]ExtractedMemoryFact, error) {
	var extracted []ExtractedMemoryFact
	for _, file := range repositoryMap.Files {
		var scope, tool string
		switch file.Path {
		case ".golangci.yml", ".golangci.yaml":
			scope, tool = "static-analysis", "golangci-lint run"
		case "go.mod":
			scope, tool = "formatting", "gofmt -w"
		default:
			continue
		}
		content, err := domain.NewRepositoryConventionMemoryContent(domain.RepositoryConventionContent{
			Repository: repositoryID,
			Scope:      scope,
			Statement:  fmt.Sprintf("Run `%s` (declared by %s, sha256:%s).", tool, file.Path, file.SHA256),
			Binding:    domain.RevisionBinding{Known: true, ExactRevision: repositoryMap.RepositoryRevision},
		})
		if err != nil {
			return nil, err
		}
		outcome, err := repositories.UpsertExtractedMemoryFact(ctx, UpsertExtractedMemoryFact{
			ProjectID:         projectID,
			Content:           content,
			DeterministicSeed: "config-convention:" + repositoryID.String() + ":" + file.Path,
		})
		if err != nil {
			return nil, err
		}
		extracted = append(extracted, ExtractedMemoryFact{Content: content, Upsert: outcome})
	}
	return extracted, nil
}

// ExtractAttributableRepositoryConventionsFromEpisode extracts M21-045
// formatting/static-analysis conventions from accepted work: an
// attributable, actually-succeeded validation.CheckFormatter or
// validation.CheckStaticAnalysis check recorded against episodeID.
func (repositories *Repositories) ExtractAttributableRepositoryConventionsFromEpisode(
	ctx context.Context,
	episodeID domain.EpisodeID,
) ([]ExtractedMemoryFact, error) {
	episode, err := repositories.GetEpisode(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	checks, err := repositories.listAttributableValidationChecks(
		ctx, episode,
		map[validation.CheckClass]bool{validation.CheckFormatter: true, validation.CheckStaticAnalysis: true},
		"",
	)
	if err != nil {
		return nil, err
	}
	extracted := make([]ExtractedMemoryFact, 0, len(checks))
	for _, check := range checks {
		scope := "formatting"
		if check.Intent.CheckClass == validation.CheckStaticAnalysis {
			scope = "static-analysis"
		}
		content, err := domain.NewRepositoryConventionMemoryContent(domain.RepositoryConventionContent{
			Repository: episode.RepositoryID,
			Scope:      scope,
			Statement:  fmt.Sprintf("Run `%s` (observed to pass at revision %s).", strings.Join(check.Arguments, " "), check.Intent.WorktreeRevision),
			Binding:    domain.RevisionBinding{Known: true, ExactRevision: check.Intent.WorktreeRevision},
		})
		if err != nil {
			continue
		}
		fact, err := repositories.admitAttributableValidationCheckFact(ctx, episode, check, content, "repository-convention:"+scope)
		if err != nil {
			return nil, err
		}
		extracted = append(extracted, fact)
	}
	return extracted, nil
}

// admitAttributableValidationCheckFact is the shared admission tail for
// every episode/validation-check-sourced extraction (040/041/043/045b): it
// reconciles content against existing project memory (M21-046..049) and
// records the observing episode plus the check's own passing result as
// corroborated, non-self-report evidence (domain.EvidenceSourceKindAutomatedValidationRun).
func (repositories *Repositories) admitAttributableValidationCheckFact(
	ctx context.Context,
	episode Episode,
	check attributableValidationCheck,
	content domain.MemoryArtifactContent,
	evidenceType string,
) (ExtractedMemoryFact, error) {
	runID := check.Intent.RunID
	outcome, err := repositories.UpsertExtractedMemoryFact(ctx, UpsertExtractedMemoryFact{
		ProjectID:         episode.ProjectID,
		Content:           content,
		DeterministicSeed: check.Intent.ID.String() + ":" + evidenceType,
		SupportingEpisode: &episode.ID,
		Evidence: &UpsertExtractedMemoryFactEvidence{
			Source:          domain.EvidenceSourceKindAutomatedValidationRun,
			TaskID:          check.Intent.TaskID,
			RunID:           &runID,
			ContentHash:     check.Result.ResultDigest,
			EvidenceType:    evidenceType,
			SummaryRedacted: fmt.Sprintf("Extracted from attributable validation run %s (check %s).", check.Intent.ID, check.Intent.CheckID),
		},
	})
	if err != nil {
		return ExtractedMemoryFact{}, err
	}
	return ExtractedMemoryFact{Content: content, Upsert: outcome}, nil
}

// -----------------------------------------------------------------------
// M21-044: stable project instructions, only after explicit user approval
// -----------------------------------------------------------------------

// ExtractApprovedRepositoryInstruction declares one M21-044 extraction
// input: a stable project instruction that may only become project memory
// once a real, granted approval names it.
type ExtractApprovedRepositoryInstruction struct {
	ProjectID            domain.ProjectID
	Repository           domain.RepositoryID
	TaskID               domain.TaskID
	ApprovalID           domain.ApprovalID
	InstructionPath      string
	InstructionStatement string
	ExactRevision        string
}

// ExtractApprovedRepositoryInstruction extracts M21-044 stable project
// instructions ONLY after an explicit, real, granted user approval: it
// requires input.ApprovalID to name a real approvals row, scoped to
// input.TaskID, already in the Granted state -- never a model's own claim
// that the user approved something. No approval, no fact:
// ErrMemoryFactExtractionApprovalNotGranted is returned instead of creating
// anything.
func (repositories *Repositories) ExtractApprovedRepositoryInstruction(
	ctx context.Context,
	input ExtractApprovedRepositoryInstruction,
) (ExtractedMemoryFact, error) {
	switch {
	case input.ProjectID.IsZero():
		return ExtractedMemoryFact{}, errors.New("project ID must not be empty")
	case input.Repository.IsZero():
		return ExtractedMemoryFact{}, errors.New("repository ID must not be empty")
	case input.TaskID.IsZero():
		return ExtractedMemoryFact{}, errors.New("task ID must not be empty")
	case input.ApprovalID.IsZero():
		return ExtractedMemoryFact{}, errors.New("approval ID must not be empty")
	case strings.TrimSpace(input.InstructionPath) == "":
		return ExtractedMemoryFact{}, errors.New("instruction path must not be empty")
	}
	granted, err := repositories.approvalIsGranted(ctx, input.ApprovalID, input.TaskID)
	if err != nil {
		return ExtractedMemoryFact{}, err
	}
	if !granted {
		return ExtractedMemoryFact{}, ErrMemoryFactExtractionApprovalNotGranted
	}
	content, err := domain.NewRepositoryConventionMemoryContent(domain.RepositoryConventionContent{
		Repository: input.Repository,
		Scope:      "project-instruction:" + input.InstructionPath,
		Statement:  input.InstructionStatement,
		Binding:    domain.RevisionBinding{Known: true, ExactRevision: input.ExactRevision},
	})
	if err != nil {
		return ExtractedMemoryFact{}, err
	}
	sum := sha256.Sum256([]byte(input.ApprovalID.String() + "|" + input.InstructionPath + "|" + input.InstructionStatement))
	outcome, err := repositories.UpsertExtractedMemoryFact(ctx, UpsertExtractedMemoryFact{
		ProjectID:         input.ProjectID,
		Content:           content,
		DeterministicSeed: "project-instruction:" + input.Repository.String() + ":" + input.InstructionPath,
		Evidence: &UpsertExtractedMemoryFactEvidence{
			Source:          domain.EvidenceSourceKindUserApproval,
			TaskID:          input.TaskID,
			ContentHash:     hex.EncodeToString(sum[:]),
			EvidenceType:    "project-instruction-approval",
			SummaryRedacted: "Approved project instruction " + input.InstructionPath + ".",
		},
	})
	if err != nil {
		return ExtractedMemoryFact{}, err
	}
	return ExtractedMemoryFact{Content: content, Upsert: outcome}, nil
}

func (repositories *Repositories) approvalIsGranted(ctx context.Context, approvalID domain.ApprovalID, taskID domain.TaskID) (bool, error) {
	var count int
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM approvals WHERE id = ? AND task_id = ? AND state = 'granted'`,
		approvalID, taskID,
	).Scan(&count); err != nil {
		return false, classify("check approval granted for memory fact extraction", err)
	}
	return count == 1, nil
}

// -----------------------------------------------------------------------
// M21-046..049: dedup, lineage tracking, invalidation, required revalidation
// -----------------------------------------------------------------------

// UpsertExtractedMemoryFactEvidence carries the real, attributable evidence
// backing one extracted fact. It is used to mint the existing
// validations/evidence rows (internal/storage's CreateValidation and
// CreateEvidence -- never a new table) and, when the destination revision
// is still Candidate, attempt promotion through
// domain.AuthorizeMemoryArtifactMaturityGrant / TransitionMemoryArtifactMaturity:
// the same governed maturity machinery every other memory artifact goes
// through. Extraction never bypasses it.
type UpsertExtractedMemoryFactEvidence struct {
	Source          domain.EvidenceSourceKind
	TaskID          domain.TaskID
	RunID           *domain.RunID
	ContentHash     string
	EvidenceType    string
	SummaryRedacted string
}

// UpsertExtractedMemoryFact is one M21-040..049 input: a deterministically
// extracted content value, the project it belongs to, and optionally the
// episode that observed it (M21-047) and the evidence that justifies
// promoting it.
type UpsertExtractedMemoryFact struct {
	ProjectID         domain.ProjectID
	Content           domain.MemoryArtifactContent
	DeterministicSeed string
	SupportingEpisode *domain.EpisodeID
	Evidence          *UpsertExtractedMemoryFactEvidence
}

// UpsertExtractedMemoryFactOutcome reports how one extracted observation
// was reconciled against existing project memory.
type UpsertExtractedMemoryFactOutcome struct {
	ArtifactID                 domain.MemoryArtifactID
	RevisionID                 domain.MemoryArtifactRevisionID
	Created                    bool
	Reconfirmed                bool
	InvalidatedPriorArtifactID *domain.MemoryArtifactID
	Maturity                   domain.MaturityState
}

// UpsertExtractedMemoryFact reconciles one deterministically extracted fact
// against existing project memory (M21-046..049):
//
//   - no live (candidate/validated/preferred-for-experiment) artifact
//     shares this content's normalized identity: mint a fresh artifact
//     identity (a "new fact");
//   - a live artifact shares the identity and its latest revision's content
//     is byte-identical: this is a reconfirmation, not a new fact -- no new
//     revision is created, but the observing episode and evidence are still
//     recorded, so M21-047's first-observed/last-confirmed/supporting-episode
//     tracking (read back via GetMemoryArtifactFactObservationSummary) stays
//     accurate;
//   - a live artifact shares the identity but its latest revision's content
//     differs: the fact this identity slot pointed to has changed -- M21-048
//     invalidates that revision (TransitionMemoryArtifactMaturity to
//     Invalidated, reason binding-changed) before a fresh artifact identity
//     is minted for the new content;
//   - the identity match is NOT live (its latest revision is already
//     quarantined/invalidated/retired -- domain.MaturityState.CanReachAuthority
//     false -- regardless of whether the newly observed content happens to
//     match it byte-for-byte): M21-049 -- a terminal fact is never silently
//     reconfirmed or resurrected under its old identity. A fresh artifact
//     identity is minted and must earn its own new evidence before it can
//     reach Validated again; domain.ReviseMemoryArtifactWithUserCorrection's
//     own refusal to correct a terminal revision in place
//     (ErrMemoryArtifactCorrectionRequiresNewIdentity) is the same rule this
//     function deliberately never tries to route around by calling
//     CreateMemoryArtifactCorrection instead of minting fresh.
//
// Every branch that reaches Candidate goes through the same authorization
// path as every other memory artifact: RecordMemoryArtifactSupportingEvidence
// + TransitionMemoryArtifactMaturity, which internally requires
// domain.AuthorizeMemoryArtifactMaturityGrant to find at least one
// corroborated, non-self-report record.
// input.Evidence.Source == domain.EvidenceSourceKindAgentSelfReport is
// rejected outright here, before any of that machinery runs, as an explicit
// extra guard on top of the structural one already in
// domain.SupportingEvidenceRecord.Validate.
//
// Known limitation: with no indexed identity-hash column (no migration was
// authorized this pass), two concurrent UpsertExtractedMemoryFact calls for
// the exact same never-before-seen observation can both observe "not found"
// and each mint a distinct fresh artifact identity, producing a duplicate.
// An indexed identity column with a UNIQUE constraint would close this race;
// see the M21-046 report.
func (repositories *Repositories) UpsertExtractedMemoryFact(
	ctx context.Context,
	input UpsertExtractedMemoryFact,
) (UpsertExtractedMemoryFactOutcome, error) {
	if input.ProjectID.IsZero() {
		return UpsertExtractedMemoryFactOutcome{}, errors.New("project ID must not be empty")
	}
	if strings.TrimSpace(input.DeterministicSeed) == "" {
		return UpsertExtractedMemoryFactOutcome{}, errors.New("deterministic extraction seed must not be empty")
	}
	if input.Evidence != nil && input.Evidence.Source == domain.EvidenceSourceKindAgentSelfReport {
		return UpsertExtractedMemoryFactOutcome{}, errors.New("agent self-report can never justify admitting an extracted memory fact")
	}
	if err := input.Content.Validate(); err != nil {
		return UpsertExtractedMemoryFactOutcome{}, err
	}
	contentRepositoryID, _, err := memoryArtifactContentBinding(input.Content)
	if err != nil {
		return UpsertExtractedMemoryFactOutcome{}, err
	}
	normalizedIdentity, err := domain.NormalizedMemoryFactIdentity(input.Content)
	if err != nil {
		return UpsertExtractedMemoryFactOutcome{}, err
	}
	_, contentSHA256, err := canonicalMemoryArtifactContent(input.Content)
	if err != nil {
		return UpsertExtractedMemoryFactOutcome{}, err
	}

	existing, found, err := findLatestMemoryArtifactRevisionByNormalizedIdentity(
		ctx, repositories.database.sql, input.ProjectID, contentRepositoryID, input.Content.Kind, normalizedIdentity,
	)
	if err != nil {
		return UpsertExtractedMemoryFactOutcome{}, err
	}

	var outcome UpsertExtractedMemoryFactOutcome
	scopeRepositoryID := &contentRepositoryID

	switch {
	case found && existing.Maturity.CanReachAuthority() && existing.ContentSHA256 == contentSHA256:
		// Reconfirmation of a still-live, unchanged fact (M21-047).
		outcome = UpsertExtractedMemoryFactOutcome{
			ArtifactID: existing.ArtifactID, RevisionID: existing.RevisionID,
			Reconfirmed: true, Maturity: existing.Maturity,
		}

	case found && existing.Maturity.CanReachAuthority():
		// M21-048: the live fact's content changed; invalidate it, then
		// mint a fresh identity for the new content.
		if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
			RevisionID: existing.RevisionID, From: existing.Maturity, To: domain.MaturityStateInvalidated,
			ReasonKind: MemoryArtifactInvalidationReasonBindingChanged,
			DetailRedacted: "a newly observed extraction for the same repository/kind identity slot carries " +
				"different content; the prior fact no longer reflects the current build/test/convention state",
			IdempotencyKey: "extract:" + input.DeterministicSeed + ":invalidate-prior:" + existing.RevisionID.String(),
		}); err != nil {
			return UpsertExtractedMemoryFactOutcome{}, err
		}
		invalidated := existing.ArtifactID
		created, err := repositories.mintFreshExtractedMemoryArtifact(ctx, input, scopeRepositoryID)
		if err != nil {
			return UpsertExtractedMemoryFactOutcome{}, err
		}
		outcome = UpsertExtractedMemoryFactOutcome{
			ArtifactID: created.ArtifactID, RevisionID: created.RevisionID, Created: true,
			Maturity: created.Maturity, InvalidatedPriorArtifactID: &invalidated,
		}

	default:
		// No identity match at all, or the match is already terminal
		// (M21-049: a terminal fact is never silently reconfirmed).
		created, err := repositories.mintFreshExtractedMemoryArtifact(ctx, input, scopeRepositoryID)
		if err != nil {
			return UpsertExtractedMemoryFactOutcome{}, err
		}
		outcome = UpsertExtractedMemoryFactOutcome{
			ArtifactID: created.ArtifactID, RevisionID: created.RevisionID, Created: true, Maturity: created.Maturity,
		}
	}

	if input.SupportingEpisode != nil {
		if err := repositories.RecordMemoryArtifactSupportingEpisode(ctx, outcome.ArtifactID, *input.SupportingEpisode); err != nil {
			return UpsertExtractedMemoryFactOutcome{}, err
		}
	}

	if input.Evidence != nil {
		maturity, err := repositories.recordExtractedFactEvidenceAndPromote(
			ctx, outcome.RevisionID, input.DeterministicSeed, *input.Evidence,
		)
		if err != nil {
			return UpsertExtractedMemoryFactOutcome{}, err
		}
		outcome.Maturity = maturity
	}

	return outcome, nil
}

// mintFreshExtractedMemoryArtifact always creates a brand-new memory
// artifact identity (never CreateMemoryArtifactCorrection, which
// domain.ReviseMemoryArtifactWithUserCorrection would refuse for a
// non-live prior revision anyway): the fresh identity starts at
// MaturityStateCandidate and inherits nothing from any prior artifact
// under the same normalized identity (M21-049).
func (repositories *Repositories) mintFreshExtractedMemoryArtifact(
	ctx context.Context,
	input UpsertExtractedMemoryFact,
	scopeRepositoryID *domain.RepositoryID,
) (MemoryArtifactRevisionRecord, error) {
	artifactID, err := domain.NewMemoryArtifactID()
	if err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	revisionID, err := domain.NewMemoryArtifactRevisionID()
	if err != nil {
		return MemoryArtifactRevisionRecord{}, err
	}
	return repositories.CreateMemoryArtifact(ctx, CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionID, ProjectID: input.ProjectID,
		ScopeRepositoryID: scopeRepositoryID, Content: input.Content,
		IdempotencyKey: "extract:" + input.DeterministicSeed + ":create:" + artifactID.String(),
	})
}

// recordExtractedFactEvidenceAndPromote mints the validations/evidence rows
// backing evidenceInput, links them to revisionID as a corroborated,
// non-self-report SupportingEvidenceRecord, and -- only when revisionID is
// currently Candidate -- attempts promotion to Validated. A revision that is
// already Validated/PreferredForExperiment is left untouched (this is a
// reconfirmation, not a re-promotion); GetMemoryArtifactRevision reads the
// real current maturity rather than assuming Candidate, so this is correct
// on every UpsertExtractedMemoryFact branch, not only a freshly minted one.
func (repositories *Repositories) recordExtractedFactEvidenceAndPromote(
	ctx context.Context,
	revisionID domain.MemoryArtifactRevisionID,
	deterministicSeed string,
	evidenceInput UpsertExtractedMemoryFactEvidence,
) (domain.MaturityState, error) {
	validationID, err := domain.NewValidationID()
	if err != nil {
		return "", err
	}
	if _, err := repositories.CreateValidation(ctx, CreateValidation{
		ID: validationID, TaskID: evidenceInput.TaskID, RunID: evidenceInput.RunID,
		State: domain.ValidationStatePassed, Severity: domain.ValidationSeverityAdvisory,
		ProfileName:     "deterministic-fact-extraction",
		SummaryRedacted: nonEmptyStringPointer(evidenceInput.SummaryRedacted),
	}); err != nil {
		return "", err
	}
	evidenceID, err := domain.NewEvidenceID()
	if err != nil {
		return "", err
	}
	if _, err := repositories.CreateEvidence(ctx, CreateEvidence{
		ID: evidenceID, ValidationID: validationID, TaskID: evidenceInput.TaskID,
		AssuranceLevel:  domain.AssuranceLevelRuntimeOnly,
		EvidenceType:    evidenceInput.EvidenceType,
		ContentHash:     evidenceInput.ContentHash,
		SummaryRedacted: evidenceInput.SummaryRedacted,
	}); err != nil {
		return "", err
	}
	if err := repositories.RecordMemoryArtifactSupportingEvidence(ctx, revisionID, domain.SupportingEvidenceRecord{
		Evidence: evidenceID, Source: evidenceInput.Source, Strength: domain.EvidenceStrengthCorroborated,
	}); err != nil {
		return "", err
	}
	current, err := repositories.GetMemoryArtifactRevision(ctx, revisionID)
	if err != nil {
		return "", err
	}
	if current.Maturity != domain.MaturityStateCandidate {
		return current.Maturity, nil
	}
	transitioned, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateValidated,
		IdempotencyKey: "extract:" + deterministicSeed + ":validate:" + revisionID.String(),
	})
	if err != nil {
		return "", err
	}
	return transitioned.To, nil
}

func nonEmptyStringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// findLatestMemoryArtifactRevisionByNormalizedIdentity scans the bounded
// set of not-deleted memory artifacts sharing kind and contentRepositoryID
// within project (all three already-indexed columns; no new schema),
// decoding each one's latest revision and comparing
// domain.NormalizedMemoryFactIdentity against normalizedIdentity (M21-046).
// See memoryFactExtractionScanLimit's doc comment for why this is a bounded
// scan rather than an indexed lookup.
func findLatestMemoryArtifactRevisionByNormalizedIdentity(
	ctx context.Context,
	queries memoryLineageQueryer,
	projectID domain.ProjectID,
	contentRepositoryID domain.RepositoryID,
	kind domain.MemoryArtifactKind,
	normalizedIdentity string,
) (MemoryArtifactRevisionRecord, bool, error) {
	// Ordered newest-artifact-first (by the latest revision's own
	// created_at_unix_micros, tie-broken by artifact_id): after an M21-048
	// invalidate-then-remint cycle, more than one artifact_id can
	// legitimately share the same normalized identity (an old, now-
	// invalidated one and its live successor). Returning on the first
	// identity match below must land on the newest such artifact, never an
	// arbitrary one, so a later reconfirmation always reconciles against the
	// current live successor (M21-049) instead of risking a match against
	// the terminal prior by undefined row order.
	//
	// memory_artifacts is filtered via a subquery, not a JOIN, because
	// memoryArtifactRevisionSelect's plain "id" column would otherwise
	// collide with memory_artifacts.id and make the SELECT ambiguous.
	rows, err := queries.QueryContext(
		ctx,
		memoryArtifactRevisionSelect+`
		 WHERE kind = ? AND content_repository_id = ?
		   AND artifact_id IN (
		     SELECT id FROM memory_artifacts WHERE project_id = ? AND deleted_at_unix_micros IS NULL
		   )
		   AND revision_number = (
		     SELECT MAX(latest.revision_number) FROM memory_artifact_revisions AS latest
		     WHERE latest.artifact_id = memory_artifact_revisions.artifact_id
		   )
		 ORDER BY created_at_unix_micros DESC, artifact_id DESC
		 LIMIT ?`,
		kind, contentRepositoryID, projectID, memoryFactExtractionScanLimit+1,
	)
	if err != nil {
		return MemoryArtifactRevisionRecord{}, false, classify("scan memory artifact revisions for normalized identity", err)
	}
	defer rows.Close()
	scanned := 0
	for rows.Next() {
		scanned++
		if scanned > memoryFactExtractionScanLimit {
			return MemoryArtifactRevisionRecord{}, false, fmt.Errorf(
				"%w: kind %q repository %s reaches at least %d live artifacts (bound %d)",
				ErrMemoryFactExtractionScopeTooLarge, kind, contentRepositoryID, scanned, memoryFactExtractionScanLimit,
			)
		}
		record, err := scanMemoryArtifactRevision(rows, "scan memory artifact revision for normalized identity")
		if err != nil {
			return MemoryArtifactRevisionRecord{}, false, err
		}
		identity, err := domain.NormalizedMemoryFactIdentity(record.Content)
		if err != nil {
			return MemoryArtifactRevisionRecord{}, false, err
		}
		if identity == normalizedIdentity {
			return record, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return MemoryArtifactRevisionRecord{}, false, classify("scan memory artifact revisions for normalized identity", err)
	}
	return MemoryArtifactRevisionRecord{}, false, nil
}

// -----------------------------------------------------------------------
// M21-047: first observed, last confirmed, supporting episode count
// -----------------------------------------------------------------------

// MemoryArtifactFactObservationSummary reports the M21-047 tracking fields
// for one memory artifact.
type MemoryArtifactFactObservationSummary struct {
	FirstObservedAt        time.Time
	LastConfirmedAt        time.Time
	SupportingEpisodeCount int
}

// GetMemoryArtifactFactObservationSummary computes the M21-047 summary for
// artifactID from memory_artifacts.created_at_unix_micros (first observed --
// creating an artifact IS itself the first observation) and
// memory_artifact_supporting_episode's real, per-episode
// recorded_at_unix_micros rows (RecordMemoryArtifactSupportingEpisode's
// backing store, read here rather than via a new column), taking the
// newest of the creation time and every recorded supporting-episode
// timestamp as "last confirmed."
func (repositories *Repositories) GetMemoryArtifactFactObservationSummary(
	ctx context.Context,
	artifactID domain.MemoryArtifactID,
) (MemoryArtifactFactObservationSummary, error) {
	if artifactID.IsZero() {
		return MemoryArtifactFactObservationSummary{}, errors.New("memory artifact ID must not be empty")
	}
	artifact, found, err := findMemoryArtifact(ctx, repositories.database.sql, artifactID)
	if err != nil {
		return MemoryArtifactFactObservationSummary{}, err
	}
	if !found {
		return MemoryArtifactFactObservationSummary{}, typedError(ErrNotFound, "get memory artifact fact observation summary", errors.New("memory artifact does not exist"))
	}
	lineage, err := repositories.GetMemoryArtifactLineage(ctx, artifactID)
	if err != nil {
		return MemoryArtifactFactObservationSummary{}, err
	}
	var lastConfirmedMicros sql.NullInt64
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT MAX(recorded_at_unix_micros) FROM memory_artifact_supporting_episode WHERE artifact_id = ?`, artifactID,
	).Scan(&lastConfirmedMicros); err != nil {
		return MemoryArtifactFactObservationSummary{}, classify("get memory artifact last confirmed", err)
	}
	lastConfirmed := artifact.CreatedAt
	if lastConfirmedMicros.Valid {
		if confirmed := repositoryTime(lastConfirmedMicros.Int64); confirmed.After(lastConfirmed) {
			lastConfirmed = confirmed
		}
	}
	return MemoryArtifactFactObservationSummary{
		FirstObservedAt:        artifact.CreatedAt,
		LastConfirmedAt:        lastConfirmed,
		SupportingEpisodeCount: len(lineage.SupportingEpisodes),
	}, nil
}
