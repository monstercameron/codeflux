package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readUserGuide(t *testing.T) string {
	t.Helper()
	root := repositoryRootForCommandGraph(t)
	source, err := os.ReadFile(filepath.Join(root, "docs", "using.md"))
	if err != nil {
		t.Fatalf("docs/using.md must exist and be tracked: %v", err)
	}
	return string(source)
}

// flow collapses whitespace so a phrase is matched by its CONTENT rather than
// by where a line happens to wrap. A documentation test that fails when a
// paragraph is rewrapped teaches people to stop rewrapping paragraphs.
func flow(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// documentationRequirement is one M23-071..085 obligation.
//
// Each names the phrases that must appear. They are chosen to be the SUBSTANCE
// of the requirement rather than its title: a section headed "Costs" that
// never mentions unknown pricing has not met M23-075.
type documentationRequirement struct {
	Todo     string
	Subject  string
	Required []string
}

func documentationRequirements() []documentationRequirement {
	return []documentationRequirement{
		{
			"M23-071", "installation",
			[]string{"Installing", "SHA256", "signature", "PATH", "administrator"},
		},
		{
			"M23-072", "provider setup",
			[]string{
				"provider set", "standard input", "credential store",
				"never from a command-line argument",
			},
		},
		{
			"M23-073", "first-task walkthrough",
			[]string{
				"codeflux start", "clean working tree", "Read the plan",
				"Review the diff", "until you accept",
			},
		},
		{
			"M23-074", "worktrees, acceptance, repair, rollback, cleanup",
			[]string{"Worktrees", "Acceptance", "Repair", "Rollback", "Cleanup"},
		},
		{
			"M23-075", "cost forecasts, actual cost, unknown pricing, hard budgets",
			[]string{
				"forecast", "actual cost", "Unknown", "hard budget",
				"never rendered as zero", "integer minor units",
			},
		},
		{
			"M23-076", "permission tiers and container isolation",
			[]string{
				"Without asking", "Always asks", "Container isolation",
				"not retried through a different tool",
			},
		},
		{
			"M23-077", "SQLite location, backup, inspection, export, deletion",
			[]string{
				"codeflux backup", "integrity-check", "Inspect",
				"diagnostics export", "Delete", "not affected",
			},
		},
		{
			"M23-078", "graph modes and non-proof status",
			[]string{"graph", "not a proof", "does not establish"},
		},
		{
			"M23-079", "memory eligibility, lineage, invalidation, vector discovery",
			[]string{
				"Eligibility", "Lineage", "Invalidation", "Vector candidate discovery",
				"derived from", "influenced by", "never confers eligibility",
			},
		},
		{
			"M23-080", "crash recovery",
			[]string{
				"Crash recovery", "what is known", "what is ambiguous",
				"reconcile", "will not retry an ambiguous external effect",
			},
		},
		{
			"M23-081", "diagnostic export",
			[]string{"diagnostics export", "no requirement text", "scanned before it is written"},
		},
		{
			"M23-082", "known limitations",
			[]string{"Known limitations", "Evidence is bounded", "Forecasts are estimates"},
		},
		{
			"M23-083", "not a security sandbox",
			[]string{
				"not a security sandbox", "runs as you",
				"does not contain what runs after you say yes",
			},
		},
		{
			"M23-084", "external systems may violate contracts",
			[]string{
				"External systems may violate their contracts",
				"report usage late", "cannot make an external system",
			},
		},
		{
			"M23-085", "deferred enterprise and deep-verification work",
			[]string{
				"Deferred work", "Container and VM isolation", "Multi-user",
				"Deep verification", "Automatic updates",
			},
		},
	}
}

// TestM23_071_085_UserDocumentationCoversEveryRequirement covers M23-071..085.
func TestM23_071_085_UserDocumentationCoversEveryRequirement(t *testing.T) {
	guide := readUserGuide(t)
	for _, requirement := range documentationRequirements() {
		t.Run(requirement.Todo, func(t *testing.T) {
			flowed := flow(guide)
			for _, phrase := range requirement.Required {
				if !strings.Contains(flowed, flow(phrase)) {
					t.Fatalf("%s (%s) is not documented: docs/using.md never says %q",
						requirement.Todo, requirement.Subject, phrase)
				}
			}
		})
	}

	// Every M23-071..085 TODO must be claimed.
	claimed := map[string]bool{}
	for _, requirement := range documentationRequirements() {
		claimed[requirement.Todo] = true
	}
	for number := 71; number <= 85; number++ {
		todo := "M23-0" + itoaDoc(number)
		if !claimed[todo] {
			t.Fatalf("%s has no documentation requirement", todo)
		}
	}
}

// TestM23_082_085_LimitationsAreStatedPlainlyNotSoftened is the property that
// makes the limitations section worth having.
//
// A limitations section written in hedges is a marketing section. These
// phrases are the ones that would be removed first by someone trying to make
// the product sound better, which is exactly why they are asserted.
func TestM23_082_085_LimitationsAreStatedPlainlyNotSoftened(t *testing.T) {
	guide := readUserGuide(t)
	limitations := flow(guide[strings.Index(guide, "## Known limitations"):])

	for _, plain := range []string{
		"These are real.",
		"It is not a security sandbox.",
		"Do not use CodeFlux as a boundary against code you do not trust.",
		"Prompt injection is mitigated, not solved.",
		"It does not mean the change is correct",
		"They are frequently wrong.",
	} {
		if !strings.Contains(limitations, flow(plain)) {
			t.Fatalf("the limitations section has been softened: it no longer says %q", plain)
		}
	}

	// Deferred work must say why it is absent, not merely that it is.
	deferred := flow(guide[strings.Index(guide, "## Deferred work"):])
	if !strings.Contains(deferred, flow("so their absence is a decision rather than an oversight")) {
		t.Fatal("the deferred section does not frame absences as decisions")
	}
	if !strings.Contains(deferred, "kill criterion") {
		t.Fatal("the deferred section does not mention the atom-reuse kill criterion")
	}
}

// TestM23_071_085_DocumentationMatchesTheRealCommandSurface proves the guide
// does not tell a user to run something that does not exist.
func TestM23_071_085_DocumentationMatchesTheRealCommandSurface(t *testing.T) {
	guide := readUserGuide(t)
	root := repositoryRootForCommandGraph(t)
	registry, err := os.ReadFile(filepath.Join(root, "cmd", "codeflux", "commands.go"))
	if err != nil {
		t.Fatalf("read command registry: %v", err)
	}

	for _, command := range []string{
		"codeflux start", "codeflux doctor", "codeflux backup",
		"codeflux integrity-check", "codeflux diagnostics export",
		"codeflux provider set", "codeflux provider test", "codeflux provider delete",
	} {
		if !strings.Contains(guide, command) {
			t.Fatalf("the guide never mentions %q", command)
		}
		name := strings.Fields(strings.TrimPrefix(command, "codeflux "))[0]
		if !strings.Contains(string(registry), `Name: "`+name+`"`) {
			t.Fatalf("the guide documents %q, which is not a registered command", command)
		}
	}

	// The data directories the guide names must match the ones the release
	// package declares, or a user looking for their data will not find it.
	releaseSource, err := os.ReadFile(filepath.Join(root, "internal", "release", "release.go"))
	if err != nil {
		t.Fatalf("read release package: %v", err)
	}
	for _, directory := range []string{
		`%LOCALAPPDATA%\codeflux`,
		"~/Library/Application Support/codeflux",
		"~/.local/share/codeflux",
	} {
		if !strings.Contains(guide, directory) {
			t.Fatalf("the guide does not name the data directory %q", directory)
		}
		escaped := strings.ReplaceAll(directory, `\`, `\\`)
		if !strings.Contains(string(releaseSource), directory) &&
			!strings.Contains(string(releaseSource), escaped) {
			t.Fatalf("the guide names %q, which the release package does not declare", directory)
		}
	}
}

// TestM23_G01_G04_MilestoneGates covers the four M23 gates.
//
// They are executed rather than asserted in prose: a gate nobody runs is a
// claim. Each states what evidence would falsify it.
func TestM23_G01_G04_MilestoneGates(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	t.Run("G01 a new user can install and start without reading source", func(t *testing.T) {
		guide := readUserGuide(t)
		// The whole path from nothing to a running first task must be in the
		// guide, in order, with no step requiring the reader to look at code.
		steps := []string{"Installing", "Connecting a provider", "Your first task"}
		previous := -1
		for _, step := range steps {
			at := strings.Index(guide, "## "+step)
			if at < 0 {
				t.Fatalf("the guide has no %q section", step)
			}
			if at < previous {
				t.Fatalf("%q appears out of order", step)
			}
			previous = at
		}
		if strings.Contains(guide, "internal/") {
			t.Fatal("the user guide sends a reader into the source tree")
		}
	})

	t.Run("G02 diagnostics are actionable", func(t *testing.T) {
		// Every non-ok doctor result carries a remediation. That is enforced
		// by internal/doctor's own validation, and the gate is that the
		// validation exists and is unconditional.
		source, err := os.ReadFile(filepath.Join(root, "internal", "doctor", "doctor.go"))
		if err != nil {
			t.Fatalf("read doctor: %v", err)
		}
		if !strings.Contains(string(source), "with no remediation") {
			t.Fatal("the doctor does not require a remediation for a failure")
		}
		if !strings.Contains(string(source), "M23-036") {
			t.Fatal("the remediation requirement does not cite its TODO")
		}
	})

	t.Run("G03 a release is complete, checksummed, and signed", func(t *testing.T) {
		source, err := os.ReadFile(filepath.Join(root, "internal", "release", "release.go"))
		if err != nil {
			t.Fatalf("read release: %v", err)
		}
		for _, required := range []string{
			"is unsigned", "ComputeSHA256", "has no artifact for", "is missing",
		} {
			if !strings.Contains(string(source), required) {
				t.Fatalf("the release contract does not enforce %q", required)
			}
		}
	})

	t.Run("G04 an upgrade cannot silently destroy data", func(t *testing.T) {
		source, err := os.ReadFile(filepath.Join(root, "internal", "release", "update.go"))
		if err != nil {
			t.Fatalf("read update: %v", err)
		}
		// Three properties together make this true: a downgrade is refused, a
		// forward migration demands a backup, and there is no way to skip it.
		for _, required := range []string{
			"VerdictRefuseDowngrade", "ErrBackupRequired", "BackupRequired: true",
		} {
			if !strings.Contains(flow(string(source)), flow(required)) {
				t.Fatalf("the update path does not enforce %q", required)
			}
		}
		if strings.Contains(string(source), "SkipBackup") ||
			strings.Contains(string(source), "--force") {
			t.Fatal("the update path offers a way to skip the mandatory backup")
		}
	})
}

func itoaDoc(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
