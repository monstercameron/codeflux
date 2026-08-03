package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
)

// enrichVerifiedWork adds the registry documentation and commits the result, or
// leaves the run exactly as it found it.
//
// The sequence is the point. Enrichment used to edit the verified worktree,
// revalidate, and stop — which left the tree differing from every recorded
// artifact and from the checkpoint digest the run then reported as its verified
// revision. Ladder rung 5 ended with a correct 7,593-byte program on disk, an
// artifact record holding the version before enrichment, and a terminal record
// saying "the worktree is NOT verified revision 8bef148ac7cd". All three were
// accurate and the run failed anyway, because the only thing anyone could read
// back was the stale copy.
//
//	verified checkpoint
//	    ↓ enrich
//	build → test → acceptance
//	    ↓ persist the artifacts
//	    ↓ capture a new verified digest
//	    └─ any step fails → put the checkpoint back, record nothing
//
// Reports how many atoms were documented, and zero when nothing was committed.
func (execution *AgentExecution) enrichVerifiedWork(
	ctx context.Context,
	scope agentScope,
	checkpoint *verifiedCheckpoint,
) int {
	produced, err := readProducedFunctions(scope.worktree)
	if err != nil {
		return 0
	}
	documented, deriveErr := deriveAtomDocumentation(scope.worktree, produced)
	if documented == 0 {
		return 0
	}
	if deriveErr != nil {
		checkpoint.restore(scope.worktree)
		tracef("enrich", "documenting failed (%v); reverted to %s",
			deriveErr, checkpoint.digest)
		return 0
	}
	// Everything that made the checkpoint a checkpoint, asked again of the tree
	// that now exists. A comment cannot change behaviour, which is exactly why
	// a comment that does is worth catching.
	if held, why := revalidateAfterWrite(ctx, scope.worktree); !held {
		checkpoint.restore(scope.worktree)
		tracef("enrich", "documenting %d atom(s) broke the work (%s); "+
			"reverted to %s", documented, firstMeaningfulLine(why),
			checkpoint.digest)
		return 0
	}
	examples := parseAcceptanceExamples(scope.requirement)
	if _, failures := execution.checkAcceptance(
		ctx, scope.worktree, scope.requirement, examples,
	); len(failures) > 0 {
		checkpoint.restore(scope.worktree)
		tracef("enrich", "documenting %d atom(s) broke acceptance (%s); "+
			"reverted to %s", documented, failures[0], checkpoint.digest)
		return 0
	}

	// The bytes on disk are now the bytes a reader must be able to fetch. An
	// artifact record that lags the worktree is worse than none: it answers,
	// and answers with a program nobody is running.
	if stored := execution.recordEnrichedSource(ctx, scope); stored == 0 {
		checkpoint.restore(scope.worktree)
		tracef("enrich", "the enriched source could not be recorded; "+
			"reverted to %s", checkpoint.digest)
		return 0
	}
	// A new verified revision, because this one is verified: it was built,
	// tested and accepted a few lines above. Reporting the pre-enrichment digest
	// would describe a tree that no longer exists.
	if err := checkpoint.capture(scope.worktree,
		"compiled, tests passed, acceptance matched, and its atoms are "+
			"documented for the registry"); err != nil {
		tracef("enrich", "the enriched revision could not be checkpointed: %v", err)
	}
	tracef("enrich", "documented %d atom(s) from measured facts; built, "+
		"tested, accepted, recorded, and checkpointed as %s",
		documented, checkpoint.digest)
	return documented
}

// recordEnrichedSource stores every produced Go file as it now stands.
//
// Written through the same redaction pipeline as the model's own writes, and
// recorded as its own artifact type so a reader can tell a file the coordinator
// documented from one the model wrote. Reports how many files were stored.
func (execution *AgentExecution) recordEnrichedSource(
	ctx context.Context, scope agentScope,
) int {
	if execution.repositories == nil {
		return 0
	}
	files, err := producedGoFiles(scope.worktree)
	if err != nil {
		return 0
	}
	stored := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		content, readErr := os.ReadFile(
			filepath.Join(scope.worktree, filepath.FromSlash(file)))
		if readErr != nil {
			continue
		}
		sanitized := execution.redactForStorage(content)
		taskID := scope.taskID
		repositoryID := scope.repositoryID
		if _, writeErr := execution.repositories.RecordProducedArtifact(
			ctx, storage.RecordArtifact{
				ProjectID: scope.projectID, RepositoryID: &repositoryID,
				TaskID: &taskID, Type: "generated-source-enriched",
				Path: file, Content: sanitized,
				MediaType:    storage.ArtifactMediaType(file),
				StorageClass: storage.ArtifactPermanentSemantic,
			},
		); writeErr != nil {
			execution.say(ctx, scope, events.KindMessageFinal,
				"The documented source could not be stored for later reading: "+
					writeErr.Error())
			return 0
		}
		stored++
	}
	return stored
}
