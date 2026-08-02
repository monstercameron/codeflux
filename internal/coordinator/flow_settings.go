package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// flowSettingsDocumentKey is where the run flow's choices live inside the user
// settings layer.
//
// They share the layer with the non-secret configuration rather than taking a
// scope of their own, because they are the same thing to a person: one set of
// choices this machine has made, revised together and bound to the tasks that
// used them. The nesting keeps the two readable independently.
const flowSettingsDocumentKey = "flow"

// flowSettingsApplier is the running engine, which adopts a changed
// configuration without a restart.
//
// It is an interface so the settings path can be exercised without an engine,
// and so a coordinator with no agent configured still serves the surface: a
// person can read and change what a run will do before the first run exists.
type flowSettingsApplier interface {
	ApplyFlowSettings(pipeline.Settings) error
}

// flowSettingsFunc adapts an engine's own adoption method to the applier.
//
// The interface is one method taking the configuration and returning whether it
// was refused, rather than the engine's own signature, so this file names no
// engine type. What adopts a configuration and what stores one are then free to
// be built and changed independently.
type flowSettingsFunc func(pipeline.Settings) error

// ApplyFlowSettings hands the configuration to the engine.
func (apply flowSettingsFunc) ApplyFlowSettings(settings pipeline.Settings) error {
	return apply(settings)
}

// ReadFlowSettings reports every choice the run flow leaves open.
//
// The description comes from the engine's own declaration rather than from a
// copy, so a bound the engine would refuse can never be offered here. The
// values are the stored layer's, or the engine's defaults where the layer says
// nothing.
func (application settingsApplication) ReadFlowSettings(
	ctx context.Context,
) (transport.FlowSettingsRecord, error) {
	settings, revision, err := application.storedFlowSettings(ctx)
	if err != nil {
		return transport.FlowSettingsRecord{}, err
	}
	described, unrenderable, err := describeFlowSettings(settings)
	if err != nil {
		return transport.FlowSettingsRecord{}, err
	}
	return transport.FlowSettingsRecord{
		Settings: described, Revision: revision, Unrenderable: unrenderable,
	}, nil
}

// WriteFlowSettings records new values and hands them to the running engine.
//
// A value the engine would refuse is refused here rather than clamped, and
// nothing is stored until the whole configuration validates: a partially
// applied change would leave a run doing something nobody asked for.
func (application settingsApplication) WriteFlowSettings(
	ctx context.Context,
	command transport.WriteFlowSettings,
) (transport.FlowSettingsRecord, error) {
	current, revision, err := application.storedFlowSettings(ctx)
	if err != nil {
		return transport.FlowSettingsRecord{}, err
	}
	if command.ExpectedRevision != nil && *command.ExpectedRevision != revision {
		return transport.FlowSettingsRecord{}, transport.ErrSettingsRevisionConflict
	}
	updated, err := applyFlowSettingChanges(current, command.Changes)
	if err != nil {
		return transport.FlowSettingsRecord{}, err
	}
	if err := updated.Validate(); err != nil {
		// The engine's own words are returned. They name the setting and the
		// bound, which is what somebody who typed the value needs to read.
		return transport.FlowSettingsRecord{}, &transport.RequestValidationError{
			Field: "changes", Reason: err.Error(),
		}
	}
	stored, err := application.persistFlowSettings(ctx, updated, command.IdempotencyKey)
	if err != nil {
		return transport.FlowSettingsRecord{}, err
	}
	// The engine adopts the change now. Storing a configuration the running
	// coordinator does not use would tell somebody their next run had changed
	// when it had not.
	if application.flow != nil {
		if err := application.flow.ApplyFlowSettings(updated); err != nil {
			return transport.FlowSettingsRecord{}, err
		}
	}
	described, unrenderable, err := describeFlowSettings(updated)
	if err != nil {
		return transport.FlowSettingsRecord{}, err
	}
	return transport.FlowSettingsRecord{
		Settings: described, Revision: stored, Unrenderable: unrenderable,
	}, nil
}

// storedFlowSettings resolves the flow configuration in force.
func (application settingsApplication) storedFlowSettings(
	ctx context.Context,
) (pipeline.Settings, uint64, error) {
	settings := pipeline.DefaultSettings()
	stored, err := application.repositories.GetLatestSettingsRevisionForScope(
		ctx, storage.SettingsScopeUser, nil,
	)
	if errors.Is(err, storage.ErrNotFound) {
		return settings, 0, nil
	}
	if err != nil {
		return pipeline.Settings{}, 0, err
	}
	document := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(stored.ConfigurationJSON), &document); err != nil {
		return pipeline.Settings{}, 0, err
	}
	raw, present := document[flowSettingsDocumentKey]
	if !present {
		// The layer exists and says nothing about the flow, which is not the
		// same as saying the defaults: it is the same answer, reached honestly.
		return settings, stored.Revision, nil
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return pipeline.Settings{}, 0, err
	}
	return settings, stored.Revision, nil
}

// persistFlowSettings writes a new user settings revision carrying the flow.
//
// The rest of the layer is carried forward rather than rewritten, because the
// same document holds the non-secret configuration and a write that dropped it
// would silently revert settings nobody touched.
func (application settingsApplication) persistFlowSettings(
	ctx context.Context,
	settings pipeline.Settings,
	idempotencyKey string,
) (uint64, error) {
	document := map[string]json.RawMessage{}
	stored, err := application.repositories.GetLatestSettingsRevisionForScope(
		ctx, storage.SettingsScopeUser, nil,
	)
	switch {
	case errors.Is(err, storage.ErrNotFound):
	case err != nil:
		return 0, err
	default:
		if err := json.Unmarshal([]byte(stored.ConfigurationJSON), &document); err != nil {
			return 0, err
		}
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return 0, err
	}
	document[flowSettingsDocumentKey] = encoded
	body, err := json.Marshal(document)
	if err != nil {
		return 0, err
	}
	written, err := application.repositories.CreateSettingsRevision(
		ctx,
		storage.CreateSettingsRevision{
			Scope:             storage.SettingsScopeUser,
			ConfigurationJSON: string(body),
			IdempotencyKey:    idempotencyKey,
		},
	)
	if err != nil {
		return 0, err
	}
	return written.Revision, nil
}

// applyFlowSettingChanges folds one command's changes into a configuration.
//
// A key the engine does not declare is refused rather than ignored: a client
// that sent it believes it changed something.
func applyFlowSettingChanges(
	settings pipeline.Settings,
	changes []transport.FlowSettingChange,
) (pipeline.Settings, error) {
	declared := map[string]pipeline.Toggle{}
	for _, toggle := range pipeline.Describe() {
		declared[toggle.Key] = toggle
	}
	for _, change := range changes {
		toggle, present := declared[change.Key]
		if !present {
			return pipeline.Settings{}, &transport.RequestValidationError{
				Field:  "changes.key",
				Reason: fmt.Sprintf("%q is not a setting this coordinator declares", change.Key),
			}
		}
		if err := setFlowValue(&settings, toggle, change); err != nil {
			return pipeline.Settings{}, err
		}
	}
	return settings, nil
}

// describeFlowSettings pairs the engine's declaration with the values in force.
func describeFlowSettings(
	settings pipeline.Settings,
) ([]transport.FlowSetting, []string, error) {
	// The engine's own defaults are the comparison, read the same way the
	// values are. Naming them separately here would be a second copy that
	// eventually disagrees with the one the engine ships.
	defaults := pipeline.DefaultSettings()
	unrenderable := []string{}
	described := make([]transport.FlowSetting, 0, len(pipeline.Describe()))
	for _, toggle := range pipeline.Describe() {
		setting := transport.FlowSetting{
			Key: toggle.Key, Label: toggle.Label, Help: toggle.Help,
			Kind:    string(toggle.Kind),
			Choices: toggle.Choices,
			Minimum: int32(toggle.Minimum), Maximum: int32(toggle.Maximum),
			Group: toggle.Group,
		}
		if err := readFlowValue(settings, toggle, &setting); err != nil {
			// The engine gains settings over time, and one it declares in a
			// shape this boundary cannot carry is a reason to leave that row
			// out — not to blank the page every other setting is read from.
			// The write path still refuses it, so nothing is set blindly.
			unrenderable = append(unrenderable, toggle.Key)
			continue
		}
		shipped := transport.FlowSetting{}
		if err := readFlowValue(defaults, toggle, &shipped); err != nil {
			unrenderable = append(unrenderable, toggle.Key)
			continue
		}
		// Every carried shape, not only the scalar ones. Comparing three of
		// five fields reported a reordered model ladder and a changed set of
		// per-stage models as shipped defaults, which is the one thing this
		// flag exists to deny: a page showing where this machine has departed
		// from the defaults would have shown no departure.
		setting.AtDefault = setting.Text == shipped.Text &&
			setting.Number == shipped.Number &&
			setting.Enabled == shipped.Enabled &&
			sameItems(setting.Items, shipped.Items) &&
			samePairs(setting.Pairs, shipped.Pairs)
		described = append(described, setting)
	}
	return described, unrenderable, nil
}

// sameItems compares two carried lists in order.
//
// Order matters even for a set: both sides are produced by the same reader
// from the same declaration, so a difference in order is a difference in
// value, not in rendering.
func sameItems(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// samePairs compares two carried mappings, which the reader emits sorted.
func samePairs(left []transport.FlowSettingPair, right []transport.FlowSettingPair) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// flowSettingField resolves the struct field one declared key names.
//
// The engine declares its settings and this reads them by convention — a
// snake_case key names the PascalCase field — rather than by a list kept here.
// A list would have to be edited every time the engine gained a setting, and
// the failure when somebody forgot is the worst kind: the new control renders,
// shows a zero, and silently drops what a person typed into it. A key that
// resolves to no field is reported instead.
func flowSettingField(
	settings *pipeline.Settings,
	key string,
) (reflect.Value, error) {
	field := reflect.ValueOf(settings).Elem().FieldByName(pascalCaseKey(key))
	if !field.IsValid() || !field.CanSet() {
		return reflect.Value{}, fmt.Errorf(
			"the engine declares setting %q but nothing in pipeline.Settings is named %q",
			key, pascalCaseKey(key),
		)
	}
	return field, nil
}

// pascalCaseKey renders a declared key as the field name it corresponds to.
func pascalCaseKey(key string) string {
	parts := strings.Split(key, "_")
	name := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		name += strings.ToUpper(part[:1]) + part[1:]
	}
	return name
}

// setFlowValue writes one declared value into the configuration.
func setFlowValue(
	settings *pipeline.Settings,
	toggle pipeline.Toggle,
	change transport.FlowSettingChange,
) error {
	field, err := flowSettingField(settings, toggle.Key)
	if err != nil {
		return err
	}
	switch toggle.Kind {
	case pipeline.ToggleChoice:
		if field.Kind() != reflect.String {
			return flowKindMismatch(toggle, field)
		}
		field.SetString(change.Text)
	case pipeline.ToggleNumber:
		if field.Kind() != reflect.Int {
			return flowKindMismatch(toggle, field)
		}
		field.SetInt(int64(change.Number))
	case pipeline.ToggleSwitch:
		if field.Kind() != reflect.Bool {
			return flowKindMismatch(toggle, field)
		}
		field.SetBool(change.Enabled)
	case pipeline.ToggleSet, pipeline.ToggleSequence:
		if field.Kind() != reflect.Slice ||
			field.Type().Elem().Kind() != reflect.String {
			return flowKindMismatch(toggle, field)
		}
		// A nil rather than an empty slice when nothing was sent, so "no
		// restrictions" and "the empty list" store as the same thing they read
		// back as. Storing one and reading the other makes a setting look
		// changed every time the page is opened.
		if len(change.Items) == 0 {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		items := reflect.MakeSlice(field.Type(), 0, len(change.Items))
		for _, item := range change.Items {
			items = reflect.Append(items, reflect.ValueOf(item))
		}
		field.Set(items)
	case pipeline.ToggleMapping:
		if field.Kind() != reflect.Map ||
			field.Type().Key().Kind() != reflect.String ||
			field.Type().Elem().Kind() != reflect.String {
			return flowKindMismatch(toggle, field)
		}
		if len(change.Pairs) == 0 {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		mapping := reflect.MakeMapWithSize(field.Type(), len(change.Pairs))
		for _, pair := range change.Pairs {
			mapping.SetMapIndex(
				reflect.ValueOf(pair.Key), reflect.ValueOf(pair.Value))
		}
		field.Set(mapping)
	default:
		// Refused rather than ignored. Without this, a setting declared in a
		// kind this boundary has not learned yet accepted a write, discarded
		// it, and reported success — so a person would set a value, be told it
		// was saved, and find it unchanged with nothing anywhere saying why.
		return fmt.Errorf(
			"setting %q is declared %q, which this boundary cannot write",
			toggle.Key, toggle.Kind)
	}
	return nil
}

// readFlowValue reads one declared value out of the configuration.
func readFlowValue(
	settings pipeline.Settings,
	toggle pipeline.Toggle,
	setting *transport.FlowSetting,
) error {
	field, err := flowSettingField(&settings, toggle.Key)
	if err != nil {
		return err
	}
	switch toggle.Kind {
	case pipeline.ToggleChoice:
		if field.Kind() != reflect.String {
			return flowKindMismatch(toggle, field)
		}
		setting.Text = field.String()
	case pipeline.ToggleNumber:
		if field.Kind() != reflect.Int {
			return flowKindMismatch(toggle, field)
		}
		setting.Number = int32(field.Int())
	case pipeline.ToggleSwitch:
		if field.Kind() != reflect.Bool {
			return flowKindMismatch(toggle, field)
		}
		setting.Enabled = field.Bool()
	case pipeline.ToggleSet, pipeline.ToggleSequence:
		if field.Kind() != reflect.Slice ||
			field.Type().Elem().Kind() != reflect.String {
			return flowKindMismatch(toggle, field)
		}
		// In stored order for a sequence, because the order is what the setting
		// means: a ladder rendered alphabetically would show a person a
		// different escalation path from the one their runs take.
		for index := 0; index < field.Len(); index++ {
			setting.Items = append(setting.Items, field.Index(index).String())
		}
	case pipeline.ToggleMapping:
		if field.Kind() != reflect.Map ||
			field.Type().Key().Kind() != reflect.String ||
			field.Type().Elem().Kind() != reflect.String {
			return flowKindMismatch(toggle, field)
		}
		keys := make([]string, 0, field.Len())
		for _, key := range field.MapKeys() {
			keys = append(keys, key.String())
		}
		// Sorted, because a map ranges differently every time and an unchanged
		// configuration would read as changed on every comparison.
		sort.Strings(keys)
		for _, key := range keys {
			setting.Pairs = append(setting.Pairs, transport.FlowSettingPair{
				Key:   key,
				Value: field.MapIndex(reflect.ValueOf(key)).String(),
			})
		}
	default:
		// Reported as unrenderable rather than sent as an empty row. A blank
		// control for a setting that governs runs is worse than an absent one:
		// it invites somebody to set it and reads back as though they had.
		return fmt.Errorf(
			"setting %q is declared %q, which this boundary cannot read",
			toggle.Key, toggle.Kind)
	}
	return nil
}

// flowKindMismatch reports a setting whose declared kind and stored type
// disagree, which no value can be read or written through honestly.
func flowKindMismatch(toggle pipeline.Toggle, field reflect.Value) error {
	return fmt.Errorf(
		"setting %q is declared %q but is stored as %s",
		toggle.Key, toggle.Kind, field.Kind(),
	)
}
