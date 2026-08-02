package coordinator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// recordingFlowApplier stands in for the running engine.
type recordingFlowApplier struct {
	applied pipeline.Settings
	calls   int
	refuse  error
}

func (applier *recordingFlowApplier) ApplyFlowSettings(
	settings pipeline.Settings,
) error {
	applier.calls++
	if applier.refuse != nil {
		return applier.refuse
	}
	applier.applied = settings
	return nil
}

// newFlowSettingsFixture starts the real application and reads its settings
// through the same application the service uses.
func newFlowSettingsFixture(
	t *testing.T,
	applier flowSettingsApplier,
) (settingsApplication, *Application) {
	t.Helper()
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	settings, err := newSettingsApplication(
		application.repos, application.credentials, applier,
	)
	if err != nil {
		t.Fatal(err)
	}
	return settings, application
}

// TestTheRunSettingsAreDescribedByTheEngineThatEnforcesThem is the surface's
// whole claim: what a page renders is what the engine declared, so a control
// cannot offer a value the engine would refuse.
func TestTheRunSettingsAreDescribedByTheEngineThatEnforcesThem(t *testing.T) {
	settings, _ := newFlowSettingsFixture(t, nil)
	record, err := settings.ReadFlowSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Settings) != len(pipeline.Describe()) {
		t.Fatalf("want every declared setting, got %d of %d",
			len(record.Settings), len(pipeline.Describe()))
	}
	// No layer has been written, so the revision is zero rather than invented.
	if record.Revision != 0 {
		t.Fatalf("revision = %d, want zero for compiled defaults", record.Revision)
	}
	byKey := map[string]transport.FlowSetting{}
	for _, setting := range record.Settings {
		byKey[setting.Key] = setting
	}
	attempts, present := byKey["maximum_attempts"]
	if !present {
		t.Fatalf("the attempt bound is missing: %+v", record.Settings)
	}
	if attempts.Kind != string(pipeline.ToggleNumber) ||
		attempts.Number != int32(pipeline.DefaultSettings().MaximumAttempts) {
		t.Fatalf("attempt bound lost a field: %+v", attempts)
	}
	// The bound and the help travel with the value, because a page carrying its
	// own copy would eventually offer a value the engine refuses.
	if attempts.Minimum == 0 && attempts.Maximum == 0 {
		t.Fatalf("the attempt bound carries no range: %+v", attempts)
	}
	if attempts.Help == "" || attempts.Group == "" {
		t.Fatalf("the attempt bound carries no description: %+v", attempts)
	}
	ambiguity := byKey["ambiguity"]
	if ambiguity.Kind != string(pipeline.ToggleChoice) ||
		len(ambiguity.Choices) != 2 || ambiguity.Text != pipeline.AmbiguityAsk {
		t.Fatalf("the ambiguity posture lost a field: %+v", ambiguity)
	}
	review := byKey["adversarial_review"]
	if review.Kind != string(pipeline.ToggleSwitch) || !review.Enabled {
		t.Fatalf("the review switch lost a field: %+v", review)
	}
}

// TestChangingARunSettingPersistsItAndHandsItToTheEngine is the other half:
// a change that is stored but not adopted would tell somebody their next run
// had changed when it had not.
func TestChangingARunSettingPersistsItAndHandsItToTheEngine(t *testing.T) {
	applier := &recordingFlowApplier{}
	settings, application := newFlowSettingsFixture(t, applier)

	saved, err := settings.WriteFlowSettings(t.Context(), transport.WriteFlowSettings{
		IdempotencyKey: "flow-1",
		Changes: []transport.FlowSettingChange{
			{Key: "maximum_attempts", Number: 9},
			{Key: "ambiguity", Text: pipeline.AmbiguityAssume},
			{Key: "adversarial_review", Enabled: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision == 0 {
		t.Fatal("a saved change must name the revision it created")
	}
	if applier.calls != 1 {
		t.Fatalf("the engine was handed the change %d times, want once", applier.calls)
	}
	if applier.applied.MaximumAttempts != 9 ||
		applier.applied.Ambiguity != pipeline.AmbiguityAssume ||
		applier.applied.AdversarialReview {
		t.Fatalf("the engine received %+v", applier.applied)
	}

	// The change survives a fresh read, and every untouched setting keeps the
	// value it had.
	reread, err := settings.ReadFlowSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]transport.FlowSetting{}
	for _, setting := range reread.Settings {
		byKey[setting.Key] = setting
	}
	if byKey["maximum_attempts"].Number != 9 ||
		byKey["ambiguity"].Text != pipeline.AmbiguityAssume ||
		byKey["adversarial_review"].Enabled {
		t.Fatalf("the stored settings lost a change: %+v", reread.Settings)
	}
	if byKey["repetition_runs"].Number != int32(pipeline.DefaultSettings().RepetitionRuns) {
		t.Fatalf("an untouched setting moved: %+v", byKey["repetition_runs"])
	}
	if reread.Revision != saved.Revision {
		t.Fatalf("revision = %d, want %d", reread.Revision, saved.Revision)
	}

	// The policy layer shares this document, so a flow write must not drop it.
	policyRecord, err := settings.ReadEffectivePolicy(t.Context())
	if err != nil {
		t.Fatalf("the policy became unreadable after a flow write: %v", err)
	}
	if policyRecord.Revision != saved.Revision {
		t.Fatalf("policy revision = %d, want %d", policyRecord.Revision, saved.Revision)
	}
	_ = application
}

// TestARunSettingTheEngineWouldRefuseIsRefusedHere keeps a bad value from
// becoming a run that does not do what its configuration says.
func TestARunSettingTheEngineWouldRefuseIsRefusedHere(t *testing.T) {
	applier := &recordingFlowApplier{}
	settings, _ := newFlowSettingsFixture(t, applier)

	_, err := settings.WriteFlowSettings(t.Context(), transport.WriteFlowSettings{
		IdempotencyKey: "flow-out-of-range",
		Changes:        []transport.FlowSettingChange{{Key: "maximum_attempts", Number: 400}},
	})
	var validation *transport.RequestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("out-of-range value = %v, want a validation error", err)
	}
	// The message names the setting and the bound, which is what somebody who
	// typed the value needs to read.
	if !strings.Contains(validation.Reason, "outside") {
		t.Fatalf("reason = %q", validation.Reason)
	}
	if applier.calls != 0 {
		t.Fatal("a refused configuration must never reach the engine")
	}

	// A key the engine does not declare is refused rather than ignored: a
	// client that sent it believes it changed something.
	if _, err := settings.WriteFlowSettings(t.Context(), transport.WriteFlowSettings{
		IdempotencyKey: "flow-unknown",
		Changes:        []transport.FlowSettingChange{{Key: "not_a_setting", Number: 1}},
	}); !errors.As(err, &validation) {
		t.Fatalf("unknown key = %v, want a validation error", err)
	}

	// Nothing was stored by either refusal.
	record, err := settings.ReadFlowSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if record.Revision != 0 {
		t.Fatalf("a refused change wrote revision %d", record.Revision)
	}
}

// TestARunSettingChangeAgainstAStaleViewIsRefused stops one page's save from
// silently overwriting another's.
func TestARunSettingChangeAgainstAStaleViewIsRefused(t *testing.T) {
	settings, _ := newFlowSettingsFixture(t, &recordingFlowApplier{})
	stale := uint64(41)
	_, err := settings.WriteFlowSettings(t.Context(), transport.WriteFlowSettings{
		IdempotencyKey: "flow-stale", ExpectedRevision: &stale,
		Changes: []transport.FlowSettingChange{{Key: "fuzz_seconds", Number: 5}},
	})
	if !errors.Is(err, transport.ErrSettingsRevisionConflict) {
		t.Fatalf("stale write = %v, want a revision conflict", err)
	}
}

// TestTheFlowLayerAndTheConfigurationLayerShareOneDocument pins the storage
// shape both readers depend on.
func TestTheFlowLayerAndTheConfigurationLayerShareOneDocument(t *testing.T) {
	settings, application := newFlowSettingsFixture(t, &recordingFlowApplier{})
	if _, err := application.repos.CreateSettingsRevision(
		t.Context(),
		storage.CreateSettingsRevision{
			Scope:             storage.SettingsScopeUser,
			ConfigurationJSON: `{"policy_preset":"correctness"}`,
			IdempotencyKey:    "configuration-first",
		},
	); err != nil {
		t.Fatal(err)
	}
	// A layer that says nothing about the flow is not a layer that says the
	// defaults are wrong; the same answer is reached honestly.
	record, err := settings.ReadFlowSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]transport.FlowSetting{}
	for _, setting := range record.Settings {
		byKey[setting.Key] = setting
	}
	if byKey["maximum_attempts"].Number != int32(pipeline.DefaultSettings().MaximumAttempts) {
		t.Fatalf("a configuration-only layer moved a run setting: %+v", byKey["maximum_attempts"])
	}

	if _, err := settings.WriteFlowSettings(t.Context(), transport.WriteFlowSettings{
		IdempotencyKey: "flow-after-configuration",
		Changes:        []transport.FlowSettingChange{{Key: "fuzz_seconds", Number: 0}},
	}); err != nil {
		t.Fatal(err)
	}
	// The preset the other layer set is still in force: a flow write carries
	// the rest of the document forward rather than rewriting it.
	policyRecord, err := settings.ReadEffectivePolicy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if string(policyRecord.Preset) != "correctness" {
		t.Fatalf("preset = %q, want the one the configuration layer set", policyRecord.Preset)
	}
}

// TestEverySettingTheEngineDeclaresResolvesToAField is the guard on the
// convention this surface reads by.
//
// The engine gains settings over time. A key that resolves to no field would
// otherwise render a control, show a zero, and silently drop what somebody
// typed into it — so the resolution is checked for every declared setting
// rather than trusted.
func TestEverySettingTheEngineDeclaresIsCarriedOrNamed(t *testing.T) {
	settings := pipeline.DefaultSettings()
	carried, unrenderable, err := describeFlowSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, setting := range carried {
		seen[setting.Key] = true
	}
	for _, key := range unrenderable {
		seen[key] = true
	}
	// Every declared setting is either drawn or named as one this boundary
	// cannot draw. A setting that fell through both would be missing from the
	// page with nothing saying so, which understates what governs a run.
	for _, toggle := range pipeline.Describe() {
		if !seen[toggle.Key] {
			t.Errorf("%s is neither carried nor reported as uncarried", toggle.Key)
		}
	}
	// A carried setting round-trips: what is written through this boundary is
	// what reads back out of it.
	for _, setting := range carried {
		toggle := declaredToggle(t, setting.Key)
		change := transport.FlowSettingChange{Key: setting.Key}
		switch setting.Kind {
		case string(pipeline.ToggleChoice):
			change.Text = firstChoice(toggle)
		case string(pipeline.ToggleNumber):
			change.Number = setting.Minimum
		case string(pipeline.ToggleSwitch):
			change.Enabled = !setting.Enabled
		case string(pipeline.ToggleSequence):
			// Reversed rather than replaced, because the order is what a
			// sequence setting means and writing the same order back would
			// pass whether or not the order survived the boundary.
			change.Items = reversed(toggle.Choices)
		case string(pipeline.ToggleSet):
			change.Items = firstChoices(toggle, 1)
		case string(pipeline.ToggleMapping):
			// A real key, so the mapping carries something. An empty one round-
			// trips trivially and would prove nothing about either direction.
			if stages := pipeline.ModelBearingStages(); len(stages) > 0 {
				change.Pairs = []transport.FlowSettingPair{
					{Key: stages[0], Value: firstChoice(toggle)},
				}
			}
		}
		written := settings
		if err := setFlowValue(&written, toggle, change); err != nil {
			t.Errorf("%s: %v", setting.Key, err)
			continue
		}
		readBack := transport.FlowSetting{}
		if err := readFlowValue(written, toggle, &readBack); err != nil {
			t.Errorf("%s: %v", setting.Key, err)
			continue
		}
		if readBack.Text != change.Text || readBack.Number != change.Number ||
			readBack.Enabled != change.Enabled ||
			!sameItems(readBack.Items, change.Items) ||
			!samePairs(readBack.Pairs, change.Pairs) {
			t.Errorf("%s did not round-trip: wrote %+v, read %+v",
				setting.Key, change, readBack)
		}
	}
}

// reversed is a copy of a list in the opposite order.
func reversed(items []string) []string {
	flipped := make([]string, 0, len(items))
	for index := len(items) - 1; index >= 0; index-- {
		flipped = append(flipped, items[index])
	}
	return flipped
}

// firstChoices is the first few options a setting offers.
func firstChoices(toggle pipeline.Toggle, count int) []string {
	if len(toggle.Choices) < count {
		count = len(toggle.Choices)
	}
	return append([]string(nil), toggle.Choices[:count]...)
}

// declaredToggle is the engine's declaration for one key.
func declaredToggle(t *testing.T, key string) pipeline.Toggle {
	t.Helper()
	for _, toggle := range pipeline.Describe() {
		if toggle.Key == key {
			return toggle
		}
	}
	t.Fatalf("the engine declares no setting named %q", key)
	return pipeline.Toggle{}
}

// firstChoice is the first option a choice setting offers.
func firstChoice(toggle pipeline.Toggle) string {
	if len(toggle.Choices) == 0 {
		return ""
	}
	return toggle.Choices[0]
}

// TestTheShippedConfigurationReportsItselfAsDefault is the guard on the marker
// the sheet colours rows by.
//
// If this drifts, a settings page tells somebody every value on it has been
// changed, which is the same as telling them nothing.
func TestTheShippedConfigurationReportsItselfAsDefault(t *testing.T) {
	described, unrenderable, err := describeFlowSettings(pipeline.DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range described {
		if !setting.AtDefault {
			t.Errorf("%s reports itself changed while holding the shipped value", setting.Key)
		}
	}
	// A setting the engine declares in a shape this boundary cannot carry is
	// named rather than rendered as a row with an empty value.
	for _, key := range unrenderable {
		t.Logf("not carried by this boundary: %s", key)
	}
	// And a value that really has moved is reported as moved.
	moved := pipeline.DefaultSettings()
	moved.MaximumAttempts = pipeline.DefaultSettings().MaximumAttempts + 1
	describedMoved, _, err := describeFlowSettings(moved)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range describedMoved {
		if setting.Key == "maximum_attempts" && setting.AtDefault {
			t.Fatal("a changed attempt ceiling reports itself as the default")
		}
	}
}
