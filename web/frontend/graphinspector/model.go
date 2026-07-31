// Package graphinspector renders one bounded, read-only graph node explanation
// and dispatches typed exploration commands without owning graph or transport state.
package graphinspector

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/primitives"
)

const (
	MaximumEvidenceItems                     = 32
	MaximumRelatedMessages                   = 32
	MaximumSourceLocations                   = 32
	MaximumProviderTokenCategories           = 16
	MaximumProviderTokenCategoryBytes        = 64
	MaximumInspectorLabelBytes               = 256
	MaximumInspectorTextBytes                = 1024
	MaximumSourceCoordinate           uint64 = 1<<32 - 1
)

var ErrInvalidInspectorProps = errors.New("invalid graph inspector props")

type EvidenceItem struct {
	ID      domain.EvidenceID
	Label   string
	State   domain.ValidationState
	Summary string
}

type EvidenceView struct {
	Known           bool
	UnknownReason   string
	GuaranteeKnown  bool
	Guarantee       domain.AssuranceLevel
	GuaranteeReason string
	SupportingItems []EvidenceItem
}

type DurationAttribution struct {
	Known         bool
	Value         time.Duration
	UnknownReason string
}

type TokenAttribution struct {
	Usage         domain.TokenUsage
	UnknownReason string
}

type CostAttribution struct {
	Known           bool
	Value           domain.Money
	PricingSnapshot string
	UnknownReason   string
}

// SourceLocation is a constructor-validated repository-relative source span.
// Its private fields prevent an unvalidated path from reaching editor dispatch.
type SourceLocation struct {
	repositoryID       domain.RepositoryID
	repositoryRevision string
	relativePath       string
	startLine          uint64
	startColumn        uint64
	endLine            uint64
	endColumn          uint64
}

func NewSourceLocation(
	repositoryID domain.RepositoryID,
	repositoryRevision string,
	relativePath string,
	startLine, startColumn, endLine, endColumn uint64,
) (SourceLocation, error) {
	if repositoryID.IsZero() {
		return SourceLocation{}, inspectorError("source.repository_id", "must not be empty")
	}
	if !validRepositoryRevision(repositoryRevision) {
		return SourceLocation{}, inspectorError("source.repository_revision", "must be a 40- or 64-character lowercase hexadecimal revision")
	}
	if !validRepositoryRelativePath(relativePath) {
		return SourceLocation{}, inspectorError("source.relative_path", "must be a bounded canonical repository-relative path")
	}
	if startLine == 0 || startColumn == 0 || endLine < startLine || endColumn == 0 ||
		startLine > MaximumSourceCoordinate || startColumn > MaximumSourceCoordinate ||
		endLine > MaximumSourceCoordinate || endColumn > MaximumSourceCoordinate ||
		(endLine == startLine && endColumn < startColumn) {
		return SourceLocation{}, inspectorError("source.span", "must be a positive ordered source span")
	}
	return SourceLocation{
		repositoryID: repositoryID, repositoryRevision: repositoryRevision,
		relativePath: relativePath, startLine: startLine, startColumn: startColumn,
		endLine: endLine, endColumn: endColumn,
	}, nil
}

func (location SourceLocation) RepositoryID() domain.RepositoryID { return location.repositoryID }
func (location SourceLocation) RepositoryRevision() string        { return location.repositoryRevision }
func (location SourceLocation) RelativePath() string              { return location.relativePath }
func (location SourceLocation) StartLine() uint64                 { return location.startLine }
func (location SourceLocation) StartColumn() uint64               { return location.startColumn }
func (location SourceLocation) EndLine() uint64                   { return location.endLine }
func (location SourceLocation) EndColumn() uint64                 { return location.endColumn }

func (location SourceLocation) Validate() error {
	_, err := NewSourceLocation(
		location.repositoryID, location.repositoryRevision, location.relativePath,
		location.startLine, location.startColumn, location.endLine, location.endColumn,
	)
	return err
}

type ActionAvailability struct {
	Enabled        bool
	Busy           bool
	DisabledReason string
}

func (availability ActionAvailability) validate(name string) error {
	reason := strings.TrimSpace(availability.DisabledReason)
	switch {
	case availability.Enabled && reason != "":
		return inspectorError("actions."+name, "cannot be enabled with a disabled reason")
	case !availability.Enabled && reason == "":
		return inspectorError("actions."+name, "requires an explicit disabled reason")
	}
	return nil
}

type Actions struct {
	ExplainInChat   ActionAvailability
	ExpandNeighbors ActionAvailability
	DependencyCone  ActionAvailability
	EvidenceCone    ActionAvailability
	CompareRevision ActionAvailability
	OpenInEditor    ActionAvailability
}

type ActionKind string

const (
	ActionExplainInChat   ActionKind = "explain-in-chat"
	ActionExpandNeighbors ActionKind = "expand-neighbors"
	ActionDependencyCone  ActionKind = "dependency-cone"
	ActionEvidenceCone    ActionKind = "evidence-cone"
	ActionCompareRevision ActionKind = "compare-revision"
)

type Props struct {
	Mode            primitives.Mode
	RevisionID      domain.GraphRevisionID
	Node            taskgraph.Node
	Evidence        EvidenceView
	Duration        DurationAttribution
	Tokens          TokenAttribution
	Cost            CostAttribution
	RelatedMessages []domain.MessageID
	SourceLocations []SourceLocation
	Actions         Actions

	OnExplainInChat         func(domain.NodeID)
	OnExpandNeighbors       func(domain.NodeID)
	OnIsolateDependencyCone func(domain.NodeID)
	OnIsolateEvidenceCone   func(domain.NodeID)
	OnCompareRevision       func(domain.GraphRevisionID)
	OnOpenInEditor          func(validatedPath string, line uint32)
}

func (props Props) Validate() error {
	if props.RevisionID.IsZero() {
		return inspectorError("revision_id", "must not be empty")
	}
	if err := props.Node.Validate(); err != nil {
		return fmt.Errorf("%w: node: %v", ErrInvalidInspectorProps, err)
	}
	if err := validateEvidence(props.Evidence); err != nil {
		return err
	}
	if err := validateDuration(props.Duration); err != nil {
		return err
	}
	if err := validateTokens(props.Tokens); err != nil {
		return err
	}
	if err := validateCost(props.Cost); err != nil {
		return err
	}
	if len(props.RelatedMessages) > MaximumRelatedMessages {
		return inspectorError("related_messages", "exceeds the bounded list limit")
	}
	seenMessages := make(map[domain.MessageID]struct{}, len(props.RelatedMessages))
	for _, messageID := range props.RelatedMessages {
		if messageID.IsZero() {
			return inspectorError("related_messages", "contains an empty identity")
		}
		if _, duplicate := seenMessages[messageID]; duplicate {
			return inspectorError("related_messages", "contains a duplicate identity")
		}
		seenMessages[messageID] = struct{}{}
	}
	if len(props.SourceLocations) > MaximumSourceLocations {
		return inspectorError("source_locations", "exceeds the bounded list limit")
	}
	seenLocations := make(map[string]struct{}, len(props.SourceLocations))
	for _, location := range props.SourceLocations {
		if err := location.Validate(); err != nil {
			return err
		}
		key := sourceLocationKey(location)
		if _, duplicate := seenLocations[key]; duplicate {
			return inspectorError("source_locations", "contains a duplicate source span")
		}
		seenLocations[key] = struct{}{}
	}
	for _, action := range []struct {
		name         string
		availability ActionAvailability
	}{
		{"explain_in_chat", props.Actions.ExplainInChat},
		{"expand_neighbors", props.Actions.ExpandNeighbors},
		{"dependency_cone", props.Actions.DependencyCone},
		{"evidence_cone", props.Actions.EvidenceCone},
		{"compare_revision", props.Actions.CompareRevision},
		{"open_in_editor", props.Actions.OpenInEditor},
	} {
		if err := action.availability.validate(action.name); err != nil {
			return err
		}
	}
	if err := validateEnabledCallbacks(props); err != nil {
		return err
	}
	return nil
}

func (props Props) RelatedMessagesClone() []domain.MessageID {
	return slices.Clone(props.RelatedMessages)
}

func (props Props) SourceLocationsClone() []SourceLocation {
	return slices.Clone(props.SourceLocations)
}

// ActivateAction dispatches one enabled, non-busy node action. It is shared by
// browser clicks and native tests so disabled-state behavior cannot drift.
func ActivateAction(props Props, kind ActionKind) bool {
	if props.Validate() != nil {
		return false
	}
	nodeID := props.Node.ID()
	switch kind {
	case ActionExplainInChat:
		if actionReady(props.Actions.ExplainInChat) {
			props.OnExplainInChat(nodeID)
			return true
		}
	case ActionExpandNeighbors:
		if actionReady(props.Actions.ExpandNeighbors) {
			props.OnExpandNeighbors(nodeID)
			return true
		}
	case ActionDependencyCone:
		if actionReady(props.Actions.DependencyCone) {
			props.OnIsolateDependencyCone(nodeID)
			return true
		}
	case ActionEvidenceCone:
		if actionReady(props.Actions.EvidenceCone) {
			props.OnIsolateEvidenceCone(nodeID)
			return true
		}
	case ActionCompareRevision:
		if actionReady(props.Actions.CompareRevision) {
			props.OnCompareRevision(props.RevisionID)
			return true
		}
	}
	return false
}

// ActivateSource opens only a constructor-validated source location while the
// editor action is enabled and non-busy.
func ActivateSource(props Props, index int) bool {
	if props.Validate() != nil || !actionReady(props.Actions.OpenInEditor) ||
		index < 0 || index >= len(props.SourceLocations) {
		return false
	}
	location := props.SourceLocations[index]
	props.OnOpenInEditor(location.RelativePath(), uint32(location.StartLine()))
	return true
}

func actionReady(availability ActionAvailability) bool {
	return availability.Enabled && !availability.Busy
}

func validateEvidence(evidence EvidenceView) error {
	if !evidence.Known {
		if evidence.GuaranteeKnown || evidence.Guarantee != "" || evidence.GuaranteeReason != "" || len(evidence.SupportingItems) != 0 {
			return inspectorError("evidence", "unknown evidence must not carry guarantee or supporting items")
		}
		return validateOptionalText("evidence.unknown_reason", evidence.UnknownReason, MaximumInspectorTextBytes)
	}
	if evidence.UnknownReason != "" {
		return inspectorError("evidence.unknown_reason", "must be empty when evidence is known")
	}
	if evidence.GuaranteeKnown {
		if !evidence.Guarantee.IsValid() {
			return inspectorError("evidence.guarantee", "is not a declared assurance level")
		}
	} else if evidence.Guarantee != "" {
		return inspectorError("evidence.guarantee", "unknown guarantee must not carry a level")
	}
	if err := validateOptionalText("evidence.guarantee_reason", evidence.GuaranteeReason, MaximumInspectorTextBytes); err != nil {
		return err
	}
	if len(evidence.SupportingItems) > MaximumEvidenceItems {
		return inspectorError("evidence.items", "exceeds the bounded list limit")
	}
	seen := make(map[domain.EvidenceID]struct{}, len(evidence.SupportingItems))
	for _, item := range evidence.SupportingItems {
		if item.ID.IsZero() || !item.State.IsValid() {
			return inspectorError("evidence.items", "contains an incomplete typed item")
		}
		if err := validateRequiredText("evidence.item.label", item.Label, MaximumInspectorLabelBytes); err != nil {
			return err
		}
		if err := validateOptionalText("evidence.item.summary", item.Summary, MaximumInspectorTextBytes); err != nil {
			return err
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return inspectorError("evidence.items", "contains a duplicate identity")
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

func validateDuration(value DurationAttribution) error {
	if value.Known && value.Value < 0 {
		return inspectorError("duration", "known duration must not be negative")
	}
	if !value.Known && value.Value != 0 {
		return inspectorError("duration", "unknown duration must not carry a value")
	}
	if value.Known && value.UnknownReason != "" {
		return inspectorError("duration.unknown_reason", "must be empty when duration is known")
	}
	return validateOptionalText("duration.unknown_reason", value.UnknownReason, MaximumInspectorTextBytes)
}

func validateTokens(value TokenAttribution) error {
	if err := value.Usage.Validate(); err != nil {
		return fmt.Errorf("%w: tokens: %v", ErrInvalidInspectorProps, err)
	}
	if value.Usage.Known && value.UnknownReason != "" {
		return inspectorError("tokens.unknown_reason", "must be empty when usage is known")
	}
	if len(value.Usage.ProviderSpecific) > MaximumProviderTokenCategories {
		return inspectorError("tokens.provider_specific", "exceeds the bounded category limit")
	}
	for category := range value.Usage.ProviderSpecific {
		if err := validateRequiredText("tokens.provider_specific.category", category, MaximumProviderTokenCategoryBytes); err != nil {
			return err
		}
	}
	return validateOptionalText("tokens.unknown_reason", value.UnknownReason, MaximumInspectorTextBytes)
}

func validateCost(value CostAttribution) error {
	if !value.Known {
		if value.Value != (domain.Money{}) || value.PricingSnapshot != "" {
			return inspectorError("cost", "unknown cost must not carry money or a pricing snapshot")
		}
		return validateOptionalText("cost.unknown_reason", value.UnknownReason, MaximumInspectorTextBytes)
	}
	if value.UnknownReason != "" {
		return inspectorError("cost.unknown_reason", "must be empty when cost is known")
	}
	if err := value.Value.Validate(); err != nil || value.Value.MinorUnits < 0 {
		return inspectorError("cost", "known cost must be exact non-negative money")
	}
	if err := validateRequiredText("cost.pricing_snapshot", value.PricingSnapshot, MaximumInspectorLabelBytes); err != nil {
		return err
	}
	return nil
}

func validateEnabledCallbacks(props Props) error {
	checks := []struct {
		name         string
		availability ActionAvailability
		callback     bool
	}{
		{"explain_in_chat", props.Actions.ExplainInChat, props.OnExplainInChat != nil},
		{"expand_neighbors", props.Actions.ExpandNeighbors, props.OnExpandNeighbors != nil},
		{"dependency_cone", props.Actions.DependencyCone, props.OnIsolateDependencyCone != nil},
		{"evidence_cone", props.Actions.EvidenceCone, props.OnIsolateEvidenceCone != nil},
		{"compare_revision", props.Actions.CompareRevision, props.OnCompareRevision != nil},
		{"open_in_editor", props.Actions.OpenInEditor, props.OnOpenInEditor != nil && len(props.SourceLocations) > 0},
	}
	for _, check := range checks {
		if check.availability.Enabled && !check.callback {
			return inspectorError("actions."+check.name, "is enabled without an executable callback or validated target")
		}
	}
	return nil
}

func validateRequiredText(field, value string, maximum int) error {
	if strings.TrimSpace(value) == "" {
		return inspectorError(field, "must not be empty")
	}
	return validateText(field, value, maximum)
}

func validateOptionalText(field, value string, maximum int) error {
	if value == "" {
		return nil
	}
	return validateText(field, value, maximum)
}

func validateText(field, value string, maximum int) error {
	if strings.TrimSpace(value) != value || !utf8.ValidString(value) || len(value) > maximum {
		return inspectorError(field, "must be trimmed, valid UTF-8, and bounded")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return inspectorError(field, "must not contain control characters")
		}
	}
	return nil
}

func validRepositoryRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func validRepositoryRelativePath(value string) bool {
	if value == "" || len(value) > 4096 || strings.TrimSpace(value) != value ||
		strings.HasPrefix(value, "/") || strings.Contains(value, "\\") ||
		strings.Contains(value, ":") ||
		strings.ContainsRune(value, '\x00') || path.Clean(value) != value ||
		value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	return true
}

func sourceLocationKey(location SourceLocation) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d",
		location.RepositoryID(), location.RepositoryRevision(), location.RelativePath(),
		location.StartLine(), location.StartColumn(), location.EndLine(), location.EndColumn())
}

func inspectorError(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidInspectorProps, field, reason)
}
