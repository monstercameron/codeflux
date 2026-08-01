package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readDeveloperDocumentation(t *testing.T) string {
	t.Helper()
	root := repositoryRootForCommandGraph(t)
	source, err := os.ReadFile(filepath.Join(root, "docs", "developing.md"))
	if err != nil {
		t.Fatalf("docs/developing.md must exist and be tracked: %v", err)
	}
	return string(source)
}

// TestM22_128_DocumentationCoversEveryDiagnosticSurface covers M22-128.
func TestM22_128_DocumentationCoversEveryDiagnosticSurface(t *testing.T) {
	document := readDeveloperDocumentation(t)

	for _, section := range []string{
		"Failure artifacts", "Replaying a session",
		"Safe database inspection", "Profiling and diagnostics",
	} {
		if !strings.Contains(document, section) {
			t.Fatalf("docs/developing.md has no %q section", section)
		}
	}

	// Every replay control must be documented, or a developer cannot reproduce
	// the transport condition it exists for.
	for _, control := range []string{
		"StopAtSequence", "StepEvent", "DuplicateSequences",
		"GapSequences", "ReconnectAfterSequence", "SnapshotRepairAtSequence",
	} {
		if !strings.Contains(document, control) {
			t.Fatalf("docs/developing.md does not document the %q replay control", control)
		}
	}

	// Every inspectable entity must be named.
	for _, entity := range []string{
		"task", "run", "event", "approval", "checkpoint", "plan",
		"memory-artifact", "graph-revision",
	} {
		if !strings.Contains(document, entity) {
			t.Fatalf("docs/developing.md does not name the %q inspection entity", entity)
		}
	}

	// Every profile kind must be named, since the point of the section is that
	// a developer can find the one they need.
	for _, profile := range []string{"CPU", "heap", "goroutine", "mutex", "block"} {
		if !strings.Contains(document, profile) {
			t.Fatalf("docs/developing.md does not document the %s profile", profile)
		}
	}

	// The artifact location rule must be stated, because it is the rule most
	// easily broken by accident.
	if !strings.Contains(document, ".artifacts") {
		t.Fatal("docs/developing.md does not state where artifacts live")
	}
}

// TestM22_129_GoldenPathsCoverEveryDeclaredChangeKind covers M22-129.
func TestM22_129_GoldenPathsCoverEveryDeclaredChangeKind(t *testing.T) {
	document := readDeveloperDocumentation(t)

	if !strings.Contains(document, "Golden paths") {
		t.Fatal("docs/developing.md has no golden paths section")
	}
	for _, path := range []string{
		"Adding a backend use case",
		"Adding an event and its card",
		"Adding a frontend component",
		"Adding a graph projection",
		"Adding an atom",
		"Adding a migration",
		"Adding a provider",
	} {
		if !strings.Contains(document, path) {
			t.Fatalf("docs/developing.md has no golden path for %q", path)
		}
	}
}

// TestM22_130_GoldenPathsIdentifyPlanTodoLayerEventAndTransaction is M22-130.
//
// The requirement is that a clean contributor or agent session, given only
// this documentation, can identify the correct plan section, TODO, test layer,
// event, and transaction for a sample vertical change. This test performs that
// lookup mechanically: it extracts the golden path for adding an event and
// checks each of the five things is findable from the text alone, and that
// each thing named actually exists in the repository.
func TestM22_130_GoldenPathsIdentifyPlanTodoLayerEventAndTransaction(t *testing.T) {
	document := readDeveloperDocumentation(t)
	root := repositoryRootForCommandGraph(t)

	// 1. Plan section. The document must send a reader to docs/plan.md as the
	//    deciding authority before anything else.
	if !strings.Contains(document, "docs/plan.md") {
		t.Fatal("the documentation does not name docs/plan.md as the deciding authority")
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "plan.md")); err != nil {
		t.Fatalf("docs/plan.md is referenced but missing: %v", err)
	}

	// 2. TODO. The document must send a reader to TODOS.md, and must say what
	//    to do when no task exists — otherwise the reader invents scope.
	if !strings.Contains(document, "TODOS.md") {
		t.Fatal("the documentation does not name TODOS.md")
	}
	if !strings.Contains(document, "stop and add the task first") {
		t.Fatal("the documentation does not say what to do when no TODO covers the change")
	}

	// 3. Test layer. Every golden path must name its test layer explicitly.
	paths := goldenPathSections(t, document)
	if len(paths) < 7 {
		t.Fatalf("found %d golden paths, want at least 7", len(paths))
	}
	for name, body := range paths {
		if !strings.Contains(body, "Test layer:") {
			t.Fatalf("golden path %q does not name its test layer", name)
		}
	}

	// 4. Event. The event golden path must name the file where kinds are
	//    declared, and that file must actually declare them.
	eventPath, ok := paths["Adding an event and its card"]
	if !ok {
		t.Fatal("no golden path for adding an event")
	}
	if !strings.Contains(eventPath, "internal/events/session.go") {
		t.Fatal("the event golden path does not name where event kinds are declared")
	}
	kinds, err := os.ReadFile(filepath.Join(root, "internal", "events", "session.go"))
	if err != nil {
		t.Fatalf("read event kinds: %v", err)
	}
	if !strings.Contains(string(kinds), "Kind = \"") {
		t.Fatal("internal/events/session.go does not declare event kinds")
	}
	// The path must also send the reader to the projection, since a kind the
	// browser does not know is a kind the browser drops.
	if !strings.Contains(eventPath, "EventKinds()") {
		t.Fatal("the event golden path does not mention the session projection's kind list")
	}

	// 5. Transaction. The backend path must state the transaction rule, and
	//    the package it names must exist.
	backendPath, ok := paths["Adding a backend use case"]
	if !ok {
		t.Fatal("no golden path for adding a backend use case")
	}
	if !strings.Contains(backendPath, "One transaction per user-visible outcome") {
		t.Fatal("the backend golden path does not state the transaction rule")
	}
	for _, directory := range []string{
		"internal/domain", "internal/storage", "internal/coordinator", "internal/transport",
	} {
		if !strings.Contains(backendPath, directory) {
			t.Fatalf("the backend golden path does not name %s", directory)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(directory))); err != nil {
			t.Fatalf("golden path names %s, which does not exist: %v", directory, err)
		}
	}

	// The prohibited dependency must be restated where a reader will act on
	// it, not only in the plan.
	if !strings.Contains(backendPath, "must not import") {
		t.Fatal("the backend golden path does not restate the domain dependency prohibition")
	}
}

// goldenPathSections splits the golden paths out of the document by heading.
func goldenPathSections(t *testing.T, document string) map[string]string {
	t.Helper()
	start := strings.Index(document, "## Golden paths")
	if start < 0 {
		t.Fatal("no golden paths section")
	}
	end := strings.Index(document[start+1:], "\n## ")
	body := document[start:]
	if end >= 0 {
		body = document[start : start+1+end]
	}

	heading := regexp.MustCompile(`(?m)^### (.+)$`)
	matches := heading.FindAllStringSubmatchIndex(body, -1)
	sections := make(map[string]string, len(matches))
	for index, match := range matches {
		name := strings.TrimSpace(body[match[2]:match[3]])
		sectionEnd := len(body)
		if index+1 < len(matches) {
			sectionEnd = matches[index+1][0]
		}
		sections[name] = body[match[1]:sectionEnd]
	}
	return sections
}

// TestM22_130_DocumentationReferencesOnlyRealCommands proves a reader
// following the document does not hit a command that no longer exists.
func TestM22_130_DocumentationReferencesOnlyRealCommands(t *testing.T) {
	document := readDeveloperDocumentation(t)
	root := repositoryRootForCommandGraph(t)

	registry, err := os.ReadFile(filepath.Join(root, "cmd", "codeflux-dev", "registry.go"))
	if err != nil {
		t.Fatalf("read command registry: %v", err)
	}
	invocation := regexp.MustCompile(`go run \./cmd/codeflux-dev ([a-z-]+)`)
	matches := invocation.FindAllStringSubmatch(document, -1)
	if len(matches) < 5 {
		t.Fatalf("the documentation shows only %d commands", len(matches))
	}
	for _, match := range matches {
		if !strings.Contains(string(registry), `"`+match[1]+`"`) {
			t.Fatalf("the documentation shows %q, which is not a real command", match[1])
		}
	}
}
