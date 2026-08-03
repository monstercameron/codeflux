package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestAUDIT011b_AFullRunResynchronisesGitIdentityAndReachesContextSelection
// reconciles AUDIT-011b.
//
// EnsureLocalBootstrap binds a repository's git_identity once, at open, and
// nothing ever updated it again. Every engine fixture in this package opens
// its repository with a fixed, non-Git value — startEngineWith uses
// strings.Repeat("f", 40) — the same shape a real first-run coordinator binds
// to whatever HEAD happened to be at that moment. workspace.SelectContext
// separately insists a repository map's declared revision match the real Git
// HEAD of the worktree it is running against exactly. Because nothing ever
// resynchronised the stored identity to the revision a task's worktree was
// actually created from, that comparison failed on every single launch, and
// selectAgentContext's success branch — the one AUDIT-010 wired a production
// caller for — was unreachable through a full StartTask run.
//
// This test proves both halves of the fix through the real production path
// rather than by calling the resync function directly: it starts an engine
// fixture the same broken way every other fixture in this package does, runs
// a task through the full requirement-to-started-run journey, and checks
//   - the repository's stored identity actually changes away from the bogus
//     bootstrap value to the real Git revision the run's worktree was
//     created from (the resynchronisation itself, observed through storage
//     rather than asserted against the internal function that performs it);
//   - a context manifest is durably recorded, which selectAgentContext only
//     ever does after workspace.SelectContext succeeds — the fallback path
//     records nothing, so a manifest existing is proof the success branch,
//     not the degraded one, produced it;
//   - the run's own narration never reports falling back to the worktree
//     listing, which is the exact, visible symptom this ticket describes.
func TestAUDIT011b_AFullRunResynchronisesGitIdentityAndReachesContextSelection(t *testing.T) {
	const requirement = "Update service/service.go so the handler reports its version."

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("service/version.go", "package service\n\nconst Version = \"1\"\n"),
		},
	})
	ctx := context.Background()

	thread, err := engine.repositories.GetThread(ctx, engine.threadID)
	if err != nil {
		t.Fatalf("read the fixture thread: %v", err)
	}
	repositoryID := thread.RepositoryID

	before, err := engine.repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		t.Fatalf("read the repository before the run: %v", err)
	}
	// This is the exact defect condition: the fixture opens the repository
	// the same way every other test in this package does, with a fixed
	// identity that is not, and will never become, a real Git revision on
	// its own.
	const bootstrapIdentity = "ffffffffffffffffffffffffffffffffffffffff"
	if before.GitIdentity != bootstrapIdentity {
		t.Fatalf("fixture precondition changed: git_identity = %q, want the fixed "+
			"bootstrap value %q; this test needs the known-stale starting state to "+
			"prove resynchronisation happened", before.GitIdentity, bootstrapIdentity)
	}

	// A file the requirement names, so selection has something to rank above
	// the rest of the tree, committed before the task starts so the
	// worktree's real HEAD is a genuine, resolvable Git revision distinct
	// from the bootstrap identity.
	worktreeSeed := filepath.Join(engine.repository, "service")
	if err := os.MkdirAll(worktreeSeed, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeSeed, "service.go"),
		[]byte("package service\n\nfunc Handle() string { return \"ok\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitEverythingForTest(t, engine.repository)
	realHead := realGitHeadForTest(t, engine.repository)

	requestID := engine.request(t, requirement)
	created, err := engine.lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 engine.threadID,
		RequestMessageID:         &requestID,
		Requirement:              requirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("1", 40),
		BaselineModelRevision:    "scripted-provider-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"service"},
		IdempotencyKey:           "audit011b-requirement",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}
	readyRevision := driveTaskToReady(t, engine.repositories, created.TaskID, created.Revision)
	preflight, err := engine.application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"audit011b-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "audit011b-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	// The manifest is only ever recorded from selectAgentContext's success
	// branch (recordSelectedContext runs after workspace.SelectContext
	// returns without error; every failure path returns through fallback
	// before reaching it). A manifest existing is therefore proof the
	// success branch ran, not merely that a run happened.
	engine.waitFor(t, "the run to reach context selection's success branch", func() bool {
		return engine.rows("context_manifests") > 0
	})
	if engine.rows("context_manifest_items") == 0 {
		t.Error("a manifest was recorded with no items, so selection did not actually choose content")
	}

	narration := engine.narration()
	if strings.Contains(narration, "repository map revision is stale") {
		t.Errorf("the run hit the exact defect this ticket closes:\n%s", narration)
	}
	if strings.Contains(narration, "Planning against the worktree listing") {
		t.Errorf("the run degraded to the worktree-listing fallback instead of reaching "+
			"selection's success branch:\n%s", narration)
	}

	after, err := engine.repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		t.Fatalf("read the repository after the run: %v", err)
	}
	if after.GitIdentity == bootstrapIdentity {
		t.Fatal("git_identity is still the bogus bootstrap value after a full run created " +
			"a task worktree; resolveWorktree resolved a real HEAD and never wrote it back")
	}
	if after.GitIdentity != realHead {
		t.Fatalf("git_identity = %q after the run, want the worktree's real base revision %q",
			after.GitIdentity, realHead)
	}
}
