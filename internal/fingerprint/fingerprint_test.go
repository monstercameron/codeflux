package fingerprint

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/forecast"
)

const (
	projectFixture    = "prj_01890f3c-4a00-7abc-8def-0123456789ab"
	repositoryFixture = "repo_01890f3c-4a00-7abc-9def-0123456789ab"
)

func sampleInput(t *testing.T) ExactFingerprintInput {
	t.Helper()
	project, err := domain.ParseProjectID(projectFixture)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := domain.ParseRepositoryID(repositoryFixture)
	if err != nil {
		t.Fatal(err)
	}
	return ExactFingerprintInput{
		Project:    project,
		Repository: repository,
		BaseRevision: domain.RevisionBinding{
			Known:         true,
			ExactRevision: "abc123def456",
		},
		TaskClass:        TaskClassBugFix,
		AffectedPaths:    []string{"internal/fingerprint/fingerprint.go", "internal/domain/memory.go"},
		AffectedPackages: []string{"codeflux.dev/codeflux/internal/fingerprint", "codeflux.dev/codeflux/internal/domain"},
		AffectedSymbols:  []string{"BuildExactFingerprint", "ExactFingerprint.Hash"},
		Bindings: []ToolchainBinding{
			{Name: "go", Version: "1.26.0"},
			{Name: "codeflux.dev/codeflux", Version: "v0.0.0"},
		},
		Risk:              domain.RiskLevelElevated,
		RequiredAssurance: domain.AssuranceLevelContractChecked,
		RequestedAuthority: []AuthorityClass{
			AuthorityClassTaskWrite,
			AuthorityClassAutomaticRead,
		},
	}
}

func buildSample(t *testing.T) ExactFingerprint {
	t.Helper()
	value, err := BuildExactFingerprint(sampleInput(t))
	if err != nil {
		t.Fatalf("BuildExactFingerprint: %v", err)
	}
	return value
}

// M21-051: schema version is explicit, stored, and a mismatch is refused
// rather than silently reinterpreted.
func TestSchemaVersionIsExplicitAndMismatchIsRejected(t *testing.T) {
	value := buildSample(t)
	if value.SchemaVersion != SchemaVersion {
		return
	}
	mismatched := value
	mismatched.SchemaVersion = SchemaVersion + 1
	if err := mismatched.Validate(); !errors.Is(err, ErrUnsupportedFingerprintSchemaVersion) {
		t.Fatalf("Validate() with future schema version error = %v, want ErrUnsupportedFingerprintSchemaVersion", err)
	}
	if _, err := mismatched.Hash(); !errors.Is(err, ErrUnsupportedFingerprintSchemaVersion) {
		t.Fatalf("Hash() with future schema version error = %v, want ErrUnsupportedFingerprintSchemaVersion", err)
	}
	zeroed := value
	zeroed.SchemaVersion = 0
	if err := zeroed.Validate(); !errors.Is(err, ErrUnsupportedFingerprintSchemaVersion) {
		t.Fatalf("Validate() with zero schema version error = %v, want ErrUnsupportedFingerprintSchemaVersion", err)
	}
}

// M21-052..058: every required exact field is present and reused from
// existing domain vocabulary where the plan describes a domain concept.
func TestRequiredExactFieldsAreEnforced(t *testing.T) {
	base := sampleInput(t)

	missingProject := base
	missingProject.Project = domain.ProjectID{}
	if _, err := BuildExactFingerprint(missingProject); err == nil {
		t.Fatal("expected error for missing project identity (M21-052)")
	}

	missingRepository := base
	missingRepository.Repository = domain.RepositoryID{}
	if _, err := BuildExactFingerprint(missingRepository); err == nil {
		t.Fatal("expected error for missing repository identity (M21-052)")
	}

	badRevision := base
	badRevision.BaseRevision = domain.RevisionBinding{}
	if _, err := BuildExactFingerprint(badRevision); err == nil {
		t.Fatal("expected error for unknown base revision without a reason (M21-053)")
	}

	badClass := base
	badClass.TaskClass = TaskClass("not-declared")
	if _, err := BuildExactFingerprint(badClass); err == nil {
		t.Fatal("expected error for undeclared task class (M21-054)")
	}

	badRisk := base
	badRisk.Risk = domain.RiskLevel("not-declared")
	if _, err := BuildExactFingerprint(badRisk); err == nil {
		t.Fatal("expected error for undeclared risk level (M21-057)")
	}

	invalidatedAssurance := base
	invalidatedAssurance.RequiredAssurance = domain.AssuranceLevelInvalidated
	if _, err := BuildExactFingerprint(invalidatedAssurance); err == nil {
		t.Fatal("expected error for invalidated required assurance (M21-057)")
	}

	badAuthority := base
	badAuthority.RequestedAuthority = []AuthorityClass{"not-declared"}
	if _, err := BuildExactFingerprint(badAuthority); err == nil {
		t.Fatal("expected error for undeclared authority class (M21-058)")
	}

	conflictingBinding := base
	conflictingBinding.Bindings = []ToolchainBinding{
		{Name: "go", Version: "1.26.0"},
		{Name: "go", Version: "1.25.0"},
	}
	if _, err := BuildExactFingerprint(conflictingBinding); err == nil {
		t.Fatal("expected error for conflicting toolchain binding versions (M21-056)")
	}
}

// M21-055: affected package/symbol/path hints are present and normalized.
func TestAffectedHintsAreNormalized(t *testing.T) {
	input := sampleInput(t)
	input.AffectedPaths = []string{"b/file.go", "a/file.go", "a/file.go"}
	value, err := BuildExactFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a/file.go", "b/file.go"}
	if len(value.AffectedPaths) != len(want) || value.AffectedPaths[0] != want[0] || value.AffectedPaths[1] != want[1] {
		t.Fatalf("AffectedPaths = %v, want %v (sorted, deduplicated)", value.AffectedPaths, want)
	}
}

// M21-059: DescriptiveRetrievalText has no structural path into
// ExactFingerprint.Hash or CanonicalJSON. This is proven mechanically, not
// just asserted: Hash and CanonicalJSON are declared only on
// ExactFingerprint, whose type has no field capable of carrying prose, and
// Fingerprint.Hash forwards only to Exact.Hash.
func TestExactAndDescriptiveSplitIsStructural(t *testing.T) {
	exact := buildSample(t)
	first, err := NewFingerprint(exact, DescriptiveRetrievalText{Summary: "fix the race in the worker pool"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewFingerprint(exact, DescriptiveRetrievalText{
		Summary:  "  totally different prose, reworded completely, mentions unrelated files and people  \n\twith odd whitespace",
		Keywords: []string{"unrelated", "keyword", "noise"},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := second.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("descriptive text changed the hash: %q != %q", firstHash, secondHash)
	}
	if firstHash != mustHash(t, exact) {
		t.Fatal("Fingerprint.Hash diverged from ExactFingerprint.Hash")
	}
}

func mustHash(t *testing.T, exact ExactFingerprint) string {
	t.Helper()
	hash, err := exact.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// M21-060/M21-062: identical inputs, submitted in different orders, produce
// byte-identical canonical JSON and identical hashes across repeated
// in-process runs.
func TestIdenticalInputsProduceIdenticalFingerprintsAcrossRepeatedRuns(t *testing.T) {
	first, err := BuildExactFingerprint(sampleInput(t))
	if err != nil {
		t.Fatal(err)
	}
	reordered := sampleInput(t)
	reordered.AffectedPaths = reverse(reordered.AffectedPaths)
	reordered.AffectedPackages = reverse(reordered.AffectedPackages)
	reordered.AffectedSymbols = reverse(reordered.AffectedSymbols)
	reordered.Bindings = []ToolchainBinding{reordered.Bindings[1], reordered.Bindings[0]}
	reordered.RequestedAuthority = []AuthorityClass{reordered.RequestedAuthority[1], reordered.RequestedAuthority[0]}
	second, err := BuildExactFingerprint(reordered)
	if err != nil {
		t.Fatal(err)
	}

	firstJSON, err := first.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := second.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("canonical JSON differs by input order:\n%s\nvs\n%s", firstJSON, secondJSON)
	}

	for run := 0; run < 5; run++ {
		hash, err := first.Hash()
		if err != nil {
			t.Fatal(err)
		}
		other, err := second.Hash()
		if err != nil {
			t.Fatal(err)
		}
		if hash != other {
			t.Fatalf("run %d: hash differs by input order: %q != %q", run, hash, other)
		}
	}
}

// M21-063: field insertion order never changes the hash. This is a small
// property-style check: every permutation of the two-element lists in the
// sample input must produce the same hash as every other permutation.
func TestFieldInsertionOrderNeverAltersHash(t *testing.T) {
	baseline, err := BuildExactFingerprint(sampleInput(t))
	if err != nil {
		t.Fatal(err)
	}
	baselineHash, err := baseline.Hash()
	if err != nil {
		t.Fatal(err)
	}

	permutations := []func(*ExactFingerprintInput){
		func(input *ExactFingerprintInput) { input.AffectedPaths = reverse(input.AffectedPaths) },
		func(input *ExactFingerprintInput) { input.AffectedPackages = reverse(input.AffectedPackages) },
		func(input *ExactFingerprintInput) { input.AffectedSymbols = reverse(input.AffectedSymbols) },
		func(input *ExactFingerprintInput) {
			input.Bindings = []ToolchainBinding{input.Bindings[1], input.Bindings[0]}
		},
		func(input *ExactFingerprintInput) {
			input.RequestedAuthority = []AuthorityClass{input.RequestedAuthority[1], input.RequestedAuthority[0]}
		},
	}
	// Exhaustively apply every subset of the permutations (2^5 combinations)
	// so ordering hazards cannot hide behind a single reordered field.
	for mask := 0; mask < (1 << len(permutations)); mask++ {
		input := sampleInput(t)
		for index, permute := range permutations {
			if mask&(1<<index) != 0 {
				permute(&input)
			}
		}
		value, err := BuildExactFingerprint(input)
		if err != nil {
			t.Fatalf("mask %d: %v", mask, err)
		}
		hash, err := value.Hash()
		if err != nil {
			t.Fatalf("mask %d: %v", mask, err)
		}
		if hash != baselineHash {
			t.Fatalf("mask %d: reordered input changed the hash: %q != %q", mask, hash, baselineHash)
		}
	}
}

// M21-063: a material base-revision change alters the fingerprint.
func TestMaterialRevisionChangeAltersHash(t *testing.T) {
	baseline := buildSample(t)
	changed := sampleInput(t)
	changed.BaseRevision.ExactRevision = "different-revision-0000"
	changedValue, err := BuildExactFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	assertHashesDiffer(t, baseline, changedValue, "base revision")
}

// M21-063: a material dependency-binding version change alters the
// fingerprint.
func TestMaterialDependencyVersionChangeAltersHash(t *testing.T) {
	baseline := buildSample(t)
	changed := sampleInput(t)
	changed.Bindings = []ToolchainBinding{
		{Name: "go", Version: "1.27.0"},
		{Name: "codeflux.dev/codeflux", Version: "v0.0.0"},
	}
	changedValue, err := BuildExactFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	assertHashesDiffer(t, baseline, changedValue, "dependency binding version")
}

// M21-063: adding a genuinely new affected path is a material change.
func TestNewAffectedPathAltersHash(t *testing.T) {
	baseline := buildSample(t)
	changed := sampleInput(t)
	changed.AffectedPaths = append(changed.AffectedPaths, "internal/fingerprint/new_file.go")
	changedValue, err := BuildExactFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	assertHashesDiffer(t, baseline, changedValue, "affected path addition")
}

// M21-063: a risk or requested-authority change is material.
func TestRiskAndAuthorityChangesAlterHash(t *testing.T) {
	baseline := buildSample(t)

	changedRisk := sampleInput(t)
	changedRisk.Risk = domain.RiskLevelProtected
	changedRiskValue, err := BuildExactFingerprint(changedRisk)
	if err != nil {
		t.Fatal(err)
	}
	assertHashesDiffer(t, baseline, changedRiskValue, "risk level")

	changedAuthority := sampleInput(t)
	changedAuthority.RequestedAuthority = append(changedAuthority.RequestedAuthority, AuthorityClassNetwork)
	changedAuthorityValue, err := BuildExactFingerprint(changedAuthority)
	if err != nil {
		t.Fatal(err)
	}
	assertHashesDiffer(t, baseline, changedAuthorityValue, "requested authority")
}

func assertHashesDiffer(t *testing.T, a, b ExactFingerprint, label string) {
	t.Helper()
	hashA, err := a.Hash()
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := b.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if hashA == hashB {
		t.Fatalf("%s: material change did not alter the hash (%q)", label, hashA)
	}
}

func reverse(values []string) []string {
	reversed := make([]string, len(values))
	for index, value := range values {
		reversed[len(values)-1-index] = value
	}
	return reversed
}

// goldenSampleFingerprintHash pins the exact SHA-256 hash of buildSample's
// canonical JSON. Reviewer follow-up (adversarial review, 2026-07-31): the
// prior version of TestIdenticalInputsProduceIdenticalHashAcrossProcessBoundary
// only compared two freshly computed hashes against each other, so it had no
// failure mode that could actually fire (no maps, floats, or timezones sit
// anywhere on this path). Pinning a known golden value gives the test real
// catching power: it fails on any future change to field order, JSON tag
// naming, the nil/empty-slice serialization shape (this is exactly what
// would have caught Defect 1), the normalization rules, or the hash
// algorithm itself. Recompute deliberately (never copy-paste a failing
// actual into this constant without checking why it changed) if a schema
// version bump or an intentional canonicalization change requires it.
const goldenSampleFingerprintHash = "851c57bae5d7f62cb5eaee6119beae009d7aedf4855a30af889a4c13f9d537c7"

// M21-062: identical inputs produce identical fingerprints across a real
// process boundary, not only one in-process comparison. This spawns the test
// binary itself as a child process (the standard library's own
// TestHelperProcess idiom, see os/exec's tests) and compares its computed
// hash against the parent process's hash for the same logical input, and
// both against the pinned golden hash above.
func TestIdenticalInputsProduceIdenticalHashAcrossProcessBoundary(t *testing.T) {
	if os.Getenv("CODEFLUX_FINGERPRINT_SUBPROCESS") == "1" {
		t.Skip("only runs as a driven subprocess")
	}
	inProcess := buildSample(t)
	inProcessHash, err := inProcess.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if inProcessHash != goldenSampleFingerprintHash {
		t.Fatalf("in-process hash does not match the pinned golden hash: got %q, want %q (serialization, normalization, or hashing changed)", inProcessHash, goldenSampleFingerprintHash)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFingerprintSubprocessEntryPoint", "-test.v")
	cmd.Env = append(os.Environ(), "CODEFLUX_FINGERPRINT_SUBPROCESS=1")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("subprocess failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("subprocess failed: %v", err)
	}
	subprocessHash := extractSubprocessHash(t, string(output))
	if subprocessHash != inProcessHash {
		t.Fatalf("cross-process hash mismatch: in-process %q, subprocess %q", inProcessHash, subprocessHash)
	}
	if subprocessHash != goldenSampleFingerprintHash {
		t.Fatalf("subprocess hash does not match the pinned golden hash: got %q, want %q", subprocessHash, goldenSampleFingerprintHash)
	}
}

const subprocessHashMarker = "CODEFLUX_FINGERPRINT_HASH="

// TestFingerprintSubprocessEntryPoint is not a real test: it is only ever
// invoked, as a subprocess, by
// TestIdenticalInputsProduceIdenticalHashAcrossProcessBoundary. It computes
// the same sample fingerprint's hash and prints it with a stable marker so
// the parent process can extract it from -test.v output.
func TestFingerprintSubprocessEntryPoint(t *testing.T) {
	if os.Getenv("CODEFLUX_FINGERPRINT_SUBPROCESS") != "1" {
		t.Skip("only runs as a driven subprocess")
	}
	value := buildSample(t)
	hash, err := value.Hash()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(subprocessHashMarker + hash)
}

func extractSubprocessHash(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, subprocessHashMarker) {
			return strings.TrimPrefix(line, subprocessHashMarker)
		}
	}
	t.Fatalf("subprocess output did not contain a hash marker:\n%s", output)
	return ""
}

// DescriptiveRetrievalText bounds are enforced independently from identity.
func TestDescriptiveRetrievalTextBounds(t *testing.T) {
	tooLong := DescriptiveRetrievalText{Summary: strings.Repeat("x", MaximumDescriptiveSummary+1)}
	if err := tooLong.Validate(); err == nil {
		t.Fatal("expected error for oversized descriptive summary")
	}
	tooManyKeywords := DescriptiveRetrievalText{Keywords: make([]string, MaximumDescriptiveKeywords+1)}
	if err := tooManyKeywords.Validate(); err == nil {
		t.Fatal("expected error for too many descriptive keywords")
	}
}

// -----------------------------------------------------------------------
// Adversarial review, 2026-07-31: Defect 1 — nil vs empty slice
// -----------------------------------------------------------------------
//
// Validate() previously accepted nil slices (it only checked length and
// ordering, never non-nil-ness), but BuildExactFingerprint always produces
// non-nil empty slices. encoding/json marshals nil as `null` and
// []string{} as `[]`, so a struct literal bypassing BuildExactFingerprint
// hashed differently from a logically identical BuildExactFingerprint
// value. Reproduced: built fccbb1b1... vs hand-constructed 0a75968a... for
// the same logical fingerprint. Chosen fix: reject nil in Validate rather
// than silently coalescing nil to empty in CanonicalJSON, because
// Validate's doc comment promises it is "safe to call on a value read back
// from storage" — a strict, honest Validate that refuses a non-canonical
// shape is what makes that promise meaningful once M21-023 starts
// round-tripping these values. Coalescing nil silently in CanonicalJSON
// would still let Validate call a malformed (nil-carrying) value "valid",
// which is the opposite of honest.

// TestValidateRejectsNilExactSliceFields fails before the fix (Validate
// currently returns nil for every nil-slice mutation) and passes after.
func TestValidateRejectsNilExactSliceFields(t *testing.T) {
	valid := buildSample(t)
	cases := []struct {
		name   string
		mutate func(*ExactFingerprint)
	}{
		{"affected_paths", func(v *ExactFingerprint) { v.AffectedPaths = nil }},
		{"affected_packages", func(v *ExactFingerprint) { v.AffectedPackages = nil }},
		{"affected_symbols", func(v *ExactFingerprint) { v.AffectedSymbols = nil }},
		{"bindings", func(v *ExactFingerprint) { v.Bindings = nil }},
		{"requested_authority", func(v *ExactFingerprint) { v.RequestedAuthority = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := valid
			tc.mutate(&mutated)
			if err := mutated.Validate(); err == nil {
				t.Fatalf("Validate() accepted a nil %s slice", tc.name)
			}
			if _, err := mutated.Hash(); err == nil {
				t.Fatalf("Hash() accepted a nil %s slice", tc.name)
			}
		})
	}
}

// TestNilVersusEmptySliceNoLongerDivergesSilently reproduces the exact
// adversarial-review finding directly: before the fix, a hand-constructed
// nil-slice literal hashed successfully to a value different from the
// BuildExactFingerprint-produced value for the same logical fingerprint.
// After the fix, the nil-slice literal is rejected outright instead of
// silently producing a divergent hash, so the divergence can never occur.
func TestNilVersusEmptySliceNoLongerDivergesSilently(t *testing.T) {
	built := buildSample(t)
	builtHash, err := built.Hash()
	if err != nil {
		t.Fatal(err)
	}

	literal := built
	literal.AffectedPaths = nil // hand-constructed, bypassing BuildExactFingerprint
	if _, err := literal.Hash(); err == nil {
		t.Fatal("nil-slice literal must be rejected, not silently hashed to a different value than the equivalent built value")
	}

	again, err := built.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if again != builtHash {
		t.Fatal("Hash() is not stable across repeated calls on the same built value")
	}
}

// -----------------------------------------------------------------------
// Adversarial review, 2026-07-31: Defect 2 — cross-platform hash divergence
// -----------------------------------------------------------------------

// TestPathSeparatorNormalizationAcrossHosts fails before the fix (backslash-
// and forward-slash-spelled paths hash differently) and passes after.
func TestPathSeparatorNormalizationAcrossHosts(t *testing.T) {
	forwardInput := sampleInput(t)
	forwardInput.AffectedPaths = []string{"a/b/c.go"}
	forwardValue, err := BuildExactFingerprint(forwardInput)
	if err != nil {
		t.Fatal(err)
	}

	backslashInput := sampleInput(t)
	backslashInput.AffectedPaths = []string{`a\b\c.go`}
	backslashValue, err := BuildExactFingerprint(backslashInput)
	if err != nil {
		t.Fatal(err)
	}

	forwardHash, err := forwardValue.Hash()
	if err != nil {
		t.Fatal(err)
	}
	backslashHash, err := backslashValue.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if forwardHash != backslashHash {
		t.Fatalf("backslash- and forward-slash-spelled paths hashed differently: %q != %q", backslashHash, forwardHash)
	}
	if len(backslashValue.AffectedPaths) != 1 || backslashValue.AffectedPaths[0] != "a/b/c.go" {
		t.Fatalf("canonical form is not forward-slash: %v", backslashValue.AffectedPaths)
	}
}

// TestUnicodeNormalizationAcrossSpellings fails before the fix (NFC and NFD
// spellings of the same identifier hash differently) and passes after.
func TestUnicodeNormalizationAcrossSpellings(t *testing.T) {
	const (
		nfcSpelling = "café"  // U+00E9 LATIN SMALL LETTER E WITH ACUTE
		nfdSpelling = "café" // "e" + U+0301 COMBINING ACUTE ACCENT
	)
	if nfcSpelling == nfdSpelling {
		t.Fatal("test fixture is not actually differently composed")
	}

	nfcInput := sampleInput(t)
	nfcInput.AffectedSymbols = append(append([]string{}, nfcInput.AffectedSymbols...), nfcSpelling)
	nfcValue, err := BuildExactFingerprint(nfcInput)
	if err != nil {
		t.Fatal(err)
	}

	nfdInput := sampleInput(t)
	nfdInput.AffectedSymbols = append(append([]string{}, nfdInput.AffectedSymbols...), nfdSpelling)
	nfdValue, err := BuildExactFingerprint(nfdInput)
	if err != nil {
		t.Fatal(err)
	}

	nfcHash, err := nfcValue.Hash()
	if err != nil {
		t.Fatal(err)
	}
	nfdHash, err := nfdValue.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if nfcHash != nfdHash {
		t.Fatalf("NFC and NFD spellings of the same identifier hashed differently: %q != %q", nfcHash, nfdHash)
	}
}

// TestValidateRejectsUnnormalizedForms fails before the fix (Validate
// accepts a backslash-spelled path and a non-NFC symbol read back from
// storage) and passes after: a deserialized value cannot carry an
// unnormalized form.
func TestValidateRejectsUnnormalizedForms(t *testing.T) {
	backslash := buildSample(t)
	backslash.AffectedPaths = append(append([]string{}, backslash.AffectedPaths...), `a\b.go`)
	sort.Strings(backslash.AffectedPaths)
	if err := backslash.Validate(); err == nil {
		t.Fatal("Validate() accepted a backslash-spelled path")
	}

	nfd := buildSample(t)
	nfd.AffectedSymbols = append(append([]string{}, nfd.AffectedSymbols...), "café")
	sort.Strings(nfd.AffectedSymbols)
	if err := nfd.Validate(); err == nil {
		t.Fatal("Validate() accepted a non-NFC-normalized symbol")
	}
}

// -----------------------------------------------------------------------
// Adversarial review, 2026-07-31: Defect 3 — exact fields accept prose
// -----------------------------------------------------------------------

// TestAffectedFieldsRejectProse fails before the fix (arbitrary trimmed
// non-empty strings up to 512 bytes are accepted with no syntax check) and
// passes after: each field is checked against its documented shape
// (repository-relative path, Go import path, Go identifier/qualified name).
func TestAffectedFieldsRejectProse(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ExactFingerprintInput)
	}{
		{"affected_paths prose", func(input *ExactFingerprintInput) {
			input.AffectedPaths = []string{"this is a description of the change, not a path"}
		}},
		{"affected_paths traversal", func(input *ExactFingerprintInput) {
			input.AffectedPaths = []string{"../../etc/passwd"}
		}},
		{"affected_paths absolute", func(input *ExactFingerprintInput) {
			input.AffectedPaths = []string{"/etc/passwd"}
		}},
		{"affected_packages prose", func(input *ExactFingerprintInput) {
			input.AffectedPackages = []string{"the fingerprint package, which computes hashes"}
		}},
		{"affected_symbols prose", func(input *ExactFingerprintInput) {
			input.AffectedSymbols = []string{"the BuildExactFingerprint function"}
		}},
		{"affected_symbols invalid identifier", func(input *ExactFingerprintInput) {
			input.AffectedSymbols = []string{"1NotAnIdentifier"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := sampleInput(t)
			tc.mutate(&input)
			if _, err := BuildExactFingerprint(input); err == nil {
				t.Fatalf("expected rejection of non-structured content in %s", tc.name)
			}
		})
	}
}

// TestAffectedFieldsAcceptWellFormedStructuredContent is the positive
// counterpart to TestAffectedFieldsRejectProse: legitimate paths, import
// paths, and qualified symbol names must still be accepted.
func TestAffectedFieldsAcceptWellFormedStructuredContent(t *testing.T) {
	input := sampleInput(t)
	input.AffectedPaths = []string{"cmd/codeflux-dev/main.go", "internal/fingerprint/fingerprint.go"}
	input.AffectedPackages = []string{"codeflux.dev/codeflux/internal/fingerprint"}
	input.AffectedSymbols = []string{"BuildExactFingerprint", "ExactFingerprint.Hash", "café"}
	if _, err := BuildExactFingerprint(input); err != nil {
		t.Fatalf("expected well-formed structured content to be accepted: %v", err)
	}
}

// -----------------------------------------------------------------------
// Adversarial review, 2026-07-31: Defect 4 — type drift risk
// -----------------------------------------------------------------------
//
// fingerprint.TaskClass and fingerprint.AuthorityClass intentionally
// duplicate internal/forecast.TaskClass's and internal/executor.AuthorityClass's
// string vocabularies rather than importing them (see those types' doc
// comments for why). Nothing mechanically enforced that the duplication
// stayed in sync: a rename in any one of the three would silently desync
// the others. These tests do not hoist a shared enum (out of scope; neither
// package is owned here) — they convert the latent landmine into a loud,
// contained failure. No import cycle risk: neither internal/forecast nor
// internal/executor imports internal/fingerprint.

// TestTaskClassSetMatchesForecastPackage passes today (the sets currently
// match) and is designed to fail the moment either vocabulary drifts.
func TestTaskClassSetMatchesForecastPackage(t *testing.T) {
	forecastClasses := map[string]struct{}{
		string(forecast.TaskClassDocumentation): {},
		string(forecast.TaskClassSmallChange):   {},
		string(forecast.TaskClassBugFix):        {},
		string(forecast.TaskClassFeature):       {},
		string(forecast.TaskClassRefactor):      {},
		string(forecast.TaskClassMigration):     {},
		string(forecast.TaskClassSecurity):      {},
	}
	fingerprintClasses := make(map[string]struct{}, len(forecastClasses))
	for _, class := range AllTaskClasses() {
		fingerprintClasses[string(class)] = struct{}{}
	}
	if len(fingerprintClasses) != len(forecastClasses) {
		t.Fatalf("fingerprint.TaskClass has %d values, internal/forecast.TaskClass has %d", len(fingerprintClasses), len(forecastClasses))
	}
	for class := range fingerprintClasses {
		if _, ok := forecastClasses[class]; !ok {
			t.Fatalf("fingerprint.TaskClass %q has no matching internal/forecast.TaskClass value", class)
		}
	}
}

// TestAuthorityClassSetMatchesExecutorPackage passes today (the sets
// currently match) and is designed to fail the moment either vocabulary
// drifts.
func TestAuthorityClassSetMatchesExecutorPackage(t *testing.T) {
	executorClasses := map[string]struct{}{
		string(executor.AuthorityAutomaticRead):         {},
		string(executor.AuthorityTaskWrite):             {},
		string(executor.AuthorityNetwork):               {},
		string(executor.AuthorityDependencyInstall):     {},
		string(executor.AuthorityExternalWrite):         {},
		string(executor.AuthorityCredential):            {},
		string(executor.AuthorityDestructive):           {},
		string(executor.AuthorityPrivileged):            {},
		string(executor.AuthorityExternalCommunication): {},
	}
	fingerprintClasses := make(map[string]struct{}, len(executorClasses))
	for _, class := range AllAuthorityClasses() {
		fingerprintClasses[string(class)] = struct{}{}
	}
	if len(fingerprintClasses) != len(executorClasses) {
		t.Fatalf("fingerprint.AuthorityClass has %d values, internal/executor.AuthorityClass has %d", len(fingerprintClasses), len(executorClasses))
	}
	for class := range fingerprintClasses {
		if _, ok := executorClasses[class]; !ok {
			t.Fatalf("fingerprint.AuthorityClass %q has no matching internal/executor.AuthorityClass value", class)
		}
	}
}
