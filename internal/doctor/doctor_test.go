package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// healthyInput supplies every check with a passing answer.
func healthyInput() Input {
	return Input{
		DatabasePath:  "/fixture/codeflux.sqlite3",
		WorktreeRoot:  "/fixture/worktrees",
		ListenAddress: "127.0.0.1:0",
		GoVersion:     func() string { return "go1.26.3" },
		GitVersion: func(context.Context) (string, error) {
			return "2.45.0", nil
		},
		PathWritable: func(string) error { return nil },
		DatabaseHealth: func(context.Context, string) (DatabaseHealth, error) {
			return DatabaseHealth{
				IntegrityOK: true, SchemaVersion: 29, SupportedSchemaVersion: 29,
			}, nil
		},
		CredentialStore: func() (bool, string) { return true, "windows-credential-manager" },
		ProviderReachable: func(context.Context) ([]ProviderReachability, error) {
			return []ProviderReachability{
				{Name: "anthropic", Reachable: true, Authorized: true},
			}, nil
		},
		DiskFree:     func(string) (uint64, error) { return 8 << 30, nil },
		PortBindable: func(string) error { return nil },
		TaskCounts: func(context.Context) (TaskCounts, error) {
			return TaskCounts{Active: 1, Paused: 0, RecoveryRequired: 0}, nil
		},
		Versions: func() Versions {
			return Versions{
				Application: "0.1.0", Commit: "abc1234",
				SchemaVersion: 29, SupportedSchemaVersion: 29, AppliedMigrations: 29,
			}
		},
	}
}

func steppingClock() func() time.Time {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		current = current.Add(time.Millisecond)
		return current
	}
}

// TestM23_026_035_EveryCheckIsDeclaredAndRuns covers M23-026..035.
func TestM23_026_035_EveryCheckIsDeclaredAndRuns(t *testing.T) {
	if err := ValidateChecks(); err != nil {
		t.Fatalf("the check set is invalid: %v", err)
	}

	report, err := Run(t.Context(), healthyInput(), steppingClock())
	if err != nil {
		t.Fatalf("doctor run: %v", err)
	}
	if len(report.Results) != len(AllCheckIDs()) {
		t.Fatalf("ran %d checks of %d", len(report.Results), len(AllCheckIDs()))
	}
	if !report.Healthy() {
		blocking, _ := report.FirstBlocking()
		t.Fatalf("a healthy system reported %d blocking results, first: %+v",
			report.Blocking, blocking)
	}
	for _, result := range report.Results {
		if result.Status != StatusOK {
			t.Fatalf("check %q reported %q on a healthy system: %s",
				result.ID, result.Status, result.Summary)
		}
		if strings.TrimSpace(result.Summary) == "" {
			t.Fatalf("check %q reported no summary", result.ID)
		}
	}

	// Each of M23-026..035 must be claimed by exactly one check.
	todos := CheckTodos()
	for number := 26; number <= 35; number++ {
		want := "M23-0" + itoa(number)
		found := 0
		for _, todo := range todos {
			if todo == want {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("%s is claimed by %d checks", want, found)
		}
	}
}

// TestM23_036_EveryFailureCarriesActionableRemediation covers M23-036.
//
// This is the property that makes a doctor useful rather than merely honest:
// every non-ok result must say what to do next, and the run refuses to
// produce one that does not.
func TestM23_036_EveryFailureCarriesActionableRemediation(t *testing.T) {
	broken := map[string]func(Input) Input{
		"no git": func(input Input) Input {
			input.GitVersion = func(context.Context) (string, error) {
				return "", errors.New("not found")
			}
			return input
		},
		"database unwritable": func(input Input) Input {
			input.PathWritable = func(string) error { return errors.New("denied") }
			return input
		},
		"database damaged": func(input Input) Input {
			input.DatabaseHealth = func(context.Context, string) (DatabaseHealth, error) {
				return DatabaseHealth{IntegrityOK: false}, nil
			}
			return input
		},
		"no database": func(input Input) Input {
			input.DatabaseHealth = func(context.Context, string) (DatabaseHealth, error) {
				return DatabaseHealth{}, errors.New("not found")
			}
			return input
		},
		"failed migrations": func(input Input) Input {
			input.DatabaseHealth = func(context.Context, string) (DatabaseHealth, error) {
				return DatabaseHealth{
					IntegrityOK: true, SchemaVersion: 29,
					SupportedSchemaVersion: 29, FailedMigrations: 2,
				}, nil
			}
			return input
		},
		"no credential store": func(input Input) Input {
			input.CredentialStore = func() (bool, string) { return false, "none" }
			return input
		},
		"no provider": func(input Input) Input {
			input.ProviderReachable = func(context.Context) ([]ProviderReachability, error) {
				return nil, nil
			}
			return input
		},
		"provider rejected the key": func(input Input) Input {
			input.ProviderReachable = func(context.Context) ([]ProviderReachability, error) {
				return []ProviderReachability{
					{Name: "anthropic", Reachable: true, Authorized: false},
				}, nil
			}
			return input
		},
		"provider unreachable": func(input Input) Input {
			input.ProviderReachable = func(context.Context) ([]ProviderReachability, error) {
				return []ProviderReachability{{Name: "anthropic"}}, nil
			}
			return input
		},
		"low disk": func(input Input) Input {
			input.DiskFree = func(string) (uint64, error) { return 1 << 20, nil }
			return input
		},
		"port taken": func(input Input) Input {
			input.PortBindable = func(string) error { return errors.New("in use") }
			return input
		},
		"tasks need recovery": func(input Input) Input {
			input.TaskCounts = func(context.Context) (TaskCounts, error) {
				return TaskCounts{Active: 0, RecoveryRequired: 2}, nil
			}
			return input
		},
		"schema ahead of build": func(input Input) Input {
			input.Versions = func() Versions {
				return Versions{
					Application: "0.1.0", Commit: "abc1234",
					SchemaVersion: 40, SupportedSchemaVersion: 29,
				}
			}
			return input
		},
		"schema behind build": func(input Input) Input {
			input.Versions = func() Versions {
				return Versions{
					Application: "0.1.0", Commit: "abc1234",
					SchemaVersion: 20, SupportedSchemaVersion: 29,
				}
			}
			return input
		},
	}
	for name, corrupt := range broken {
		t.Run(name, func(t *testing.T) {
			report, err := Run(t.Context(), corrupt(healthyInput()), steppingClock())
			if err != nil {
				t.Fatalf("doctor run: %v", err)
			}
			degradedOrWorse := 0
			for _, result := range report.Results {
				if result.Status == StatusOK {
					continue
				}
				degradedOrWorse++
				if strings.TrimSpace(result.Remediation) == "" {
					t.Fatalf("check %q reported %q with no remediation",
						result.ID, result.Status)
				}
				// A remediation must tell the user to do something, not merely
				// restate the problem.
				if len(strings.Fields(result.Remediation)) < 4 {
					t.Fatalf("check %q gives an unusably short remediation: %q",
						result.ID, result.Remediation)
				}
			}
			if degradedOrWorse == 0 {
				t.Fatalf("%q produced no non-ok result", name)
			}
		})
	}
}

// TestM23_031_ProviderCheckDistinguishesNetworkFromCredential is the M23-031
// property that decides whether the result is useful: "no network" and "wrong
// key" need different fixes, and conflating them sends a user to re-paste a
// key that was fine.
func TestM23_031_ProviderCheckDistinguishesNetworkFromCredential(t *testing.T) {
	unreachable := healthyInput()
	unreachable.ProviderReachable = func(context.Context) ([]ProviderReachability, error) {
		return []ProviderReachability{{Name: "anthropic"}}, nil
	}
	result := checkProvider(t.Context(), unreachable)
	if result.Status != StatusDegraded {
		t.Fatalf("an unreachable provider reported %q", result.Status)
	}
	if !strings.Contains(result.Detail, "network") {
		t.Fatalf("an unreachable provider does not point at the network: %q", result.Detail)
	}
	if strings.Contains(strings.ToLower(result.Remediation), "key") &&
		!strings.Contains(result.Remediation, "does not need to change") {
		t.Fatalf("an unreachable provider sends the user to change their key: %q",
			result.Remediation)
	}

	rejected := healthyInput()
	rejected.ProviderReachable = func(context.Context) ([]ProviderReachability, error) {
		return []ProviderReachability{
			{Name: "anthropic", Reachable: true, Authorized: false},
		}, nil
	}
	result = checkProvider(t.Context(), rejected)
	if result.Status != StatusFailed {
		t.Fatalf("a rejected credential reported %q", result.Status)
	}
	if !strings.Contains(result.Remediation, "provider set") {
		t.Fatalf("a rejected credential does not say how to replace it: %q", result.Remediation)
	}
}

// TestM23_031_ProviderCheckNeverExposesCredentials is the other half of
// M23-031.
func TestM23_031_ProviderCheckNeverExposesCredentials(t *testing.T) {
	input := healthyInput()
	input.ProviderReachable = func(context.Context) ([]ProviderReachability, error) {
		return []ProviderReachability{
			{
				Name: "anthropic", Reachable: true, Authorized: false,
				FailureKind: "unauthorized",
			},
		}, nil
	}
	report, err := Run(t.Context(), input, steppingClock())
	if err != nil {
		t.Fatalf("doctor run: %v", err)
	}
	// Nothing in a doctor report may carry credential material or an endpoint:
	// a report is routinely pasted into a chat.
	for _, result := range report.Results {
		text := result.Summary + " " + result.Detail + " " + result.Remediation
		for _, secret := range testfixtures.FixtureCredentialShapes() {
			if strings.Contains(text, secret) {
				t.Fatalf("check %q leaked credential material", result.ID)
			}
		}
		if strings.Contains(text, "https://") || strings.Contains(text, "http://") {
			t.Fatalf("check %q leaked an endpoint: %q", result.ID, text)
		}
	}
}

// TestM23_026_035_UnrunnableChecksReportUnknownRatherThanFailure proves a
// check with no inputs does not invent a verdict.
func TestM23_026_035_UnrunnableChecksReportUnknownRatherThanFailure(t *testing.T) {
	report, err := Run(t.Context(), Input{}, steppingClock())
	if err != nil {
		t.Fatalf("doctor run: %v", err)
	}
	for _, result := range report.Results {
		if result.Status == StatusFailed {
			t.Fatalf("check %q reported a failure with no inputs: %s",
				result.ID, result.Summary)
		}
		if result.Status != StatusUnknown {
			t.Fatalf("check %q reported %q with no inputs", result.ID, result.Status)
		}
		if strings.TrimSpace(result.Remediation) == "" {
			t.Fatalf("check %q reported unknown with no remediation", result.ID)
		}
	}
	// Unknown blocks, because a system whose state cannot be determined is not
	// a system that should be relied on.
	if report.Healthy() {
		t.Fatal("a report of entirely unknown results claimed to be healthy")
	}
}

// TestM23_026_035_ResultValidationIsLoadBearing proves a malformed result
// cannot reach a user.
func TestM23_026_035_ResultValidationIsLoadBearing(t *testing.T) {
	valid := Result{
		ID: CheckGit, Todo: "M23-027", Status: StatusOK, Summary: "Git 2.45.0",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a valid result was rejected: %v", err)
	}
	corruptions := map[string]func(Result) Result{
		"unknown check": func(result Result) Result {
			result.ID = CheckID("invented")
			return result
		},
		"unknown status": func(result Result) Result {
			result.Status = Status("invented")
			return result
		},
		"no summary": func(result Result) Result {
			result.Summary = ""
			return result
		},
		"foreign todo": func(result Result) Result {
			result.Todo = "M22-001"
			return result
		},
		"failure without remediation": func(result Result) Result {
			result.Status = StatusFailed
			return result
		},
		"negative duration": func(result Result) Result {
			result.Duration = -time.Second
			return result
		},
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(valid).Validate(); err == nil {
				t.Fatalf("an unusable result validated: %s", name)
			}
		})
	}
	if _, err := Run(t.Context(), healthyInput(), nil); err == nil {
		t.Fatal("a doctor run with no clock succeeded")
	}
	if Status("invented").Valid() {
		t.Fatal("an unknown status validated")
	}
	if !Status("invented").Blocking() {
		t.Fatal("an unknown status does not block")
	}
}

// TestM23_037_040_ManifestPreviewsContentAndSizeBeforeExport covers M23-037,
// M23-038, and M23-040.
func TestM23_037_040_ManifestPreviewsContentAndSizeBeforeExport(t *testing.T) {
	estimate := func(section SectionID) (int64, int, error) {
		return 1024, 3, nil
	}
	manifest, err := BuildManifest(estimate, nil)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}

	// Every declared section must appear, included or not, so a user can see
	// what was left out rather than having to know to ask.
	if len(manifest.Sections) != len(AllSections()) {
		t.Fatalf("the manifest lists %d of %d sections",
			len(manifest.Sections), len(AllSections()))
	}
	for _, entry := range manifest.Sections {
		if strings.TrimSpace(entry.Description) == "" {
			t.Fatalf("section %q has no description", entry.ID)
		}
	}

	// M23-038: the default set is present.
	included := manifest.IncludedSections()
	if len(included) != len(DefaultSections()) {
		t.Fatalf("the default manifest includes %d sections, want %d: %v",
			len(included), len(DefaultSections()), included)
	}
	for _, section := range DefaultSections() {
		found := false
		for _, candidate := range included {
			if candidate == section {
				found = true
			}
		}
		if !found {
			t.Fatalf("the default manifest omits %q", section)
		}
	}

	// M23-040: the size must be previewed, and must be the sum of what is
	// actually included.
	want := int64(1024 * len(DefaultSections()))
	if manifest.TotalEstimatedBytes != want {
		t.Fatalf("estimated total = %d, want %d", manifest.TotalEstimatedBytes, want)
	}
	preview := manifest.Preview()
	joined := strings.Join(preview, "\n")
	if !strings.Contains(joined, "will contain") || !strings.Contains(joined, "NOT contain") {
		t.Fatalf("the preview does not show both halves:\n%s", joined)
	}
	if !strings.Contains(joined, "Estimated total") {
		t.Fatalf("the preview does not show a size:\n%s", joined)
	}
	for _, section := range SensitiveSections() {
		if !strings.Contains(joined, string(section)) {
			t.Fatalf("the preview does not mention excluded section %q:\n%s", section, joined)
		}
	}

	if _, err := BuildManifest(nil, nil); err == nil {
		t.Fatal("a manifest with no estimator was built")
	}
	if _, err := BuildManifest(estimate, []SectionID{SectionVersions}); err == nil {
		t.Fatal("a non-sensitive section was accepted as a confirmation")
	}
	failing := func(SectionID) (int64, int, error) { return 0, 0, errors.New("cannot size") }
	if _, err := BuildManifest(failing, nil); err == nil {
		t.Fatal("a failing estimator produced a manifest")
	}
	negative := func(SectionID) (int64, int, error) { return -1, 0, nil }
	if _, err := BuildManifest(negative, nil); err == nil {
		t.Fatal("a negative size produced a manifest")
	}
}

// TestM23_039_041_SensitiveContentIsExcludedUntilConfirmed covers M23-039 and
// M23-041.
func TestM23_039_041_SensitiveContentIsExcludedUntilConfirmed(t *testing.T) {
	estimate := func(SectionID) (int64, int, error) { return 2048, 5, nil }

	// M23-039: by default, none of it is included.
	manifest, err := BuildManifest(estimate, nil)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	for _, section := range SensitiveSections() {
		for _, entry := range manifest.Sections {
			if entry.ID == section && entry.Included {
				t.Fatalf("sensitive section %q was included by default", section)
			}
		}
	}
	if len(manifest.ConfirmedSensitive) != 0 {
		t.Fatalf("an unconfirmed manifest claims confirmations: %v", manifest.ConfirmedSensitive)
	}

	// M23-041: confirmation is per section. Confirming diffs must not include
	// prompts.
	manifest, err = BuildManifest(estimate, []SectionID{SectionSourceDiffs})
	if err != nil {
		t.Fatalf("build confirmed manifest: %v", err)
	}
	included := map[SectionID]bool{}
	for _, section := range manifest.IncludedSections() {
		included[section] = true
	}
	if !included[SectionSourceDiffs] {
		t.Fatal("a confirmed section was not included")
	}
	if included[SectionPrompts] || included[SectionTaskContent] {
		t.Fatal("confirming one sensitive section included another")
	}
	if len(manifest.ConfirmedSensitive) != 1 {
		t.Fatalf("confirmations = %v", manifest.ConfirmedSensitive)
	}
	if !strings.Contains(strings.Join(manifest.Preview(), "\n"), "You confirmed") {
		t.Fatal("the preview does not record what the user confirmed")
	}

	// The authorization gate refuses an unconfirmed request outright.
	if err := AuthorizeSensitive(
		[]SectionID{SectionPrompts}, []SectionID{SectionSourceDiffs},
	); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("an unconfirmed sensitive section was authorized: %v", err)
	}
	if err := AuthorizeSensitive(
		[]SectionID{SectionPrompts, SectionSourceDiffs},
		[]SectionID{SectionPrompts, SectionSourceDiffs},
	); err != nil {
		t.Fatalf("confirmed sections were refused: %v", err)
	}
	// A non-sensitive section never needs confirmation.
	if err := AuthorizeSensitive([]SectionID{SectionVersions}, nil); err != nil {
		t.Fatalf("a non-sensitive section required confirmation: %v", err)
	}

	// The description of a sensitive section must state plainly what it
	// reveals, since it is what the user reads before saying yes.
	for _, section := range SensitiveSections() {
		description := section.Describe()
		if !strings.Contains(description, "INCLUDING") &&
			!strings.Contains(description, "as written") {
			t.Fatalf("sensitive section %q understates what it reveals: %q",
				section, description)
		}
	}
}

// TestM23_042_ExportScanCatchesSeededSecrets covers M23-042.
func TestM23_042_ExportScanCatchesSeededSecrets(t *testing.T) {
	seeded := testfixtures.FixtureCredentialShapes()

	clean := map[SectionID]string{
		SectionVersions: "codeflux 0.1.0 schema 29",
		SectionHealth:   "git: ok\nstorage: ok",
	}
	findings, err := ScanForSeededSecrets(clean, seeded)
	if err != nil {
		t.Fatalf("scan clean bundle: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("a clean bundle produced findings: %v", findings)
	}

	// Every seeded shape must be detected, in whichever section carries it.
	for _, secret := range seeded {
		leaky := map[SectionID]string{
			SectionLogs: "provider error: auth failed for " + secret,
		}
		findings, err := ScanForSeededSecrets(leaky, seeded)
		if err != nil {
			t.Fatalf("scan leaky bundle: %v", err)
		}
		if len(findings) == 0 {
			t.Fatalf("seeded material %q was not detected", secret[:8])
		}
		for _, finding := range findings {
			if !strings.Contains(finding, string(SectionLogs)) {
				t.Fatalf("the finding does not name the section: %q", finding)
			}
			// The report must identify the leak without reproducing it.
			if strings.Contains(finding, secret) {
				t.Fatalf("the finding reproduced the secret: %q", finding)
			}
		}
	}

	// A scan with nothing to look for must refuse, or it would pass for every
	// bundle regardless of contents.
	if _, err := ScanForSeededSecrets(clean, nil); err == nil {
		t.Fatal("a scan with no seeded secrets succeeded")
	}
	if _, err := ScanForSeededSecrets(clean, []string{""}); err != nil {
		t.Fatalf("a blank seed should be skipped, not fail: %v", err)
	}
}

func itoa(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
