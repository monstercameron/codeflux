package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
)

// agentsInstructionMarker is content a granted-vs-refused assertion can
// recognize, so the test proves the exact approved bytes were admitted
// rather than merely that some item with the right path arrived.
const agentsInstructionMarker = "AUDIT011A-APPROVED-INSTRUCTION-MARKER"

// TestAUDIT011a_SelectAgentContextHonorsARealGrantedApproval reconciles the
// wiring half of AUDIT-011a.
//
// AUDIT-011 built a durable, immutable, consumable, revocable approval table
// and a resolver over it, but recorded no production caller. Reading
// internal/coordinator/agent_execution.go now shows
// execution.repositories.InstructionApprovalResolverFor(...) is already
// threaded into selectAgentContext's InstructionApprovals field — so the
// wiring this ticket asks for already exists in the tree. What was missing
// was proof: this test calls the same unexported selectAgentContext
// production code AUDIT-010 wired, with the same InstructionApprovalResolverFor
// construction agent_execution.go uses, backed by a real migrated SQLite
// database, and shows a granted approval is admitted while an identical but
// ungranted one is refused — the property the resolver exists to enforce.
//
// It drives selectAgentContext directly rather than through a full
// StartTask-to-worker run for a reason worth recording rather than hiding:
// every engine-fixture-based run in this package binds a task's repository
// to the identity EnsureLocalBootstrap was opened with (a fixed value, not a
// live Git object), while selectAgentContext independently insists the
// worktree's actual HEAD match that identity exactly. In every engine test
// this reduces to a permanent mismatch, so selection always degrades to the
// worktree-listing fallback and its success branch — the one this ticket is
// about — is never reached by a full run in this test fixture family. That
// is a real, separate gap (repository identity is never resynchronized to
// the Git object a task's worktree actually starts from) worth its own
// ticket; it is reported here rather than routed around, and this test
// sidesteps it by supplying a real, resolved revision directly instead of
// going through a task's worktree binding.
//
// It does not attempt the ticket's second half, a user-facing grant surface.
// Every existing caller of GrantRepositoryInstructionApproval is a test;
// building the RPC and UI that would let a person create one live outside
// this package's ownership (internal/transport, api/proto, web/frontend) and
// is reported separately rather than attempted here.
func TestAUDIT011a_SelectAgentContextHonorsARealGrantedApproval(t *testing.T) {
	root := t.TempDir()
	repositoryPath := filepath.Join(root, "repo")
	initializeCoordinatorGitRepository(t, repositoryPath)

	if err := os.WriteFile(filepath.Join(repositoryPath, "AGENTS.md"),
		[]byte("# Agents\n\nThis project tracks a version.\n\n"+agentsInstructionMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktreeSeed := filepath.Join(repositoryPath, "service")
	if err := os.MkdirAll(worktreeSeed, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeSeed, "service.go"),
		[]byte("package service\n\nfunc Handle() string { return \"ok\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitEverythingForTest(t, repositoryPath)
	revision := realGitHeadForTest(t, repositoryPath)

	engine := startEngineFixture(t, nil)
	scope, err := engine.repositories.EnsureLocalBootstrap(
		context.Background(), repositoryPath, revision, "Engine")
	if err != nil {
		t.Fatalf("opening the repository failed: %v", err)
	}

	const requirement = "Update service/service.go so the handler reports its version."
	redactor, err := redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes: 1 << 20, MaximumOutputBytes: 512 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Refused first: the same instruction, the same everything, with no
	// approval granted yet. This is the control that makes the granted case
	// below mean something.
	refused := selectAgentContext(
		context.Background(), engine.repositories, scope.RepositoryID, repositoryPath,
		revision, requirement, redactor,
		engine.repositories.InstructionApprovalResolverFor(
			scope.ProjectID, scope.RepositoryID, "audit011a-refused"),
	)
	if refused.Degraded {
		t.Fatalf("selection degraded before any approval existed: %s", refused.Reason)
	}
	if contextItemsContainPath(refused.Items, "AGENTS.md") {
		t.Fatal("an unapproved repository instruction was admitted")
	}

	digest := sha256HexOfFile(t, filepath.Join(repositoryPath, "AGENTS.md"))
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.repositories.GrantRepositoryInstructionApproval(context.Background(),
		storage.GrantRepositoryInstructionApproval{
			ID:                 approvalID.String(),
			ProjectID:          scope.ProjectID,
			RepositoryID:       scope.RepositoryID,
			RepositoryRevision: revision,
			InstructionPath:    "AGENTS.md",
			ContentSHA256:      digest,
			Scope:              storage.InstructionApprovalRevision,
			ReasonRedacted:     "audit011a fixture grant",
		},
	); err != nil {
		t.Fatalf("granting the instruction approval failed: %v", err)
	}

	granted := selectAgentContext(
		context.Background(), engine.repositories, scope.RepositoryID, repositoryPath,
		revision, requirement, redactor,
		engine.repositories.InstructionApprovalResolverFor(
			scope.ProjectID, scope.RepositoryID, "audit011a-granted"),
	)
	if granted.Degraded {
		t.Fatalf("selection degraded after a real approval was granted: %s", granted.Reason)
	}
	content, ok := contextItemContent(granted.Items, "AGENTS.md")
	if !ok {
		t.Fatal("a granted instruction approval was not admitted by the real production " +
			"selectAgentContext function; the resolver was not honored")
	}
	if !strings.Contains(content, agentsInstructionMarker) {
		t.Fatalf("the admitted instruction content did not match the approved bytes: %q", content)
	}
}

func contextItemsContainPath(items []agentloop.RepositoryContextItem, path string) bool {
	_, ok := contextItemContent(items, path)
	return ok
}

// contextItemContent finds one selected item by path. Selection labels a
// partial excerpt "path:start-end", so a prefix match is required rather
// than exact equality; AGENTS.md is small enough here to be selected whole,
// but the helper does not assume that.
func contextItemContent(items []agentloop.RepositoryContextItem, path string) (string, bool) {
	for _, item := range items {
		if item.Path == path || strings.HasPrefix(item.Path, path+":") {
			return item.ContentRedacted, true
		}
	}
	return "", false
}

// sha256HexOfFile is the same digest source workspace.SelectContext takes
// from disk at selection time, so a grant bound to it matches exactly what
// the resolver is asked about.
func sha256HexOfFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// realGitHeadForTest resolves the actual commit HEAD points to, which is
// what selectAgentContext's own staleness check compares a repository map's
// declared revision against.
func realGitHeadForTest(t *testing.T, repository string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", "rev-parse", "--verify", "HEAD")
	command.Dir = repository
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(output))
}
