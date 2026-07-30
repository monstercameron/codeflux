package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckInstructionFilesAcceptsAuthoritativeAndThinPair(t *testing.T) {
	root := t.TempDir()
	writeInstructionFixture(t, root)
	tracked := []string{"AGENTS.md", "CLAUDE.md", "TODOS.md", "docs/plan.md", "testdata/instructions/smoke-valid.txt"}

	if err := checkInstructionFiles(root, tracked); err != nil {
		t.Fatalf("checkInstructionFiles() error = %v", err)
	}
}

func TestCheckInstructionFilesRequiresBothTrackedFiles(t *testing.T) {
	root := t.TempDir()
	writeInstructionFixture(t, root)

	err := checkInstructionFiles(root, []string{"AGENTS.md"})
	if err == nil || !strings.Contains(err.Error(), "CLAUDE.md") {
		t.Fatalf("missing tracked CLAUDE.md error = %v", err)
	}
}

func TestCheckInstructionFilesRejectsMissingPointerAndWeakenedMarkdownRule(t *testing.T) {
	root := t.TempDir()
	writeInstructionFixture(t, root)
	tracked := []string{"AGENTS.md", "CLAUDE.md", "TODOS.md", "docs/plan.md", "testdata/instructions/smoke-valid.txt"}

	claudePath := filepath.Join(root, "CLAUDE.md")
	content, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, claudePath, strings.ReplaceAll(string(content), "TODOS.md", "tasks.txt"))
	if err := checkInstructionFiles(root, tracked); err == nil || !strings.Contains(err.Error(), "TODOS.md") {
		t.Fatalf("missing pointer error = %v", err)
	}

	writeInstructionFixture(t, root)
	agentPath := filepath.Join(root, "AGENTS.md")
	content, err = os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, agentPath, strings.ReplaceAll(string(content), "Never create a new Markdown file unless the user explicitly requests that specific file.", "Prefer not to create Markdown."))
	if err := checkInstructionFiles(root, tracked); err == nil || !strings.Contains(err.Error(), "Markdown-creation") {
		t.Fatalf("weakened Markdown rule error = %v", err)
	}
}

func TestCheckRepositoryRelativeMarkdownLinksRejectsMissingAndEscapingTargets(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "existing.txt"), "fixture")

	if err := checkRepositoryRelativeMarkdownLinks(root, "AGENTS.md", []byte("[ok](existing.txt)")); err != nil {
		t.Fatalf("valid link: %v", err)
	}
	if err := checkRepositoryRelativeMarkdownLinks(root, "AGENTS.md", []byte("[missing](missing.txt)")); err == nil {
		t.Fatal("missing repository link was accepted")
	}
	if err := checkRepositoryRelativeMarkdownLinks(root, "AGENTS.md", []byte("[escape](../outside.txt)")); err == nil {
		t.Fatal("escaping repository link was accepted")
	}
}

func TestValidateInstructionSmokeScenarioRequiresPlanAndTodoBeforeEdit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "docs", "plan.md"), "# Plan")
	writeTestFile(t, filepath.Join(root, "TODOS.md"), "- [ ] `M01-065 TEST` fixture")

	valid := "PLAN: docs/plan.md#fixture\nTODO: M01-065\nEDIT: testdata/fixture.txt\n"
	if err := validateInstructionSmokeScenario(root, valid); err != nil {
		t.Fatalf("valid smoke scenario: %v", err)
	}
	invalid := "EDIT: testdata/fixture.txt\nPLAN: docs/plan.md#fixture\nTODO: M01-065\n"
	if err := validateInstructionSmokeScenario(root, invalid); err == nil {
		t.Fatal("edit before plan and TODO was accepted")
	}
}

func writeInstructionFixture(t *testing.T, root string) {
	t.Helper()
	agent := "# Agent\n\n" +
		"Never create a new Markdown file unless the user explicitly requests that specific file.\n\n" +
		"[reference](" + pinnedKarpathyReference + ") is community-maintained and not an official Andrej Karpathy repository.\n"
	claude := "[agents](AGENTS.md) [plan](docs/plan.md) [todos](TODOS.md)\n\n" +
		"[reference](" + pinnedKarpathyReference + ") is community-maintained and not an official Andrej Karpathy repository.\n"
	writeTestFile(t, filepath.Join(root, "AGENTS.md"), agent)
	writeTestFile(t, filepath.Join(root, "CLAUDE.md"), claude)
	writeTestFile(t, filepath.Join(root, "docs", "plan.md"), "# Plan")
	writeTestFile(t, filepath.Join(root, "TODOS.md"), "- [ ] `M01-065 TEST` fixture")
	writeTestFile(t, filepath.Join(root, "testdata", "instructions", "smoke-valid.txt"), "PLAN: docs/plan.md#fixture\nTODO: M01-065\nEDIT: testdata/instructions/target.fixture\n")
}
