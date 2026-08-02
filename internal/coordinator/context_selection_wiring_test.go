package coordinator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestAUDIT010_ARunSelectsContextAndPersistsItsManifest covers AUDIT-010,
// reconciling M08-043, M08-045, and M08-G03.
//
// workspace.SelectContext had no production caller. The agent's first round
// was a directory listing plus go.mod and README.md, no context manifest was
// ever written, and the expandable context card therefore had nothing to
// expand — while all three items were recorded complete.
func TestAUDIT010_ARunSelectsContextAndPersistsItsManifest(t *testing.T) {
	const requirement = "Update service/service.go so the handler reports its version."

	engine := startEngineFixture(t, &scriptedEngineModel{
		turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
			writeFile("service/version.go", "package service\n\nconst Version = \"1\"\n"),
		},
	})
	ctx := context.Background()

	// A file the requirement names, so selection has something to rank above
	// the rest of the tree.
	worktreeSeed := filepath.Join(engine.repository, "service")
	if err := os.MkdirAll(worktreeSeed, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeSeed, "service.go"),
		[]byte("package service\n\nfunc Handle() string { return \"ok\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitEverythingForTest(t, engine.repository)

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
		IdempotencyKey:           "audit010-requirement",
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
		"audit010-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "audit010-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	// The manifest is the evidence M08-043 claims. Before this wiring the
	// table stayed empty for every run.
	engine.waitFor(t, "the run to persist its context manifest", func() bool {
		return engine.rows("context_manifests") > 0
	})

	if engine.rows("context_manifest_items") == 0 {
		t.Error("a manifest was recorded with no items, so the context card has nothing to expand")
	}
}

// commitEverythingForTest stages and commits the worktree so the repository
// map's revision matches HEAD.
func commitEverythingForTest(t *testing.T, repository string) {
	t.Helper()
	for _, arguments := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=audit010@example.invalid",
			"-c", "user.name=Audit 010", "commit", "-m", "seed"},
	} {
		command := exec.CommandContext(t.Context(), "git", arguments...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
}
