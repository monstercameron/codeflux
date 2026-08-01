package dogfood

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scaffoldFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "reserveflow")
	for _, relative := range ScaffoldContent() {
		full := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("create %s: %v", relative, err)
		}
		if err := os.WriteFile(full, []byte("// "+relative+"\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", relative, err)
		}
	}
	return root
}

// TestM24_101_102_TheScaffoldIsMinimalAndIdentified covers M24-101 and
// M24-102.
func TestM24_101_102_TheScaffoldIsMinimalAndIdentified(t *testing.T) {
	root := scaffoldFixture(t)
	if err := VerifyScaffoldContent(root); err != nil {
		t.Fatalf("a correct scaffold was rejected: %v", err)
	}

	// Anything extra hands the agent work it was supposed to do.
	extra := filepath.Join(root, "internal", "reservation", "reservation.go")
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		t.Fatalf("create extra: %v", err)
	}
	if err := os.WriteFile(extra, []byte("package reservation\n"), 0o600); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	if err := VerifyScaffoldContent(root); err == nil {
		t.Fatal("a scaffold containing an implementation was accepted")
	}
	if err := os.Remove(extra); err != nil {
		t.Fatalf("remove extra: %v", err)
	}

	// Anything missing means the agent starts from something other than the
	// declared baseline.
	if err := os.Remove(filepath.Join(root, "LICENSE")); err != nil {
		t.Fatalf("remove licence: %v", err)
	}
	if err := VerifyScaffoldContent(root); err == nil {
		t.Fatal("an incomplete scaffold was accepted")
	}

	// M24-102: the identity must change when anything changes, including a
	// rename that leaves contents identical.
	full := scaffoldFixture(t)
	original, err := HashTree(full, SkipGit)
	if err != nil {
		t.Fatalf("hash tree: %v", err)
	}
	if len(original) != 64 {
		t.Fatalf("tree hash length = %d", len(original))
	}
	again, err := HashTree(full, SkipGit)
	if err != nil || again != original {
		t.Fatalf("the tree hash is not deterministic: %q vs %q", original, again)
	}

	edited := filepath.Join(full, "README.md")
	if err := os.WriteFile(edited, []byte("changed\n"), 0o600); err != nil {
		t.Fatalf("edit file: %v", err)
	}
	afterEdit, err := HashTree(full, SkipGit)
	if err != nil {
		t.Fatalf("hash tree: %v", err)
	}
	if afterEdit == original {
		t.Fatal("editing a file did not change the tree hash")
	}

	renamed := scaffoldFixture(t)
	base, _ := HashTree(renamed, SkipGit)
	if err := os.Rename(
		filepath.Join(renamed, "README.md"),
		filepath.Join(renamed, "READ.md"),
	); err != nil {
		t.Fatalf("rename: %v", err)
	}
	afterRename, err := HashTree(renamed, SkipGit)
	if err != nil {
		t.Fatalf("hash tree: %v", err)
	}
	if afterRename == base {
		t.Fatal("renaming a file did not change the tree hash; contents alone are hashed")
	}
}

// TestM24_104_NothingTheAgentControlsCanReadTheEvaluator is the check the whole
// trial depends on.
func TestM24_104_NothingTheAgentControlsCanReadTheEvaluator(t *testing.T) {
	root := t.TempDir()
	evaluator := filepath.Join(root, "evaluator")
	scaffold := filepath.Join(root, "reserveflow")
	for _, directory := range []string{evaluator, scaffold} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}

	isolated := make([]Isolation, 0, len(IsolatedComponents()))
	for _, component := range IsolatedComponents() {
		isolated = append(isolated, Isolation{
			Component: component, ReachablePaths: []string{scaffold},
		})
	}
	violations, err := VerifyEvaluatorIsolation(evaluator, isolated)
	if err != nil {
		t.Fatalf("verify isolation: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("an isolated setup reported violations: %+v", violations)
	}

	// Every component must be checked. Checking four of five and finding
	// nothing proves nothing about the fifth.
	partial := isolated[1:]
	if _, err := VerifyEvaluatorIsolation(evaluator, partial); err == nil {
		t.Fatal("isolation was verified without checking every component")
	}
	if _, err := VerifyEvaluatorIsolation(evaluator, nil); err == nil {
		t.Fatal("isolation was verified with no components at all")
	}

	// A component that can reach the evaluator, at any depth, is a violation.
	for _, reachable := range []string{
		evaluator,
		filepath.Join(evaluator, "hidden"),
		filepath.Join(evaluator, "hidden", "acceptance_test.go"),
	} {
		tainted := make([]Isolation, len(isolated))
		copy(tainted, isolated)
		tainted[0] = Isolation{
			Component:      tainted[0].Component,
			ReachablePaths: []string{scaffold, reachable},
		}
		violations, err := VerifyEvaluatorIsolation(evaluator, tainted)
		if err != nil {
			t.Fatalf("verify isolation: %v", err)
		}
		if len(violations) == 0 {
			t.Fatalf("a component reaching %q was not reported", reachable)
		}
		if violations[0].Component != tainted[0].Component {
			t.Fatalf("the violation names %q", violations[0].Component)
		}
	}
}

// TestM24_105_107_StorageIsPerTrackAndResetIsSafe covers M24-105..107.
func TestM24_105_107_StorageIsPerTrackAndResetIsSafe(t *testing.T) {
	root := t.TempDir()
	allocations := []DatabaseAllocation{
		{
			Track:               "A",
			CodefluxDatabase:    filepath.Join(root, "a", "codeflux.sqlite3"),
			ApplicationDatabase: filepath.Join(root, "a", "reserveflow.sqlite3"),
		},
		{
			Track:               "B",
			CodefluxDatabase:    filepath.Join(root, "b", "codeflux.sqlite3"),
			ApplicationDatabase: filepath.Join(root, "b", "reserveflow.sqlite3"),
		},
	}
	if err := ValidateAllocations(allocations); err != nil {
		t.Fatalf("valid allocations were rejected: %v", err)
	}

	// M24-106: one file cannot serve both.
	shared := []DatabaseAllocation{{
		Track:               "A",
		CodefluxDatabase:    filepath.Join(root, "shared.sqlite3"),
		ApplicationDatabase: filepath.Join(root, "shared.sqlite3"),
	}}
	if err := ValidateAllocations(shared); err == nil {
		t.Fatal("a track sharing one database between coordinator and application validated")
	}

	// M24-105: two tracks cannot share a database, or their results are not
	// comparable.
	crossed := []DatabaseAllocation{
		allocations[0],
		{
			Track:               "B",
			CodefluxDatabase:    allocations[0].CodefluxDatabase,
			ApplicationDatabase: filepath.Join(root, "b", "reserveflow.sqlite3"),
		},
	}
	if err := ValidateAllocations(crossed); err == nil {
		t.Fatal("two tracks sharing a database validated")
	}
	if err := ValidateAllocations(nil); err == nil {
		t.Fatal("an empty allocation set validated")
	}

	// M24-107: a reset restores the accepted commit, clears only run-scoped
	// state, and never destroys what it promises to keep.
	plan := ResetPlan{
		RestoreCommit:         "abc1234567890abc1234567890abc1234567890a",
		RemovePaths:           []string{"reserveflow.sqlite3", "run-artifacts"},
		FreshCodefluxDatabase: filepath.Join(root, "fresh.sqlite3"),
		PreservePaths:         []string{".git"},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("a valid reset was rejected: %v", err)
	}
	for name, corrupt := range map[string]func(ResetPlan) ResetPlan{
		"no restore commit": func(candidate ResetPlan) ResetPlan {
			candidate.RestoreCommit = ""
			return candidate
		},
		"removes nothing": func(candidate ResetPlan) ResetPlan {
			candidate.RemovePaths = nil
			return candidate
		},
		"no fresh database": func(candidate ResetPlan) ResetPlan {
			candidate.FreshCodefluxDatabase = ""
			return candidate
		},
		"removes and preserves the same path": func(candidate ResetPlan) ResetPlan {
			candidate.RemovePaths = append(candidate.RemovePaths, ".git")
			return candidate
		},
		"removes the whole repository": func(candidate ResetPlan) ResetPlan {
			candidate.RemovePaths = []string{"."}
			return candidate
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(plan).Validate(); err == nil {
				t.Fatalf("an unsafe reset validated: %s", name)
			}
		})
	}
}

// TestM24_108_TheRunManifestFreezesEverythingThatCouldChangeAResult covers
// M24-108.
func TestM24_108_TheRunManifestFreezesEverythingThatCouldChangeAResult(t *testing.T) {
	manifest := RunManifest{
		GoVersion: "go1.26.3", DependencyLock: "sha256:abc",
		OperatingSystem: "windows", Architecture: "arm64",
		CodefluxVersion: "0.2.0", ProviderName: "anthropic",
		ModelIdentity: "claude-fixture-1", ReasoningEffort: "standard",
		ToolSchemaVersion: "3", PricingSnapshot: "2026-03-01",
		ValidationPolicy: "correctness", RoutingPolicy: "fixed",
		FrozenAt: FixtureEpoch,
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("a complete manifest was rejected: %v", err)
	}

	// Every field must be required. Anything optional is something that can
	// change a result without anyone noticing.
	for _, field := range ManifestFields() {
		partial := manifest
		switch field {
		case "go-version":
			partial.GoVersion = ""
		case "dependency-lock":
			partial.DependencyLock = ""
		case "operating-system":
			partial.OperatingSystem = ""
		case "architecture":
			partial.Architecture = ""
		case "codeflux-version":
			partial.CodefluxVersion = ""
		case "provider":
			partial.ProviderName = ""
		case "model":
			partial.ModelIdentity = ""
		case "reasoning-effort":
			partial.ReasoningEffort = ""
		case "tool-schema-version":
			partial.ToolSchemaVersion = ""
		case "pricing-snapshot":
			partial.PricingSnapshot = ""
		case "validation-policy":
			partial.ValidationPolicy = ""
		case "routing-policy":
			partial.RoutingPolicy = ""
		}
		if err := partial.Validate(); err == nil {
			t.Fatalf("a manifest missing %q validated", field)
		}
	}
	if err := (RunManifest{}).Validate(); err == nil {
		t.Fatal("an empty manifest validated")
	}

	// Two tracks under identical conditions must produce the same identity, or
	// "same conditions" cannot be demonstrated.
	other := manifest
	other.FrozenAt = FixtureEpoch.Add(time.Hour)
	if manifest.Identity() != other.Identity() {
		t.Fatal("the manifest identity changed with the freeze time alone")
	}
	changed := manifest
	changed.ModelIdentity = "claude-fixture-2"
	if manifest.Identity() == changed.Identity() {
		t.Fatal("changing the model did not change the manifest identity")
	}
}

// TestM24_109_110_PacketsAreChronologicalAndTheFutureIsSealed covers M24-109
// and M24-110.
func TestM24_109_110_PacketsAreChronologicalAndTheFutureIsSealed(t *testing.T) {
	if err := ValidatePackets(); err != nil {
		t.Fatalf("the packet sequence is invalid: %v", err)
	}
	if len(Packets()) != PacketCount {
		t.Fatalf("%d packets are declared, want %d", len(Packets()), PacketCount)
	}

	// M24-110: a run may read what has been revealed and nothing beyond.
	revealer := NewRevealer()
	if _, err := revealer.Get(1); !errors.Is(err, ErrFuturePacket) {
		t.Fatalf("packet 1 was readable before it was revealed: %v", err)
	}
	if len(revealer.Accessible()) != 0 {
		t.Fatal("a fresh revealer exposes packets")
	}

	for ordinal := 1; ordinal <= PacketCount; ordinal++ {
		packet, err := revealer.Reveal()
		if err != nil {
			t.Fatalf("reveal %d: %v", ordinal, err)
		}
		if packet.Ordinal != ordinal {
			t.Fatalf("revealed packet %d, want %d", packet.Ordinal, ordinal)
		}
		// Everything up to here is readable.
		for earlier := 1; earlier <= ordinal; earlier++ {
			if _, err := revealer.Get(earlier); err != nil {
				t.Fatalf("packet %d unreadable at ordinal %d: %v", earlier, ordinal, err)
			}
		}
		// Nothing beyond is.
		for later := ordinal + 1; later <= PacketCount; later++ {
			if _, err := revealer.Get(later); !errors.Is(err, ErrFuturePacket) {
				t.Fatalf("future packet %d was readable at ordinal %d", later, ordinal)
			}
		}
		if len(revealer.Accessible()) != ordinal {
			t.Fatalf("accessible = %d at ordinal %d", len(revealer.Accessible()), ordinal)
		}
	}
	if _, err := revealer.Reveal(); err == nil {
		t.Fatal("a sixteenth packet was revealed")
	}
	if _, err := revealer.Get(0); err == nil {
		t.Fatal("packet 0 resolved")
	}

	// A sequence where nothing builds on anything is a set of exercises, not a
	// product history.
	dependent := 0
	for _, packet := range Packets() {
		if len(packet.DependsOn) > 0 {
			dependent++
		}
	}
	if dependent < PacketCount/2 {
		t.Fatalf("only %d packets build on an earlier one", dependent)
	}
}

// TestM24_111_TheAcceptedChainIsOnePerPacket covers M24-111.
func TestM24_111_TheAcceptedChainIsOnePerPacket(t *testing.T) {
	// Each commit must be distinct AND hexadecimal; a fixture that drifted out
	// of the hex alphabet would fail for the wrong reason.
	const hexDigits = "0123456789abcdef"
	chain := AcceptedChain{}
	for index := range PacketCount {
		chain.Commits = append(chain.Commits,
			strings.Repeat("0", 39)+string(hexDigits[index%len(hexDigits)]))
	}
	if err := chain.Validate(); err != nil {
		t.Fatalf("a valid chain was rejected: %v", err)
	}
	commit, err := chain.EquivalentState(3)
	if err != nil || commit != chain.Commits[2] {
		t.Fatalf("equivalent state for packet 3 = %q %v", commit, err)
	}

	short := AcceptedChain{Commits: chain.Commits[1:]}
	if err := short.Validate(); err == nil {
		t.Fatal("a chain shorter than the packet sequence validated")
	}
	// A repeated commit means one packet changed nothing.
	repeated := AcceptedChain{Commits: append([]string(nil), chain.Commits...)}
	repeated.Commits[4] = repeated.Commits[3]
	if err := repeated.Validate(); err == nil {
		t.Fatal("a chain where a packet changed nothing validated")
	}
	if _, err := chain.EquivalentState(99); err == nil {
		t.Fatal("an out-of-range packet resolved a state")
	}
}

// TestM24_112_113_TheLedgerIsAppendOnlyAndDetectsContamination covers M24-112
// and M24-113.
func TestM24_112_113_TheLedgerIsAppendOnlyAndDetectsContamination(t *testing.T) {
	ledger := NewLedger()
	at := FixtureEpoch

	for _, kind := range []InterventionKind{
		InterventionApproval, InterventionClarification, InterventionDenial,
	} {
		at = at.Add(time.Minute)
		if err := ledger.Append(Intervention{
			At: at, Task: 1, Kind: kind,
			Detail: "recorded during task one", Actor: "evaluator",
		}); err != nil {
			t.Fatalf("append %s: %v", kind, err)
		}
	}
	if len(ledger.Entries()) != 3 {
		t.Fatalf("the ledger holds %d entries", len(ledger.Entries()))
	}
	if ledger.Count(InterventionApproval) != 1 {
		t.Fatalf("approval count = %d", ledger.Count(InterventionApproval))
	}

	// M24-113: approving does NOT contaminate. Approving is what the product
	// asks the user to do, and counting it would make the claim unachievable.
	if ledger.Contaminated() {
		t.Fatalf("approvals contaminated the run: %v", ledger.ContaminationReasons())
	}

	// A manual source edit does contaminate: after one, the diff is no longer
	// the agent's work.
	at = at.Add(time.Minute)
	if err := ledger.Append(Intervention{
		At: at, Task: 2, Kind: InterventionManualEdit,
		Detail: "fixed a compile error by hand", Actor: "evaluator",
	}); err != nil {
		t.Fatalf("append manual edit: %v", err)
	}
	if !ledger.Contaminated() {
		t.Fatal("a manual source edit did not contaminate the run")
	}
	reasons := ledger.ContaminationReasons()
	if len(reasons) != 1 || !strings.Contains(reasons[0], "task 2") {
		t.Fatalf("contamination reasons = %v", reasons)
	}

	// Append-only: an entry cannot go backwards in time.
	if err := ledger.Append(Intervention{
		At: FixtureEpoch, Task: 1, Kind: InterventionApproval,
		Detail: "backdated", Actor: "evaluator",
	}); err == nil {
		t.Fatal("a backdated entry was appended")
	}

	// The digest ties a report to the ledger it came from.
	digest := ledger.Digest()
	at = at.Add(time.Minute)
	if err := ledger.Append(Intervention{
		At: at, Task: 3, Kind: InterventionRedirect,
		Detail: "asked for a different approach", Actor: "evaluator",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if ledger.Digest() == digest {
		t.Fatal("appending did not change the ledger digest")
	}

	// A sealed ledger accepts nothing further.
	ledger.Seal()
	if err := ledger.Append(Intervention{
		At: at.Add(time.Minute), Task: 3, Kind: InterventionApproval,
		Detail: "late", Actor: "evaluator",
	}); err == nil {
		t.Fatal("a sealed ledger accepted an entry")
	}

	// Unusable entries are refused.
	fresh := NewLedger()
	for name, intervention := range map[string]Intervention{
		"no time":   {Task: 1, Kind: InterventionApproval, Detail: "x", Actor: "y"},
		"no task":   {At: FixtureEpoch, Kind: InterventionApproval, Detail: "x", Actor: "y"},
		"bad kind":  {At: FixtureEpoch, Task: 1, Kind: "invented", Detail: "x", Actor: "y"},
		"no detail": {At: FixtureEpoch, Task: 1, Kind: InterventionApproval, Actor: "y"},
		"no actor":  {At: FixtureEpoch, Task: 1, Kind: InterventionApproval, Detail: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := fresh.Append(intervention); err == nil {
				t.Fatalf("an unusable intervention was appended: %s", name)
			}
		})
	}
}

// TestM24_115_TracksAreComparable covers M24-115.
func TestM24_115_TracksAreComparable(t *testing.T) {
	if err := ValidateTracks(); err != nil {
		t.Fatalf("the declared tracks are invalid: %v", err)
	}
	tracks := DeclaredTracks()
	if len(tracks) != 4 {
		t.Fatalf("%d tracks are declared, want 4", len(tracks))
	}

	// A track where both a human and CodeFlux write attributes its result to
	// neither.
	both := tracks[0]
	both.HumanWrites = true
	if err := both.Validate(); err == nil {
		t.Fatal("a track with both writing code validated")
	}
	neither := tracks[0]
	neither.UsesCodeflux = false
	neither.HumanWrites = false
	if err := neither.Validate(); err == nil {
		t.Fatal("a track with nobody writing code validated")
	}

	// Track D must isolate exactly one variable from Track A, or the
	// comparison attributes a difference to the wrong cause.
	var trackA, trackD Track
	for _, track := range tracks {
		switch track.Name {
		case "A":
			trackA = track
		case "D":
			trackD = track
		}
	}
	if !trackA.UsesCodeflux || !trackD.UsesCodeflux {
		t.Fatal("tracks A and D must both use CodeFlux for D to isolate memory")
	}
	if !strings.Contains(strings.ToLower(trackD.Description), "memory") {
		t.Fatalf("track D does not say what it isolates: %q", trackD.Description)
	}
}

// TestM24_116_117_EvaluatorFixturesAreDeterministic covers M24-116 and
// M24-117.
func TestM24_116_117_EvaluatorFixturesAreDeterministic(t *testing.T) {
	clock := NewEvaluatorClock()
	if !clock.Now().Equal(FixtureEpoch) {
		t.Fatalf("the clock starts at %v", clock.Now())
	}
	// A clock that advances on its own would make an expiry boundary
	// unassertable.
	if !clock.Now().Equal(clock.Now()) {
		t.Fatal("reading the clock advanced it")
	}
	if err := clock.Advance(time.Minute); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !clock.Now().Equal(FixtureEpoch.Add(time.Minute)) {
		t.Fatalf("after advancing one minute the clock reads %v", clock.Now())
	}
	if err := clock.Advance(-time.Second); err == nil {
		t.Fatal("the clock moved backwards")
	}
	if err := clock.AdvanceTo(FixtureEpoch); err == nil {
		t.Fatal("the clock was moved back with AdvanceTo")
	}

	// All three boundary instants must be distinct and correctly ordered.
	before, at, after := BoundaryInstants(FixtureEpoch)
	if !before.Before(at) || !at.Before(after) {
		t.Fatalf("boundary instants are not ordered: %v %v %v", before, at, after)
	}
	if !at.Equal(FixtureEpoch.Add(ExpirationWindow)) {
		t.Fatalf("the boundary is at %v", at)
	}

	// M24-117: identities are stable and ordered, so a duplicate is
	// detectable rather than merely countable.
	fixture := NewIdentityFixture()
	first, err := fixture.Next(IdentityDelivery)
	if err != nil {
		t.Fatalf("next identity: %v", err)
	}
	second, err := fixture.Next(IdentityDelivery)
	if err != nil {
		t.Fatalf("next identity: %v", err)
	}
	if first == second {
		t.Fatal("the fixture issued the same identity twice")
	}
	if first >= second {
		t.Fatalf("identities are not ordered: %q then %q", first, second)
	}
	if fixture.Issued(IdentityDelivery) != 2 {
		t.Fatalf("issued = %d", fixture.Issued(IdentityDelivery))
	}
	// Kinds are independent, so a resource identity never collides with a
	// delivery identity.
	resource, err := fixture.Next(IdentityResource)
	if err != nil {
		t.Fatalf("next identity: %v", err)
	}
	if strings.HasPrefix(resource, string(IdentityDelivery)) {
		t.Fatalf("a resource identity looks like a delivery: %q", resource)
	}
	if _, err := fixture.Next(IdentityKind("invented")); err == nil {
		t.Fatal("an unknown identity kind was issued")
	}
}

// TestM24_118_119_TheReceiverRecordsWithoutRevealingItsAssertions covers
// M24-118 and M24-119.
func TestM24_118_119_TheReceiverRecordsWithoutRevealingItsAssertions(t *testing.T) {
	clock := NewEvaluatorClock()
	receiver, err := NewMockReceiver(ModeAccepted, clock)
	if err != nil {
		t.Fatalf("new receiver: %v", err)
	}

	mode, err := receiver.Deliver("delivery-0001", "sig", "hash",
		map[string]string{"X-Signature": "sig"})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if mode != ModeAccepted {
		t.Fatalf("mode = %q", mode)
	}
	receipts := receiver.Receipts()
	if len(receipts) != 1 {
		t.Fatalf("recorded %d receipts", len(receipts))
	}
	// Everything an evaluator needs to assert on must be recorded.
	receipt := receipts[0]
	if receipt.DeliveryID == "" || receipt.Signature == "" ||
		receipt.PayloadHash == "" || len(receipt.Headers) == 0 || receipt.At.IsZero() {
		t.Fatalf("the receipt is incomplete: %+v", receipt)
	}
	// A delivery with no identity cannot be checked for duplication.
	if _, err := receiver.Deliver("", "sig", "hash", nil); err == nil {
		t.Fatal("a delivery with no identity was accepted")
	}

	// Duplicates are detectable by identity, not by count.
	if _, err := receiver.Deliver("delivery-0001", "sig", "hash", nil); err != nil {
		t.Fatalf("redeliver: %v", err)
	}
	duplicates := receiver.DuplicateDeliveries()
	if len(duplicates) != 1 || duplicates[0] != "delivery-0001" {
		t.Fatalf("duplicates = %v", duplicates)
	}

	// M24-119: every declared mode must exist and classify correctly.
	if len(AllAmbiguityModes()) != 7 {
		t.Fatalf("%d ambiguity modes are declared", len(AllAmbiguityModes()))
	}
	for mode, retryable := range map[AmbiguityMode]bool{
		ModeConnectionRefused: true, ModeRetryable5xx: true, ModeSlowResponse: true,
		ModeTerminal4xx: false, ModeAccepted: false, ModeAcceptedThenTimeout: false,
	} {
		if mode.Retryable() != retryable {
			t.Fatalf("%s retryable = %v, want %v", mode, mode.Retryable(), retryable)
		}
	}
	// The dangerous case: the receiver acted and the sender cannot know.
	if !ModeAcceptedThenTimeout.Ambiguous() {
		t.Fatal("accepted-then-timeout is not treated as ambiguous")
	}
	if ModeAcceptedThenTimeout.Retryable() {
		t.Fatal("accepted-then-timeout is retryable; retrying would deliver twice")
	}
	if ModeTerminal4xx.Ambiguous() {
		t.Fatal("a terminal 4xx is treated as ambiguous")
	}
	if _, err := NewMockReceiver(AmbiguityMode("invented"), clock); err == nil {
		t.Fatal("an unknown mode built a receiver")
	}
	if _, err := NewMockReceiver(ModeAccepted, nil); err == nil {
		t.Fatal("a receiver was built with no clock")
	}
}

// TestM24_120_122_RacesAndCrashPointsCoverTheRealGaps covers M24-120..122.
func TestM24_120_122_RacesAndCrashPointsCoverTheRealGaps(t *testing.T) {
	// M24-120 and M24-121: the split is real. Multi-process races cannot be
	// simulated in one process.
	if len(InProcessRaces()) == 0 || len(MultiProcessRaces()) == 0 {
		t.Fatal("one of the concurrency drivers covers nothing")
	}
	for _, race := range InProcessRaces() {
		for _, other := range MultiProcessRaces() {
			if race == other {
				t.Fatalf("race %q is claimed by both drivers", race)
			}
		}
	}
	for _, required := range []ConcurrencyRace{
		RaceSameResource, RaceSameIdempotency, RaceSQLiteLock,
		RaceStaleVersion, RaceWorkerOwnership, RaceShutdown,
	} {
		found := false
		for _, race := range AllRaces() {
			if race == required {
				found = true
			}
		}
		if !found {
			t.Fatalf("no driver covers %q", required)
		}
	}

	// M24-122: crash points come in before/after pairs straddling a commit.
	// The gap between a pair is where every duplicate-effect bug lives.
	points := AllCrashPoints()
	if len(points)%2 != 0 {
		t.Fatalf("%d crash points are declared; they must pair", len(points))
	}
	before := 0
	after := 0
	for _, point := range points {
		if point.DurableBefore() {
			after++
		} else {
			before++
		}
	}
	if before == 0 || after == 0 {
		t.Fatalf("crash points are one-sided: %d before, %d after", before, after)
	}
	if before != after {
		t.Fatalf("crash points do not pair: %d before, %d after", before, after)
	}
	if !CrashAfterReservationCommit.DurableBefore() {
		t.Fatal("an after-commit crash point is not treated as durable")
	}
	if CrashBeforeReservationCommit.DurableBefore() {
		t.Fatal("a before-commit crash point is treated as durable")
	}
}

// TestM24_123_124_KeyFixturesAndSecretMarkersAreSynthetic covers M24-123 and
// M24-124.
func TestM24_123_124_KeyFixturesAndSecretMarkersAreSynthetic(t *testing.T) {
	fixtures := KeyFixtures()
	if len(fixtures) != len(AllKeyFixtures()) {
		t.Fatalf("%d key fixtures for %d kinds", len(fixtures), len(AllKeyFixtures()))
	}
	distinguishable := 0
	for _, fixture := range fixtures {
		if err := fixture.Validate(); err != nil {
			t.Fatalf("key fixture %q is invalid: %v", fixture.Kind, err)
		}
		if fixture.MustBeDistinguishable {
			distinguishable++
		}
	}
	// Every rejection must be distinguishable: a caller cannot fix a revoked
	// key by re-sending it, and a generic 401 tells them to try.
	if distinguishable < 5 {
		t.Fatalf("only %d rejections must be distinguishable", distinguishable)
	}

	// A fixture that looks like a real key becomes one in a bug report.
	realistic := KeyFixture{
		Kind: KeyInvalid, Material: "sk-live-abcdefghijklmnop", ExpectedStatus: 401,
	}
	if err := realistic.Validate(); err == nil {
		t.Fatal("a realistic-looking key fixture validated")
	}

	// M24-124: markers are seeded per surface, so a leak names its origin.
	markers := SecretMarkers()
	if len(markers) < 4 {
		t.Fatalf("%d markers are seeded", len(markers))
	}
	seen := map[string]bool{}
	for surface, marker := range markers {
		if seen[marker] {
			t.Fatalf("surface %q shares a marker with another surface", surface)
		}
		seen[marker] = true
		if !strings.Contains(marker, "not-a-real") {
			t.Fatalf("marker for %q is not obviously synthetic: %q", surface, marker)
		}
	}

	// A marker on its own surface is not a leak; anywhere else is.
	clean := map[SecretMarkerSurface]string{
		SurfaceCredentialStore: markers[SurfaceCredentialStore],
		SurfaceLogs:            "nothing secret here",
		SurfaceDatabase:        "reservation rows",
	}
	findings, err := ScanSurfaces(clean)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("a clean run reported leaks: %+v", findings)
	}

	leaky := map[SecretMarkerSurface]string{
		SurfaceLogs: "provider error: " + markers[SurfaceCredentialStore],
	}
	findings, err = ScanSurfaces(leaky)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Marker != SurfaceCredentialStore || findings[0].FoundIn != SurfaceLogs {
		t.Fatalf("the finding does not name origin and destination: %+v", findings[0])
	}
	if _, err := ScanSurfaces(nil); err == nil {
		t.Fatal("scanning nothing succeeded")
	}
}

// TestM24_125_126_SnapshotsAndContractVerificationCoverBothDirections covers
// M24-125 and M24-126.
func TestM24_125_126_SnapshotsAndContractVerificationCoverBothDirections(t *testing.T) {
	if len(AllSnapshots()) != 5 {
		t.Fatalf("%d snapshots are declared, want 5", len(AllSnapshots()))
	}
	// Only the newer-schema snapshot must be refused; the others are states
	// the system has to handle.
	refused := 0
	for _, snapshot := range AllSnapshots() {
		if snapshot.MustRefuse() {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("%d snapshots must be refused, want exactly 1", refused)
	}
	if !SnapshotUnsupportedNewer.MustRefuse() {
		t.Fatal("an unsupported newer schema is not refused")
	}

	// M24-126: both directions are checked.
	described := map[ContractAspect][]string{}
	actual := map[ContractAspect][]string{}
	for _, aspect := range AllContractAspects() {
		described[aspect] = []string{"same"}
		actual[aspect] = []string{"same"}
	}
	mismatches, err := VerifyContract(described, actual)
	if err != nil {
		t.Fatalf("verify contract: %v", err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("identical contracts reported mismatches: %+v", mismatches)
	}

	// Described but not implemented: a caller would rely on it.
	described[AspectPaths] = []string{"same", "/reservations/{id}/confirm"}
	mismatches, err = VerifyContract(described, actual)
	if err != nil {
		t.Fatalf("verify contract: %v", err)
	}
	if len(mismatches) != 1 || mismatches[0].Actual != "(absent)" {
		t.Fatalf("mismatches = %+v", mismatches)
	}

	// Implemented but not described: a caller cannot discover it.
	described[AspectPaths] = []string{"same"}
	actual[AspectPaths] = []string{"same", "/internal/debug"}
	mismatches, err = VerifyContract(described, actual)
	if err != nil {
		t.Fatalf("verify contract: %v", err)
	}
	if len(mismatches) != 1 || mismatches[0].Described != "(absent)" {
		t.Fatalf("mismatches = %+v", mismatches)
	}

	// An aspect nobody compared is reported rather than passing silently.
	delete(described, AspectPagination)
	mismatches, err = VerifyContract(described, actual)
	if err != nil {
		t.Fatalf("verify contract: %v", err)
	}
	found := false
	for _, mismatch := range mismatches {
		if mismatch.Aspect == AspectPagination &&
			strings.Contains(mismatch.Explanation, "not compared") {
			found = true
		}
	}
	if !found {
		t.Fatalf("an uncompared aspect was not reported: %+v", mismatches)
	}
	if _, err := VerifyContract(nil, actual); err == nil {
		t.Fatal("a contract was verified with no description")
	}
}

// TestM24_127_130_SuitesAreFrozenBeforeTheRunAndSealed covers M24-127..130.
func TestM24_127_130_SuitesAreFrozenBeforeTheRunAndSealed(t *testing.T) {
	runStarted := FixtureEpoch.Add(24 * time.Hour)
	hash := strings.Repeat("a", 64)

	visible := FrozenSuite{
		Kind: SuiteVisible, Packet: 1, TreeHash: hash, FrozenAt: runStarted,
	}
	if err := visible.Validate(runStarted); err != nil {
		t.Fatalf("a visible suite frozen at run start was rejected: %v", err)
	}

	hidden := FrozenSuite{
		Kind: SuiteHidden, Packet: 1, TreeHash: hash,
		FrozenAt:         FixtureEpoch,
		AssertsBehaviour: true,
		ReviewNote:       "asserts overlap refusal and capacity, not a particular struct layout",
	}
	if err := hidden.Validate(runStarted); err != nil {
		t.Fatalf("a properly frozen hidden suite was rejected: %v", err)
	}

	// M24-128: a hidden suite frozen after the run began can be shaped to
	// whatever was produced.
	late := hidden
	late.FrozenAt = runStarted.Add(time.Hour)
	if err := late.Validate(runStarted); err == nil {
		t.Fatal("a hidden suite frozen after the run began validated")
	}

	// M24-129: a suite that encodes a preferred implementation shape fails a
	// correct solution.
	unreviewed := hidden
	unreviewed.AssertsBehaviour = false
	if err := unreviewed.Validate(runStarted); err == nil {
		t.Fatal("an unreviewed hidden suite validated")
	}
	unnoted := hidden
	unnoted.ReviewNote = ""
	if err := unnoted.Validate(runStarted); err == nil {
		t.Fatal("a hidden suite with no review note validated")
	}

	// M24-130: every artifact is hashed, so a post-run edit is detectable.
	sealed := SealedArtifacts{
		EvaluatorRepository: strings.Repeat("1", 64),
		RequirementPackets:  strings.Repeat("2", 64),
		VisibleFixtures:     strings.Repeat("3", 64),
		HiddenFixtures:      strings.Repeat("4", 64),
		ScoringConfig:       strings.Repeat("5", 64),
		SealedAt:            FixtureEpoch,
	}
	if err := sealed.Validate(); err != nil {
		t.Fatalf("a complete seal was rejected: %v", err)
	}
	for _, field := range SealedArtifactFields() {
		partial := sealed
		switch field {
		case "evaluator-repository":
			partial.EvaluatorRepository = ""
		case "requirement-packets":
			partial.RequirementPackets = ""
		case "visible-fixtures":
			partial.VisibleFixtures = ""
		case "hidden-fixtures":
			partial.HiddenFixtures = ""
		case "scoring-configuration":
			partial.ScoringConfig = ""
		}
		if err := partial.Validate(); err == nil {
			t.Fatalf("a seal omitting %q validated", field)
		}
	}

	current := map[string]string{
		"evaluator-repository":  sealed.EvaluatorRepository,
		"requirement-packets":   sealed.RequirementPackets,
		"visible-fixtures":      sealed.VisibleFixtures,
		"hidden-fixtures":       sealed.HiddenFixtures,
		"scoring-configuration": sealed.ScoringConfig,
	}
	changed, err := DetectTampering(sealed, current)
	if err != nil {
		t.Fatalf("detect tampering: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("an unchanged seal reported changes: %v", changed)
	}

	current["hidden-fixtures"] = strings.Repeat("9", 64)
	changed, err = DetectTampering(sealed, current)
	if err != nil {
		t.Fatalf("detect tampering: %v", err)
	}
	if len(changed) != 1 || changed[0] != "hidden-fixtures" {
		t.Fatalf("tampering was not detected: %v", changed)
	}

	// An artifact nobody re-hashed cannot be ruled out as changed.
	delete(current, "scoring-configuration")
	changed, err = DetectTampering(sealed, current)
	if err != nil {
		t.Fatalf("detect tampering: %v", err)
	}
	found := false
	for _, entry := range changed {
		if strings.Contains(entry, "scoring-configuration") {
			found = true
		}
	}
	if !found {
		t.Fatalf("an un-rehashed artifact was treated as unchanged: %v", changed)
	}
}

// TestM24_131_160_EveryTaskIsPairedWithAnIndependentVerdict covers
// M24-131..160.
func TestM24_131_160_EveryTaskIsPairedWithAnIndependentVerdict(t *testing.T) {
	if err := ValidateTasks(); err != nil {
		t.Fatalf("the task sequence is invalid: %v", err)
	}
	tasks := Tasks()
	if len(tasks) != PacketCount {
		t.Fatalf("%d tasks for %d packets", len(tasks), PacketCount)
	}

	for _, task := range tasks {
		// The pairing is what makes a verdict independent. A task that ran and
		// judged itself under one TODO would grade its own homework.
		if task.RunTodo == task.VerdictTodo {
			t.Fatalf("task %d runs and judges itself", task.Ordinal)
		}
		// Every task must declare enough acceptance cases to be more than a
		// smoke test.
		if len(task.AcceptanceCases) < 4 {
			t.Fatalf("task %d declares only %d acceptance cases",
				task.Ordinal, len(task.AcceptanceCases))
		}
		// The task and its packet must agree about what is being built.
		packet, err := TaskFor(task.Ordinal)
		if err != nil {
			t.Fatalf("task %d does not resolve: %v", task.Ordinal, err)
		}
		if packet.Ordinal != task.Ordinal {
			t.Fatalf("task %d resolved to %d", task.Ordinal, packet.Ordinal)
		}
	}
	if _, err := TaskFor(99); err == nil {
		t.Fatal("a task outside the sequence resolved")
	}

	// Two tasks must withhold information: without them the sequence never
	// tests diagnosis, only construction.
	withheld, err := WithheldInformation()
	if err != nil {
		t.Fatalf("withheld information: %v", err)
	}
	if len(withheld) < 2 {
		t.Fatalf("only %d tasks withhold anything", len(withheld))
	}
	if _, ok := withheld[13]; !ok {
		t.Fatal("the defect task supplies its own root cause")
	}
	if _, ok := withheld[14]; !ok {
		t.Fatal("the rule-change task supplies the affected files")
	}
}

// TestM24_131_160_AVerdictCannotAcceptAFailedCase is the property that stops a
// dogfood run grading itself generously.
func TestM24_131_160_AVerdictCannotAcceptAFailedCase(t *testing.T) {
	task, err := TaskFor(1)
	if err != nil {
		t.Fatalf("task 1: %v", err)
	}

	passing := TaskVerdict{Ordinal: 1, Accepted: true, CaseResults: map[string]bool{}}
	for _, acceptanceCase := range task.AcceptanceCases {
		passing.CaseResults[acceptanceCase] = true
	}
	if err := passing.Validate(); err != nil {
		t.Fatalf("a fully passing verdict was rejected: %v", err)
	}

	// Accepting while a declared case failed is exactly what this structure
	// exists to prevent.
	generous := TaskVerdict{Ordinal: 1, Accepted: true, CaseResults: map[string]bool{}}
	for index, acceptanceCase := range task.AcceptanceCases {
		generous.CaseResults[acceptanceCase] = index != 0
	}
	if err := generous.Validate(); err == nil {
		t.Fatal("a task was accepted although a declared case failed")
	}

	// Judging without checking every case is refused: a verdict over a subset
	// is a verdict about a different task.
	partial := TaskVerdict{Ordinal: 1, Accepted: true, CaseResults: map[string]bool{
		task.AcceptanceCases[0]: true,
	}}
	if err := partial.Validate(); err == nil {
		t.Fatal("a task was judged without checking every case")
	}
	if err := (TaskVerdict{Ordinal: 1, Accepted: true}).Validate(); err == nil {
		t.Fatal("a task was judged with no case results at all")
	}

	// A rejection must say why.
	rejected := TaskVerdict{Ordinal: 1, Accepted: false, CaseResults: map[string]bool{}}
	for _, acceptanceCase := range task.AcceptanceCases {
		rejected.CaseResults[acceptanceCase] = false
	}
	if err := rejected.Validate(); err == nil {
		t.Fatal("a task was rejected with no note")
	}
	rejected.Note = "the server left a listener open after cancellation"
	if err := rejected.Validate(); err != nil {
		t.Fatalf("an explained rejection was refused: %v", err)
	}
}

// TestM24_131_160_AProgressionCannotSkipItsDependencies proves the run is
// genuinely chronological.
func TestM24_131_160_AProgressionCannotSkipItsDependencies(t *testing.T) {
	accept := func(ordinal int) TaskVerdict {
		task, err := TaskFor(ordinal)
		if err != nil {
			t.Fatalf("task %d: %v", ordinal, err)
		}
		verdict := TaskVerdict{Ordinal: ordinal, Accepted: true, CaseResults: map[string]bool{}}
		for _, acceptanceCase := range task.AcceptanceCases {
			verdict.CaseResults[acceptanceCase] = true
		}
		return verdict
	}

	complete := Progression{}
	for ordinal := 1; ordinal <= PacketCount; ordinal++ {
		complete.Verdicts = append(complete.Verdicts, accept(ordinal))
	}
	if err := complete.Validate(); err != nil {
		t.Fatalf("a complete progression was rejected: %v", err)
	}
	if complete.Completed() != PacketCount {
		t.Fatalf("completed = %d", complete.Completed())
	}
	if _, rejected := complete.FirstRejection(); rejected {
		t.Fatal("a fully accepted progression reported a rejection")
	}

	// Attempting a task whose dependency never ran describes a codebase that
	// never existed.
	skipped := Progression{Verdicts: []TaskVerdict{accept(5)}}
	if err := skipped.Validate(); err == nil {
		t.Fatal("a task was attempted with its dependencies never run")
	}

	// Attempting one whose dependency was REJECTED is the subtler version of
	// the same problem.
	task3, err := TaskFor(3)
	if err != nil {
		t.Fatalf("task 3: %v", err)
	}
	rejectedThree := TaskVerdict{
		Ordinal: 3, Accepted: false, Note: "capacity went negative",
		CaseResults: map[string]bool{},
	}
	for _, acceptanceCase := range task3.AcceptanceCases {
		rejectedThree.CaseResults[acceptanceCase] = false
	}
	onRejected := Progression{Verdicts: []TaskVerdict{
		accept(1), accept(2), rejectedThree, accept(4),
	}}
	if err := onRejected.Validate(); err == nil {
		t.Fatal("a task was built on a rejected dependency")
	}

	// A duplicate verdict is refused.
	duplicated := Progression{Verdicts: []TaskVerdict{accept(1), accept(1)}}
	if err := duplicated.Validate(); err == nil {
		t.Fatal("a task was judged twice")
	}

	// The first rejection is the one that matters: everything after it is
	// built on something that did not hold.
	partial := Progression{Verdicts: []TaskVerdict{accept(1), accept(2), rejectedThree}}
	if err := partial.Validate(); err != nil {
		t.Fatalf("a partial progression was rejected: %v", err)
	}
	first, rejected := partial.FirstRejection()
	if !rejected || first.Ordinal != 3 {
		t.Fatalf("first rejection = %+v (%v)", first, rejected)
	}
	if partial.Completed() != 2 {
		t.Fatalf("completed = %d", partial.Completed())
	}
}
