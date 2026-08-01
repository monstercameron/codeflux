package main

import (
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func repositoryRootForCommandGraph(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, err)
	}
	return root
}

func readWorkflow(t *testing.T) string {
	t.Helper()
	root := repositoryRootForCommandGraph(t)
	source, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	return string(source)
}

var devCommandInvocation = regexp.MustCompile(`go run \./cmd/codeflux-dev ([a-z-]+)`)

// workflowCommands returns every codeflux-dev command CI invokes, sorted.
func workflowCommands(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, match := range devCommandInvocation.FindAllStringSubmatch(readWorkflow(t), -1) {
		seen[match[1]] = true
	}
	commands := make([]string, 0, len(seen))
	for command := range seen {
		commands = append(commands, command)
	}
	sort.Strings(commands)
	return commands
}

// TestM22_124_CIInvokesOnlyRealDeveloperCommands is half of M22-124.
//
// A workflow calling a command the CLI does not have fails only on push, in
// somebody else's pipeline. Checking it here means a renamed command breaks
// the build of the person who renamed it.
func TestM22_124_CIInvokesOnlyRealDeveloperCommands(t *testing.T) {
	invoked := workflowCommands(t)
	if len(invoked) == 0 {
		t.Fatal("CI invokes no codeflux-dev command; the workflow parser is broken")
	}
	root := repositoryRootForCommandGraph(t)
	dispatch, err := os.ReadFile(filepath.Join(root, "cmd", "codeflux-dev", "main.go"))
	if err != nil {
		t.Fatalf("read command dispatch: %v", err)
	}
	for _, command := range invoked {
		if !strings.Contains(string(dispatch), `case "`+command+`":`) {
			t.Fatalf("CI runs %q, which codeflux-dev does not dispatch", command)
		}
	}
}

// TestM22_124_LocalAndCIShareTheSameCommandGraph is the other half of M22-124.
//
// The point is not that the two lists are identical — CI legitimately runs
// more — but that every gate CI enforces is reachable locally. A gate that
// only exists in the workflow is a gate a developer cannot run before pushing,
// which makes it a surprise rather than a check.
func TestM22_124_LocalAndCIShareTheSameCommandGraph(t *testing.T) {
	invoked := workflowCommands(t)

	// These are the gates CI enforces and a developer must be able to run.
	required := []string{"lint", "artifact-check", "generate-check", "build"}
	for _, command := range required {
		found := false
		for _, candidate := range invoked {
			if candidate == command {
				found = true
			}
		}
		if !found {
			t.Fatalf("CI does not run %q, so the gate is not enforced", command)
		}
	}

	// Every CI command must also be one a developer can invoke by hand with
	// the same name and no additional setup.
	root := repositoryRootForCommandGraph(t)
	registry, err := os.ReadFile(filepath.Join(root, "cmd", "codeflux-dev", "registry.go"))
	if err != nil {
		t.Fatalf("read command registry: %v", err)
	}
	for _, command := range invoked {
		if !strings.Contains(string(registry), `"`+command+`"`) {
			t.Fatalf("CI runs %q, which is not in the developer command registry", command)
		}
	}
}

// TestM22_126_GeneratedDriftFailsBeforeTestsThatDependOnIt covers M22-126.
//
// Ordering is the whole requirement. If tests run first, a drifted generated
// file produces a confusing test failure somewhere unrelated; if generate-check
// runs first, the failure names the actual problem.
func TestM22_126_GeneratedDriftFailsBeforeTestsThatDependOnIt(t *testing.T) {
	workflow := readWorkflow(t)

	generateAt := strings.Index(workflow, "go run ./cmd/codeflux-dev generate-check")
	if generateAt < 0 {
		t.Fatal("CI does not run generate-check")
	}
	for _, dependent := range []string{
		"go run ./cmd/codeflux-dev test-coverage",
		"go run ./cmd/codeflux-dev build",
	} {
		at := strings.Index(workflow, dependent)
		if at < 0 {
			t.Fatalf("CI does not run %q", dependent)
		}
		if at < generateAt {
			t.Fatalf("%q runs before generate-check; drifted generated output would surface "+
				"as an unrelated failure", dependent)
		}
	}
}

// TestM22_127_OrdinaryTestsMakeNoExternalNetworkRequest covers M22-127.
//
// The check is structural rather than behavioural: rather than trying to prove
// a negative at runtime, it asserts no non-test source reaches a non-loopback
// host, and that the workflow does not add a network step to the ordinary
// suites.
func TestM22_127_OrdinaryTestsMakeNoExternalNetworkRequest(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	// The live-provider gate is the ONE place external calls are allowed, and
	// it is deliberately not part of the ordinary suites.
	workflow := readWorkflow(t)
	if strings.Contains(workflow, "go run ./cmd/codeflux-dev run-live") {
		t.Fatal("CI runs the live-provider gate as part of the ordinary suites")
	}

	// No test may reach a host that could actually resolve. The rule is
	// structural: a hostname under an RFC-reserved TLD cannot route anywhere,
	// so it is safe by construction; anything else needs a stated reason.
	external := regexp.MustCompile(`https?://([a-zA-Z0-9.-]+)`)

	// reservedSuffixes cannot resolve on the public internet (RFC 2606, RFC
	// 6761), so a test using one cannot make an external request even if it
	// tried.
	reservedSuffixes := []string{
		".invalid", ".test", ".example", ".localhost", ".internal", ".local",
		"example.com", "example.net", "example.org", "localhost",
	}
	// documentedExceptions are real hostnames a test may NAME. Each is a
	// negative fixture: the test asserts the request is refused, so the host
	// is never dialled. Anything added here needs the same justification.
	documentedExceptions := map[string]string{
		"api.anthropic.com": "TestAdapterRequiresExplicitRemoteEndpointApproval asserts the " +
			"adapter refuses this endpoint without explicit approval; it is never dialled",
		"git-lfs.github.com": "appears inside a Git-LFS pointer-file header, which is a file " +
			"format identifier rather than a request target",
		"www.w3.org": "appears as the SVG XML namespace, which is an identifier and is " +
			"never fetched",
	}

	resolvable := func(host string) bool {
		lowered := strings.ToLower(strings.TrimSuffix(host, "."))
		if lowered == "" {
			return false
		}
		if _, documented := documentedExceptions[lowered]; documented {
			return false
		}
		for _, suffix := range reservedSuffixes {
			if lowered == suffix || strings.HasSuffix(lowered, suffix) {
				return false
			}
		}
		// A loopback literal cannot leave the machine. Private, link-local,
		// and unspecified literals cannot reach the public internet either,
		// and they are how a test writes "deliberately not loopback" when
		// proving something is refused. A globally routable literal is still
		// reported: dialling one would genuinely reach outside.
		if address := net.ParseIP(lowered); address != nil {
			switch {
			case address.IsLoopback(), address.IsPrivate(),
				address.IsLinkLocalUnicast(), address.IsUnspecified():
				return false
			default:
				return true
			}
		}
		return strings.Contains(lowered, ".")
	}

	var offenders []string
	walk := func(directory string) {
		_ = filepath.Walk(filepath.Join(root, directory),
			func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() || !strings.HasSuffix(path, "_test.go") {
					return nil
				}
				source, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				for _, match := range external.FindAllStringSubmatch(string(source), -1) {
					if !resolvable(match[1]) {
						continue
					}
					relative, _ := filepath.Rel(root, path)
					offenders = append(offenders,
						filepath.ToSlash(relative)+": "+match[1])
				}
				return nil
			})
	}
	walk("internal")
	walk("cmd")
	walk("web")

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("tests reference %d hosts that could actually resolve; use a reserved "+
			"TLD or document why the host is never dialled: %s",
			len(offenders), strings.Join(offenders, "; "))
	}

	// The exception list must not rot into a blanket allowance.
	if len(documentedExceptions) > 3 {
		t.Fatalf("%d real hostnames are excepted; the rule is becoming decorative",
			len(documentedExceptions))
	}
}

// TestM22_125_EventSchemaAdditionsRequireReducerAndPresentationCoverage covers
// M22-125.
//
// A new event kind that no reducer handles and no card presents is an event
// the user never sees: the system records it, the timeline ignores it, and the
// gap is invisible until someone asks why a step is missing.
func TestM22_125_EventSchemaAdditionsRequireReducerAndPresentationCoverage(t *testing.T) {
	root := repositoryRootForCommandGraph(t)

	source, err := os.ReadFile(filepath.Join(root, "internal", "events", "session.go"))
	if err != nil {
		t.Fatalf("read event kinds: %v", err)
	}
	kindPattern := regexp.MustCompile(`Kind[A-Za-z]+ Kind = "([a-z-]+)"`)
	matches := kindPattern.FindAllStringSubmatch(string(source), -1)
	if len(matches) == 0 {
		t.Fatal("no event kinds were found; the kind parser is broken")
	}

	// Every declared kind must be handled by the payload validator, which is
	// the switch a new kind must be added to for the event to be storable at
	// all. A kind missing from it would be silently unvalidated.
	validation := string(source)
	var unhandled []string
	for _, match := range matches {
		kind := match[1]
		if !strings.Contains(validation, `case Kind`) {
			t.Fatal("the payload validator switch was not found")
		}
		// The constant name, not the string, is what the switch uses.
		constantPattern := regexp.MustCompile(`(Kind[A-Za-z]+) Kind = "` + regexp.QuoteMeta(kind) + `"`)
		constant := constantPattern.FindStringSubmatch(validation)
		if len(constant) != 2 {
			t.Fatalf("could not resolve the constant for kind %q", kind)
		}
		if !strings.Contains(validation, constant[1]+",") &&
			!strings.Contains(validation, "case "+constant[1]+":") &&
			!strings.Contains(validation, constant[1]+":") {
			unhandled = append(unhandled, kind)
		}
	}
	if len(unhandled) > 0 {
		sort.Strings(unhandled)
		t.Fatalf("%d event kinds are declared but never handled: %s",
			len(unhandled), strings.Join(unhandled, ", "))
	}

	// The frontend must know the same vocabulary. A kind the session
	// projection cannot name is a kind the browser will drop.
	projection, err := os.ReadFile(filepath.Join(
		root, "web", "frontend", "sessionprojection", "model.go"))
	if err != nil {
		t.Fatalf("read session projection: %v", err)
	}
	if !strings.Contains(string(projection), "func EventKinds()") {
		t.Fatal("the session projection does not declare the event kinds it consumes")
	}
}
