package memoryinspector

import (
	"strconv"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Component renders the project-memory Inspection and Correction UI
// (M21-090..099): the filtered, virtualized artifact list; the selected
// artifact's lineage, bindings, and retrieval history; its correction,
// quarantine, invalidation, and deletion controls; and the export batch. It
// never talks to transport — every mutation is a typed callback validated
// against internal/domain before it ever fires.
func Component(props Props) ui.Node {
	translator := translatorOrEnglish(props.Translator)
	if err := props.Validate(); err != nil {
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title:   translator.MustText(frontendi18n.MsgMemoryInspectorUnavailable),
			Message: err.Error(), Tone: design.StatusFailure, Mode: props.Mode,
		})
	}
	tokens := props.Mode.Tokens()
	return html.Section(html.Props{
		Aria:  map[string]string{"label": translator.MustText(frontendi18n.MsgMemoryListAriaLabel)},
		Data:  map[string]string{"component": "project-memory-inspector"},
		Class: rootClass(tokens),
	},
		listSection(props, translator),
		selectedSection(props, translator),
		exportSection(props, translator),
	)
}

func translatorOrEnglish(translator frontendi18n.Translator) frontendi18n.Translator {
	if translator.Selection().Resolved != "" {
		return translator
	}
	return frontendi18n.EnglishRegistry().Resolve(string(frontendi18n.LocaleEnglishUnitedStates))
}

// -----------------------------------------------------------------------
// Kind, maturity, and status presentation
// -----------------------------------------------------------------------

// designKindForArtifactKind assigns each of the eight domain memory-artifact
// kinds a distinct design.Kind color family, one-to-one, so every kind is
// visually distinguishable without relying on the (also present) text label.
func designKindForArtifactKind(kind domain.MemoryArtifactKind) design.Kind {
	switch kind {
	case domain.MemoryArtifactKindRepositoryFact:
		return design.KindMemory
	case domain.MemoryArtifactKindReviewedCommand:
		return design.KindExecution
	case domain.MemoryArtifactKindFileToTestMapping:
		return design.KindTest
	case domain.MemoryArtifactKindRepositoryConvention:
		return design.KindCode
	case domain.MemoryArtifactKindAcceptedRegressionCase:
		return design.KindValidation
	case domain.MemoryArtifactKindExecutionRecipe:
		return design.KindPlan
	case domain.MemoryArtifactKindExecutableAtomReference:
		return design.KindEvidence
	case domain.MemoryArtifactKindObservationHypothesis:
		return design.KindForecast
	default:
		return design.KindMemory
	}
}

func kindMessageID(kind domain.MemoryArtifactKind) frontendi18n.MessageID {
	switch kind {
	case domain.MemoryArtifactKindRepositoryFact:
		return frontendi18n.MsgMemoryKindRepositoryFact
	case domain.MemoryArtifactKindReviewedCommand:
		return frontendi18n.MsgMemoryKindReviewedCommand
	case domain.MemoryArtifactKindFileToTestMapping:
		return frontendi18n.MsgMemoryKindFileToTestMapping
	case domain.MemoryArtifactKindRepositoryConvention:
		return frontendi18n.MsgMemoryKindRepositoryConvention
	case domain.MemoryArtifactKindAcceptedRegressionCase:
		return frontendi18n.MsgMemoryKindAcceptedRegressionCase
	case domain.MemoryArtifactKindExecutionRecipe:
		return frontendi18n.MsgMemoryKindExecutionRecipe
	case domain.MemoryArtifactKindExecutableAtomReference:
		return frontendi18n.MsgMemoryKindExecutableAtomReference
	case domain.MemoryArtifactKindObservationHypothesis:
		return frontendi18n.MsgMemoryKindObservationHypothesis
	default:
		return frontendi18n.MsgCommonUnknown
	}
}

func maturityMessageID(maturity domain.MaturityState) frontendi18n.MessageID {
	switch maturity {
	case domain.MaturityStateCandidate:
		return frontendi18n.MsgMemoryMaturityCandidate
	case domain.MaturityStateValidated:
		return frontendi18n.MsgMemoryMaturityValidated
	case domain.MaturityStatePreferredForExperiment:
		return frontendi18n.MsgMemoryMaturityPreferredForExperiment
	case domain.MaturityStateQuarantined:
		return frontendi18n.MsgMemoryMaturityQuarantined
	case domain.MaturityStateInvalidated:
		return frontendi18n.MsgMemoryMaturityInvalidated
	case domain.MaturityStateRetired:
		return frontendi18n.MsgMemoryMaturityRetired
	default:
		return frontendi18n.MsgCommonUnknown
	}
}

func maturityStatusTone(maturity domain.MaturityState) design.Status {
	switch maturity {
	case domain.MaturityStateCandidate:
		return design.StatusPending
	case domain.MaturityStateValidated:
		return design.StatusSuccess
	case domain.MaturityStatePreferredForExperiment:
		return design.StatusAccent
	case domain.MaturityStateQuarantined:
		return design.StatusBlocked
	case domain.MaturityStateInvalidated:
		return design.StatusFailure
	case domain.MaturityStateRetired:
		return design.StatusNeutral
	default:
		return design.StatusNeutral
	}
}

func correctionFieldMessageID(kind domain.MemoryArtifactKind) frontendi18n.MessageID {
	switch kind {
	case domain.MemoryArtifactKindReviewedCommand:
		return frontendi18n.MsgMemoryCorrectionFieldCommandLines
	case domain.MemoryArtifactKindFileToTestMapping:
		return frontendi18n.MsgMemoryCorrectionFieldTestPathsLines
	case domain.MemoryArtifactKindAcceptedRegressionCase:
		return frontendi18n.MsgMemoryCorrectionFieldExpectedOutcome
	case domain.MemoryArtifactKindExecutionRecipe:
		return frontendi18n.MsgMemoryCorrectionFieldPlanSummary
	case domain.MemoryArtifactKindExecutableAtomReference:
		return frontendi18n.MsgMemoryCorrectionFieldCanonicalName
	default:
		return frontendi18n.MsgMemoryCorrectionFieldStatement
	}
}

// -----------------------------------------------------------------------
// List section (M21-090, M21-091)
// -----------------------------------------------------------------------

var allMemoryArtifactKinds = []domain.MemoryArtifactKind{
	domain.MemoryArtifactKindRepositoryFact,
	domain.MemoryArtifactKindReviewedCommand,
	domain.MemoryArtifactKindFileToTestMapping,
	domain.MemoryArtifactKindRepositoryConvention,
	domain.MemoryArtifactKindAcceptedRegressionCase,
	domain.MemoryArtifactKindExecutionRecipe,
	domain.MemoryArtifactKindExecutableAtomReference,
	domain.MemoryArtifactKindObservationHypothesis,
}

func listSection(props Props, translator frontendi18n.Translator) ui.Node {
	tokens := props.Mode.Tokens()
	filtered := ApplyListFilter(props.List.Entries, props.List.Filter, time.Now())

	height := props.List.Height
	if height <= 0 {
		height = DefaultListHeight
	}
	rowHeight := props.List.RowHeight
	if rowHeight <= 0 {
		rowHeight = DefaultListRowHeight
	}
	state := props.List.State
	if state == "" {
		state = primitives.VirtualListReady
	}

	list := primitives.VirtualList(primitives.VirtualListProps[ListEntry]{
		ID:        "project-memory-list",
		Label:     translator.MustText(frontendi18n.MsgMemoryListAriaLabel),
		Items:     filtered,
		State:     state,
		Height:    height,
		RowHeight: rowHeight,
		Mode:      props.Mode,
		ActiveKey: props.List.ActiveID,
		ItemKey:   func(entry ListEntry) string { return entry.ArtifactID.String() },
		ItemLabel: func(entry ListEntry) string {
			return translator.MustText(kindMessageID(entry.Kind)) + ": " + entry.Summary
		},
		RenderItem:        listRowRenderer(props.Mode, translator),
		OnActiveChange:    func(key string) { dispatchSelect(props.List.OnSelect, key) },
		OnActivate:        func(key string) { dispatchSelect(props.List.OnSelect, key) },
		EmptyTitle:        translator.MustText(frontendi18n.MsgMemoryListEmptyTitle),
		EmptyBody:         translator.MustText(frontendi18n.MsgMemoryListEmptyBody),
		LoadingLabel:      translator.MustText(frontendi18n.MsgMemoryListLoading),
		ErrorTitle:        translator.MustText(frontendi18n.MsgMemoryListErrorTitle),
		ErrorBody:         translator.MustText(frontendi18n.MsgMemoryListErrorBody),
		DisconnectedTitle: translator.MustText(frontendi18n.MsgMemoryListDisconnectedTitle),
		DisconnectedBody:  translator.MustText(frontendi18n.MsgMemoryListDisconnectedBody),
		RetryLabel:        retryLabelIfAvailable(translator, props.List.OnRetry),
		OnRetry:           props.List.OnRetry,
	})

	return html.Section(html.Props{
		Aria:  map[string]string{"label": translator.MustText(frontendi18n.MsgMemoryListTitleLabel)},
		Data:  map[string]string{"component": "project-memory-list-section", "result-count": strconv.Itoa(len(filtered))},
		Class: sectionClass(tokens),
	},
		html.H2(html.Props{Class: sectionHeadingClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryListTitleLabel)}),
		filterControls(props.List, props.Mode, translator),
		list,
	)
}

func retryLabelIfAvailable(translator frontendi18n.Translator, retry func()) string {
	if retry == nil {
		return ""
	}
	return translator.MustText(frontendi18n.MsgMemoryListRetry)
}

func dispatchSelect(onSelect func(domain.MemoryArtifactID), key string) {
	if onSelect == nil {
		return
	}
	artifactID, err := domain.ParseMemoryArtifactID(key)
	if err != nil {
		return
	}
	onSelect(artifactID)
}

func listRowRenderer(mode primitives.Mode, translator frontendi18n.Translator) func(primitives.VirtualListItemProps[ListEntry]) ui.Node {
	tokens := mode.Tokens()
	return func(row primitives.VirtualListItemProps[ListEntry]) ui.Node {
		entry := row.Item
		return html.Div(html.Props{
			Data: map[string]string{
				"component": "project-memory-row", "artifact-id": entry.ArtifactID.String(),
				"kind": string(entry.Kind), "maturity": string(entry.Maturity),
			},
			Class: rowClass(tokens),
		},
			primitives.IconChip(primitives.IconChipProps{
				Label: translator.MustText(kindMessageID(entry.Kind)), Kind: designKindForArtifactKind(entry.Kind), Mode: mode,
			}),
			primitives.Badge(primitives.BadgeProps{
				Label: translator.MustText(maturityMessageID(entry.Maturity)), Status: maturityStatusTone(entry.Maturity), Mode: mode,
			}),
			html.Span(html.Props{Class: rowSummaryClass(tokens), Text: entry.Summary}),
			html.Span(html.Props{
				Data: map[string]string{"row-field": "last-confirmation"}, Class: rowMetaClass(tokens),
				Text: lastConfirmedText(translator, entry.LastConfirmation),
			}),
		)
	}
}

func lastConfirmedText(translator frontendi18n.Translator, attribution LastConfirmationAttribution) string {
	if !attribution.Known {
		return translator.MustText(frontendi18n.MsgMemoryListRowNeverConfirmed)
	}
	return translator.MustText(frontendi18n.MsgMemoryListRowLastConfirmed, map[string]any{
		"state": attribution.At.UTC().Format(time.RFC3339),
	})
}

func filterControls(props ListProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": translator.MustText(frontendi18n.MsgMemoryFilterKindLabel)},
		Data: map[string]string{"component": "project-memory-filters"}, Class: filterGroupClass(tokens),
	},
		kindFilterGroup(props, mode, translator),
		maturityFilterGroup(props, mode, translator),
		validityFilterTabs(props, mode, translator),
		lastConfirmationFilterTabs(props, mode, translator),
		primitives.Button(primitives.ButtonProps{
			ID: "memory-filter-clear", Label: translator.MustText(frontendi18n.MsgMemoryFilterClear), Mode: mode,
			OnClick: func() {
				if props.OnFilterChange != nil {
					props.OnFilterChange(ListFilter{})
				}
			},
		}),
	)
}

func kindFilterGroup(props ListProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	children := []ui.Node{html.H3(html.Props{Class: filterHeadingClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryFilterKindLabel)})}
	for _, kind := range allMemoryArtifactKinds {
		kind := kind
		children = append(children, primitives.ToggleButton(primitives.ToggleButtonProps{
			ID: "memory-filter-kind-" + string(kind), Label: translator.MustText(kindMessageID(kind)),
			Pressed: containsKind(props.Filter.Kinds, kind), Mode: mode,
			OnToggle: func(next bool) {
				if props.OnFilterChange == nil {
					return
				}
				updated := props.Filter
				updated.Kinds = toggleKind(updated.Kinds, kind, next)
				props.OnFilterChange(updated)
			},
		}))
	}
	return html.Div(html.Props{Data: map[string]string{"component": "memory-filter-kind"}, Class: filterRowClass(tokens)}, children...)
}

func maturityFilterGroup(props ListProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	maturities := domain.AllMaturityStates()
	children := []ui.Node{html.H3(html.Props{Class: filterHeadingClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryFilterMaturityLabel)})}
	for _, maturity := range maturities {
		maturity := maturity
		children = append(children, primitives.ToggleButton(primitives.ToggleButtonProps{
			ID: "memory-filter-maturity-" + string(maturity), Label: translator.MustText(maturityMessageID(maturity)),
			Pressed: containsMaturity(props.Filter.Maturities, maturity), Mode: mode,
			OnToggle: func(next bool) {
				if props.OnFilterChange == nil {
					return
				}
				updated := props.Filter
				updated.Maturities = toggleMaturity(updated.Maturities, maturity, next)
				props.OnFilterChange(updated)
			},
		}))
	}
	return html.Div(html.Props{Data: map[string]string{"component": "memory-filter-maturity"}, Class: filterRowClass(tokens)}, children...)
}

func validityFilterTabs(props ListProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	items := []primitives.TabItem{
		{ID: string(ValidityFilterAny), Label: translator.MustText(frontendi18n.MsgMemoryFilterValidityAny)},
		{ID: string(ValidityFilterCurrentlyValid), Label: translator.MustText(frontendi18n.MsgMemoryFilterValidityCurrentlyValid)},
		{ID: string(ValidityFilterNotYetValidated), Label: translator.MustText(frontendi18n.MsgMemoryFilterValidityNotYetValidated)},
		{ID: string(ValidityFilterNoLongerValid), Label: translator.MustText(frontendi18n.MsgMemoryFilterValidityNoLongerValid)},
	}
	return html.Div(html.Props{Data: map[string]string{"component": "memory-filter-validity"}, Class: filterRowClass(tokens)},
		html.H3(html.Props{Class: filterHeadingClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryFilterValidityLabel)}),
		primitives.Tabs(primitives.TabsProps{
			Label: translator.MustText(frontendi18n.MsgMemoryFilterValidityLabel), Items: items,
			SelectedID: string(props.Filter.Validity.normalized()), Mode: mode,
			OnSelect: func(id string) {
				if props.OnFilterChange == nil {
					return
				}
				updated := props.Filter
				updated.Validity = ValidityFilter(id)
				props.OnFilterChange(updated)
			},
		}),
	)
}

func lastConfirmationFilterTabs(props ListProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	items := []primitives.TabItem{
		{ID: string(LastConfirmationFilterAny), Label: translator.MustText(frontendi18n.MsgMemoryFilterConfirmationAny)},
		{ID: string(LastConfirmationFilterWithin7Days), Label: translator.MustText(frontendi18n.MsgMemoryFilterConfirmationWithin7Days)},
		{ID: string(LastConfirmationFilterWithin30Days), Label: translator.MustText(frontendi18n.MsgMemoryFilterConfirmationWithin30Days)},
		{ID: string(LastConfirmationFilterOlderThan30Days), Label: translator.MustText(frontendi18n.MsgMemoryFilterConfirmationOlderThan30Days)},
		{ID: string(LastConfirmationFilterNeverConfirmed), Label: translator.MustText(frontendi18n.MsgMemoryFilterConfirmationNeverConfirmed)},
	}
	return html.Div(html.Props{Data: map[string]string{"component": "memory-filter-last-confirmation"}, Class: filterRowClass(tokens)},
		html.H3(html.Props{Class: filterHeadingClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryFilterLastConfirmationLabel)}),
		primitives.Tabs(primitives.TabsProps{
			Label: translator.MustText(frontendi18n.MsgMemoryFilterLastConfirmationLabel), Items: items,
			SelectedID: string(props.Filter.LastConfirmation.normalized()), Mode: mode,
			OnSelect: func(id string) {
				if props.OnFilterChange == nil {
					return
				}
				updated := props.Filter
				updated.LastConfirmation = LastConfirmationFilter(id)
				props.OnFilterChange(updated)
			},
		}),
	)
}

func containsKind(kinds []domain.MemoryArtifactKind, kind domain.MemoryArtifactKind) bool {
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func containsMaturity(maturities []domain.MaturityState, maturity domain.MaturityState) bool {
	for _, candidate := range maturities {
		if candidate == maturity {
			return true
		}
	}
	return false
}

func toggleKind(kinds []domain.MemoryArtifactKind, kind domain.MemoryArtifactKind, include bool) []domain.MemoryArtifactKind {
	result := make([]domain.MemoryArtifactKind, 0, len(kinds)+1)
	for _, candidate := range kinds {
		if candidate == kind {
			continue
		}
		result = append(result, candidate)
	}
	if include {
		result = append(result, kind)
	}
	return result
}

func toggleMaturity(maturities []domain.MaturityState, maturity domain.MaturityState, include bool) []domain.MaturityState {
	result := make([]domain.MaturityState, 0, len(maturities)+1)
	for _, candidate := range maturities {
		if candidate == maturity {
			continue
		}
		result = append(result, candidate)
	}
	if include {
		result = append(result, maturity)
	}
	return result
}

// -----------------------------------------------------------------------
// Selected-artifact section: detail (M21-092..094) and governance
// (M21-095..098)
// -----------------------------------------------------------------------

func selectedSection(props Props, translator frontendi18n.Translator) ui.Node {
	mode := props.Mode
	switch props.Selected.State {
	case SelectedStateNone:
		return html.Section(html.Props{Data: map[string]string{"component": "project-memory-selected", "state": "none"}},
			primitives.EmptyState(primitives.EmptyStateProps{
				Title: translator.MustText(frontendi18n.MsgMemorySelectedNoneTitle),
				Body:  translator.MustText(frontendi18n.MsgMemorySelectedNoneBody), Mode: mode,
			}),
		)
	case SelectedStateLoading:
		return html.Section(html.Props{Data: map[string]string{"component": "project-memory-selected", "state": "loading"}},
			primitives.Skeleton(primitives.SkeletonProps{
				AccessibleLabel: translator.MustText(frontendi18n.MsgMemorySelectedLoading), Lines: 4, Mode: mode,
			}),
		)
	case SelectedStateReady:
		detail := props.Selected.Detail
		return html.Section(html.Props{Data: map[string]string{
			"component": "project-memory-selected", "state": "ready", "artifact-id": detail.Revision.ArtifactID.String(),
		}},
			identitySection(detail, mode, translator),
			lineageSection(detail, mode, translator),
			episodesSection(detail, mode, translator),
			bindingsSection(detail, mode, translator),
			applicabilitySection(detail, mode, translator),
			historySection(detail, mode, translator),
			correctionSection(props.Selected.Correction, mode, translator),
			quarantineSection(props.Selected.Quarantine, mode, translator),
			invalidationSection(props.Selected.Invalidation, mode, translator),
			deletionSection(props.Selected.Deletion, mode, translator),
		)
	default:
		title, body := selectedUnreadyCopy(props.Selected.State, translator)
		return html.Section(html.Props{Data: map[string]string{"component": "project-memory-selected", "state": string(props.Selected.State)}},
			primitives.ErrorState(primitives.ErrorStateProps{
				Title: title, Body: body + " " + props.Selected.ErrorMessage,
				ActionLabel: translator.MustText(frontendi18n.MsgMemorySelectedRetry),
				Mode:        mode, OnAction: props.Selected.OnRetry,
			}),
		)
	}
}

func selectedUnreadyCopy(state SelectedState, translator frontendi18n.Translator) (string, string) {
	switch state {
	case SelectedStateError:
		return translator.MustText(frontendi18n.MsgMemorySelectedErrorTitle), translator.MustText(frontendi18n.MsgMemorySelectedErrorBody)
	case SelectedStateDenied:
		return translator.MustText(frontendi18n.MsgMemorySelectedDeniedTitle), translator.MustText(frontendi18n.MsgMemorySelectedDeniedBody)
	case SelectedStateIncompatible:
		return translator.MustText(frontendi18n.MsgMemorySelectedIncompatibleTitle), translator.MustText(frontendi18n.MsgMemorySelectedIncompatibleBody)
	case SelectedStateDisconnected:
		return translator.MustText(frontendi18n.MsgMemorySelectedDisconnectedTitle), translator.MustText(frontendi18n.MsgMemorySelectedDisconnectedBody)
	default:
		return translator.MustText(frontendi18n.MsgDataUnknownState), ""
	}
}

func identitySection(detail DetailProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	revision := detail.Revision
	scope := revision.Scope.Project.String()
	if !revision.Scope.Repository.IsZero() {
		scope += " / " + revision.Scope.Repository.String()
	}
	children := []definition{
		{translator.MustText(frontendi18n.MsgMemoryDetailIdentityArtifactID), revision.ArtifactID.String()},
		{translator.MustText(frontendi18n.MsgMemoryDetailIdentityRevisionID), revision.RevisionID.String()},
		{translator.MustText(frontendi18n.MsgMemoryDetailIdentityScope), scope},
	}
	if !revision.SupersedesRevision.IsZero() {
		children = append(children, definition{translator.MustText(frontendi18n.MsgMemoryDetailIdentitySupersedes), revision.SupersedesRevision.String()})
	}
	extra := []ui.Node{
		primitives.Badge(primitives.BadgeProps{
			Label: translator.MustText(maturityMessageID(revision.Maturity)), Status: maturityStatusTone(revision.Maturity), Mode: mode,
		}),
	}
	if revision.CreatedFromCorrection {
		extra = append(extra, html.P(html.Props{
			Data: map[string]string{"field": "created-from-correction"},
			Text: translator.MustText(frontendi18n.MsgMemoryDetailIdentityFromCorrection),
		}))
	}
	return inspectorSection(mode, "identity", translator.MustText(frontendi18n.MsgMemoryDetailIdentityTitle),
		definitionList(tokens, children...), html.Div(html.Props{Class: rowClass(tokens)}, extra...),
	)
}

func lineageSection(detail DetailProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	lineage := detail.Lineage
	return inspectorSection(mode, "lineage", translator.MustText(frontendi18n.MsgMemoryDetailLineageTitle),
		ancestorList(mode, translator, translator.MustText(frontendi18n.MsgMemoryDetailLineageDerivedFrom), "derived-from", lineage.DerivedFrom, detail.Ancestors),
		ancestorList(mode, translator, translator.MustText(frontendi18n.MsgMemoryDetailLineageInfluencedBy), "influenced-by", lineage.InfluencedBy, detail.Ancestors),
		originSection(lineage, mode, translator),
		rootsSection(lineage, mode, translator),
	)
}

// originSection renders lineage's origin per its Known/UnknownReason
// discipline: "Unknown — <reason>" when OriginKnown is false, explicit
// self-origin copy when OriginKnown is true and OriginArtifactID is the
// legitimate zero value, and the origin identity otherwise. Per AGENTS.md
// "Never display unknown cost as zero" applied here (see
// domain.MemoryArtifactLineage's doc comment): the zero OriginArtifactID
// must never be rendered as a bare, unexplained "—".
func originSection(lineage domain.MemoryArtifactLineage, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	var text string
	switch {
	case !lineage.OriginKnown:
		text = translator.MustText(frontendi18n.MsgMemoryDetailLineageOriginUnknown, map[string]any{"message": lineage.OriginUnknownReason})
	case lineage.OriginArtifactID.IsZero():
		text = translator.MustText(frontendi18n.MsgMemoryDetailLineageOriginIsSelf)
	default:
		text = lineage.OriginArtifactID.String()
	}
	return html.Div(html.Props{Data: map[string]string{"component": "memory-lineage-origin", "known": strconv.FormatBool(lineage.OriginKnown)}},
		html.H4(html.Props{Class: subheadingClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailLineageOrigin)}),
		html.P(html.Props{Class: identityTextClass(tokens), Text: text}),
	)
}

// rootsSection renders lineage's roots per its Known/UnknownReason
// discipline: "Unknown — <reason>" when LineageRootsKnown is false, explicit
// "this artifact is the only root" copy when LineageRootsKnown is true and
// LineageRootIDs is legitimately empty, and the root list otherwise. Per
// AGENTS.md "Never display unknown cost as zero" applied here: an empty
// LineageRootIDs must never render as a silent, textless empty list.
func rootsSection(lineage domain.MemoryArtifactLineage, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	children := []ui.Node{html.H4(html.Props{Class: subheadingClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailLineageRoots)})}
	switch {
	case !lineage.LineageRootsKnown:
		children = append(children, html.P(html.Props{
			Class: secondaryTextClass(tokens),
			Text:  translator.MustText(frontendi18n.MsgMemoryDetailLineageRootsUnknown, map[string]any{"message": lineage.LineageRootsUnknownReason}),
		}))
	case len(lineage.LineageRootIDs) == 0:
		children = append(children, html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailLineageRootsIsSelf)}))
	default:
		items := make([]ui.Node, 0, len(lineage.LineageRootIDs))
		for _, id := range lineage.LineageRootIDs {
			items = append(items, html.Li(html.Props{Class: identityTextClass(tokens), Text: id.String()}))
		}
		children = append(children, html.Ul(html.Props{
			Data: map[string]string{"inspector-list": "roots", "count": strconv.Itoa(len(items))}, Class: listClass(tokens),
		}, items...))
	}
	return html.Div(html.Props{Data: map[string]string{"component": "memory-lineage-roots", "known": strconv.FormatBool(lineage.LineageRootsKnown)}}, children...)
}

func ancestorList(
	mode primitives.Mode, translator frontendi18n.Translator, title, kind string,
	ids []domain.MemoryArtifactID, lookup map[domain.MemoryArtifactID]AncestorSummary,
) ui.Node {
	tokens := mode.Tokens()
	children := []ui.Node{html.H4(html.Props{Class: subheadingClass(tokens), Text: title})}
	if len(ids) == 0 {
		children = append(children, html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailLineageEmpty)}))
		return html.Div(html.Props{Data: map[string]string{"component": "memory-lineage-" + kind}}, children...)
	}
	items := make([]ui.Node, 0, len(ids))
	for _, id := range ids {
		summary, found := lookup[id]
		if !found || !summary.Known {
			reason := ""
			if found {
				reason = summary.UnknownReason
			}
			text := translator.MustText(frontendi18n.MsgMemoryDetailLineageUnknownAncestor)
			if reason != "" {
				text += ": " + reason
			}
			items = append(items, html.Li(html.Props{
				Data: map[string]string{"artifact-id": id.String(), "known": "false"}, Class: itemClass(tokens),
				Text: id.String() + " — " + text,
			}))
			continue
		}
		items = append(items, html.Li(html.Props{
			Data: map[string]string{"artifact-id": id.String(), "known": "true"}, Class: itemClass(tokens),
		},
			primitives.IconChip(primitives.IconChipProps{Label: translator.MustText(kindMessageID(summary.Kind)), Kind: designKindForArtifactKind(summary.Kind), Mode: mode}),
			primitives.Badge(primitives.BadgeProps{Label: translator.MustText(maturityMessageID(summary.Maturity)), Status: maturityStatusTone(summary.Maturity), Mode: mode}),
			html.Span(html.Props{Class: identityTextClass(tokens), Text: id.String()}),
		))
	}
	children = append(children, html.Ul(html.Props{
		Data: map[string]string{"inspector-list": kind, "count": strconv.Itoa(len(items))}, Class: listClass(tokens),
	}, items...))
	return html.Div(html.Props{Data: map[string]string{"component": "memory-lineage-" + kind}}, children...)
}

func plainListNode(mode primitives.Mode, title, kind string, values []string) ui.Node {
	tokens := mode.Tokens()
	children := []ui.Node{html.H4(html.Props{Class: subheadingClass(tokens), Text: title})}
	items := make([]ui.Node, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		items = append(items, html.Li(html.Props{Class: identityTextClass(tokens), Text: value}))
	}
	children = append(children, html.Ul(html.Props{
		Data: map[string]string{"inspector-list": kind, "count": strconv.Itoa(len(items))}, Class: listClass(tokens),
	}, items...))
	return html.Div(html.Props{Data: map[string]string{"component": "memory-lineage-" + kind}}, children...)
}

func episodesSection(detail DetailProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	if len(detail.SupportingEpisodes) == 0 {
		return inspectorSection(mode, "episodes", translator.MustText(frontendi18n.MsgMemoryDetailEpisodesTitle),
			html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailEpisodesEmpty)}),
		)
	}
	items := make([]ui.Node, 0, len(detail.SupportingEpisodes))
	for _, episode := range detail.SupportingEpisodes {
		text := episode.Summary
		if !episode.Known {
			text = translator.MustText(frontendi18n.MsgMemoryDetailEpisodeUnknown, map[string]any{"message": episode.UnknownReason})
		}
		items = append(items, html.Li(html.Props{
			Data: map[string]string{"episode-id": episode.Episode.String(), "known": strconv.FormatBool(episode.Known)}, Class: itemClass(tokens),
		},
			html.P(html.Props{Class: identityTextClass(tokens), Text: episode.Episode.String()}),
			html.P(html.Props{Class: secondaryTextClass(tokens), Text: text}),
		))
	}
	return inspectorSection(mode, "episodes", translator.MustText(frontendi18n.MsgMemoryDetailEpisodesTitle),
		html.Ul(html.Props{
			Data: map[string]string{"inspector-list": "supporting-episodes", "count": strconv.Itoa(len(items))}, Class: listClass(tokens),
		}, items...),
	)
}

func bindingsSection(detail DetailProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	binding, ok := RevisionBindingFor(detail.Revision.Content)
	if !ok {
		return inspectorSection(mode, "bindings", translator.MustText(frontendi18n.MsgMemoryDetailBindingsTitle),
			html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailBindingsUnmodeled)}),
		)
	}
	if !binding.Known {
		return inspectorSection(mode, "bindings", translator.MustText(frontendi18n.MsgMemoryDetailBindingsTitle),
			html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailBindingsUnknown, map[string]any{"message": binding.UnknownReason})}),
		)
	}
	var children []definition
	if binding.ExactRevision != "" {
		children = append(children, definition{translator.MustText(frontendi18n.MsgMemoryDetailBindingsExactRevision), binding.ExactRevision})
	}
	if binding.ValidFromRevision != "" {
		children = append(children, definition{translator.MustText(frontendi18n.MsgMemoryDetailBindingsValidFrom), binding.ValidFromRevision})
	}
	if binding.ValidUntilRevision != "" {
		children = append(children, definition{translator.MustText(frontendi18n.MsgMemoryDetailBindingsValidUntil), binding.ValidUntilRevision})
	}
	return inspectorSection(mode, "bindings", translator.MustText(frontendi18n.MsgMemoryDetailBindingsTitle), definitionList(tokens, children...))
}

func applicabilitySection(detail DetailProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	statement, ok := ApplicabilityStatementFor(detail.Revision.Content)
	if !ok {
		return inspectorSection(mode, "applicability", translator.MustText(frontendi18n.MsgMemoryDetailApplicabilityTitle),
			html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailApplicabilityUnmodeled)}),
		)
	}
	return inspectorSection(mode, "applicability", translator.MustText(frontendi18n.MsgMemoryDetailApplicabilityTitle),
		html.P(html.Props{Class: secondaryTextClass(tokens), Text: statement}),
	)
}

func historySection(detail DetailProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	influenced := retrievalBucket(detail.RetrievalHistory, RetrievalOutcomeInfluencedOutcome)
	retrievedOnly := retrievalBucket(detail.RetrievalHistory, RetrievalOutcomeRetrievedOnly)
	rejected := retrievalBucket(detail.RetrievalHistory, RetrievalOutcomeRetrievedAndRejected)
	return inspectorSection(mode, "history", translator.MustText(frontendi18n.MsgMemoryDetailHistoryTitle),
		retrievalList(mode, translator, translator.MustText(frontendi18n.MsgMemoryDetailHistoryInfluenced), "influenced", influenced),
		retrievalList(mode, translator, translator.MustText(frontendi18n.MsgMemoryDetailHistoryRetrieved), "retrieved-only", retrievedOnly),
		retrievalList(mode, translator, translator.MustText(frontendi18n.MsgMemoryDetailHistoryRejected), "rejected", rejected),
	)
}

func retrievalBucket(records []RetrievalRecord, outcome RetrievalOutcome) []RetrievalRecord {
	bucket := make([]RetrievalRecord, 0, len(records))
	for _, record := range records {
		if record.Outcome == outcome {
			bucket = append(bucket, record)
		}
	}
	return bucket
}

func retrievalList(mode primitives.Mode, translator frontendi18n.Translator, title, kind string, records []RetrievalRecord) ui.Node {
	tokens := mode.Tokens()
	children := []ui.Node{html.H4(html.Props{Class: subheadingClass(tokens), Text: title})}
	if len(records) == 0 {
		children = append(children, html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDetailHistoryEmpty)}))
		return html.Div(html.Props{Data: map[string]string{"component": "memory-history-" + kind}}, children...)
	}
	items := make([]ui.Node, 0, len(records))
	for _, record := range records {
		text := record.Note
		if !record.Known {
			text = translator.MustText(frontendi18n.MsgMemoryDetailEpisodeUnknown, map[string]any{"message": record.UnknownReason})
		}
		items = append(items, html.Li(html.Props{
			Data:  map[string]string{"episode-id": record.Episode.String(), "outcome": string(record.Outcome), "known": strconv.FormatBool(record.Known)},
			Class: itemClass(tokens),
		},
			html.P(html.Props{Class: identityTextClass(tokens), Text: record.Episode.String()}),
			html.P(html.Props{Class: secondaryTextClass(tokens), Text: text}),
		))
	}
	children = append(children, html.Ul(html.Props{
		Data: map[string]string{"inspector-list": kind, "count": strconv.Itoa(len(items))}, Class: listClass(tokens),
	}, items...))
	return html.Div(html.Props{Data: map[string]string{"component": "memory-history-" + kind}}, children...)
}

// -----------------------------------------------------------------------
// Correction (M21-095)
// -----------------------------------------------------------------------

func correctionSection(props CorrectionProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	_, previewErr := PreviewCorrection(props)
	fieldLabel := translator.MustText(correctionFieldMessageID(props.Prior.Content.Kind))
	children := []ui.Node{
		html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryCorrectionExplain)}),
	}
	if correctableFieldIsList(props.Prior.Content.Kind) {
		children = append(children,
			html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryCorrectionListFieldExplain)}),
			correctionListField(mode, "memory-correction-draft", fieldLabel, props.Draft, props.Busy, props.OnDraftChange),
		)
	} else {
		children = append(children, primitives.TextField(primitives.TextFieldProps{
			ID: "memory-correction-draft", Label: fieldLabel,
			Value: props.Draft, Disabled: props.Busy, Mode: mode, OnInput: props.OnDraftChange,
		}))
	}
	if previewErr != nil {
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Message: translator.MustText(frontendi18n.MsgMemoryCorrectionInvalid, map[string]any{"message": previewErr.Error()}),
			Tone:    design.StatusWarning, Mode: mode,
		}))
	}
	children = append(children, primitives.Button(primitives.ButtonProps{
		ID: "memory-correction-submit", Label: translator.MustText(frontendi18n.MsgMemoryCorrectionSubmit),
		Primary: true, Disabled: previewErr != nil, Busy: props.Busy, Mode: mode,
		OnClick: func() { ActivateCorrection(props) },
	}))
	return inspectorSection(mode, "correction", translator.MustText(frontendi18n.MsgMemoryCorrectionTitle), children...)
}

// correctionListField renders a one-value-per-line multi-line editor for a
// list-shaped correctable field (Argv, TestPaths). It exists specifically
// because primitives.TextField is single-line and the historical "; "-joined
// single-line editor is what let a value containing "; " be silently split
// on round trip (see splitCorrectableList). A textarea's value cannot
// contain a value-corrupting delimiter for these fields, because a newline
// is the one character none of their elements can legitimately contain.
func correctionListField(mode primitives.Mode, id, label, value string, disabled bool, onInput func(string)) ui.Node {
	tokens := mode.Tokens()
	inputProps := html.PropsOf(html.OnInput(func(event ui.InputEvent) {
		if onInput != nil {
			onInput(event.GetValue())
		}
	}))
	inputProps.ID = id
	inputProps.Value = value
	inputProps.Disabled = disabled
	inputProps.Rows = 4
	inputProps.Aria = map[string]string{"label": label}
	inputProps.Data = map[string]string{"component": "memory-correction-list-field"}
	inputProps.Class = css.New(
		css.MinHeight(css.Px(96)),
		css.PaddingX(css.Px(tokens.Spacing.MD)), css.PaddingY(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Code.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Code.LineHeight)),
		css.MaxWidth(css.Percent(100)),
	).String()
	labelClass := css.New(
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.ControlLabel.LineHeight)),
		css.FontWeight.Semibold,
	).String()
	return html.Div(html.Props{Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String()},
		html.Label(html.Props{For: id, Class: labelClass}, html.Text(label)),
		html.Textarea(inputProps, html.Text(value)),
	)
}

// -----------------------------------------------------------------------
// Quarantine (M21-096)
// -----------------------------------------------------------------------

func quarantineSection(props QuarantineProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	available := TransitionAvailability(props.Revision.Maturity, domain.MaturityStateQuarantined) == nil
	children := []ui.Node{
		html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryQuarantineExplain)}),
	}
	if !available {
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Message: translator.MustText(frontendi18n.MsgMemoryQuarantineUnavailable, map[string]any{
				"message": translator.MustText(maturityMessageID(props.Revision.Maturity)),
			}),
			Tone: design.StatusNeutral, Mode: mode,
		}))
	}
	children = append(children,
		primitives.ToggleButton(primitives.ToggleButtonProps{
			ID: "memory-quarantine-confirm", Label: translator.MustText(frontendi18n.MsgMemoryQuarantineConfirm),
			Pressed: props.Confirmed, Disabled: props.Busy || !available, Mode: mode,
			OnToggle: props.OnConfirmChange,
		}),
		primitives.Button(primitives.ButtonProps{
			ID: "memory-quarantine-submit", Label: translator.MustText(frontendi18n.MsgMemoryQuarantineSubmit),
			Disabled: !available || !props.Confirmed, Busy: props.Busy, Mode: mode,
			OnClick: func() { ActivateQuarantine(props) },
		}),
	)
	return inspectorSection(mode, "quarantine", translator.MustText(frontendi18n.MsgMemoryQuarantineTitle), children...)
}

// -----------------------------------------------------------------------
// Invalidation (M21-097)
// -----------------------------------------------------------------------

func invalidationSection(props InvalidationProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	available := TransitionAvailability(props.Revision.Maturity, domain.MaturityStateInvalidated) == nil
	children := []ui.Node{
		html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryInvalidationExplain)}),
	}
	if !available {
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Message: translator.MustText(frontendi18n.MsgMemoryInvalidationUnavailable, map[string]any{
				"message": translator.MustText(maturityMessageID(props.Revision.Maturity)),
			}),
			Tone: design.StatusNeutral, Mode: mode,
		}))
	}
	trimmedReason := trimmed(props.Reason)
	children = append(children,
		primitives.TextField(primitives.TextFieldProps{
			ID: "memory-invalidation-reason", Label: translator.MustText(frontendi18n.MsgMemoryInvalidationReasonLabel),
			Value: props.Reason, Required: true, Disabled: props.Busy || !available, Mode: mode, OnInput: props.OnReasonChange,
		}),
		primitives.Button(primitives.ButtonProps{
			ID: "memory-invalidation-submit", Label: translator.MustText(frontendi18n.MsgMemoryInvalidationSubmit),
			Disabled: !available || trimmedReason == "", Busy: props.Busy, Mode: mode,
			OnClick: func() { ActivateInvalidation(props) },
		}),
	)
	return inspectorSection(mode, "invalidation", translator.MustText(frontendi18n.MsgMemoryInvalidationTitle), children...)
}

func trimmed(value string) string {
	result := value
	for len(result) > 0 && (result[0] == ' ' || result[0] == '\t' || result[0] == '\n' || result[0] == '\r') {
		result = result[1:]
	}
	for len(result) > 0 {
		last := result[len(result)-1]
		if last == ' ' || last == '\t' || last == '\n' || last == '\r' {
			result = result[:len(result)-1]
			continue
		}
		break
	}
	return result
}

// -----------------------------------------------------------------------
// Deletion with affected-descendant preview (M21-098)
// -----------------------------------------------------------------------

func deletionSection(props DeletionProps, mode primitives.Mode, translator frontendi18n.Translator) ui.Node {
	tokens := mode.Tokens()
	switch props.PreviewState {
	case DeletionPreviewLoading:
		return inspectorSection(mode, "deletion", translator.MustText(frontendi18n.MsgMemoryDeletionTitle),
			html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryDeletionExplain)}),
			primitives.Skeleton(primitives.SkeletonProps{AccessibleLabel: translator.MustText(frontendi18n.MsgMemoryDeletionPreviewLoading), Lines: 2, Mode: mode}),
		)
	case DeletionPreviewError:
		return inspectorSection(mode, "deletion", translator.MustText(frontendi18n.MsgMemoryDeletionTitle),
			primitives.ErrorState(primitives.ErrorStateProps{
				Title: translator.MustText(frontendi18n.MsgMemoryDeletionPreviewLoading), Body: props.ErrorMessage,
				ActionLabel: translator.MustText(frontendi18n.MsgMemorySelectedRetry), Mode: mode, OnAction: props.OnRetryPreview,
			}),
		)
	default:
		direct := artifactIDStrings(props.Preview.DirectDependents)
		contextual := artifactIDStrings(props.Preview.ContextuallyInfluenced)
		acknowledged := artifactIDStrings(props.AcknowledgedDependents)
		acknowledgedAll := acknowledgesAllDirectDependents(props)
		children := []ui.Node{
			plainListNode(mode, translator.MustText(frontendi18n.MsgMemoryDeletionDirectDependents), "direct-dependents", noneIfEmpty(direct, translator)),
			plainListNode(mode, translator.MustText(frontendi18n.MsgMemoryDeletionContextuallyInfluenced), "contextually-influenced", noneIfEmpty(contextual, translator)),
		}
		if len(props.Preview.DirectDependents) > 0 {
			// Acknowledgement now names exactly which dependents the user
			// accepted losing (domain.MemoryArtifactDeletionRequest), so this
			// surfaces the acknowledged identities themselves rather than a
			// bare confirmed/unconfirmed checkbox with no data behind it.
			children = append(children, plainListNode(mode, translator.MustText(frontendi18n.MsgMemoryDeletionAcknowledgedList), "acknowledged-dependents", noneIfEmpty(acknowledged, translator)))
		}
		children = append(children,
			primitives.ToggleButton(primitives.ToggleButtonProps{
				ID: "memory-deletion-acknowledge", Label: translator.MustText(frontendi18n.MsgMemoryDeletionAcknowledge),
				Pressed: acknowledgedAll, Disabled: props.Busy, Mode: mode,
				OnToggle: func(next bool) {
					if props.OnAcknowledgeChange == nil {
						return
					}
					if next {
						props.OnAcknowledgeChange(append([]domain.MemoryArtifactID(nil), props.Preview.DirectDependents...))
						return
					}
					props.OnAcknowledgeChange(nil)
				},
			}),
			primitives.Button(primitives.ButtonProps{
				ID: "memory-deletion-submit", Label: translator.MustText(frontendi18n.MsgMemoryDeletionSubmit),
				Disabled: len(direct) > 0 && !acknowledgedAll, Busy: props.Busy, Mode: mode,
				OnClick: func() { ActivateDeletion(props) },
			}),
		)
		return inspectorSection(mode, "deletion", translator.MustText(frontendi18n.MsgMemoryDeletionTitle), children...)
	}
}

func artifactIDStrings(values []domain.MemoryArtifactID) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func noneIfEmpty(values []string, translator frontendi18n.Translator) []string {
	if len(values) == 0 {
		return []string{translator.MustText(frontendi18n.MsgMemoryDeletionNone)}
	}
	return values
}

// -----------------------------------------------------------------------
// Export selected structured records without secrets (M21-099)
// -----------------------------------------------------------------------

func exportSection(props Props, translator frontendi18n.Translator) ui.Node {
	mode := props.Mode
	tokens := mode.Tokens()
	export := props.Export
	children := []ui.Node{
		html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryExportExplain)}),
	}
	if len(export.Candidates) == 0 {
		children = append(children, html.P(html.Props{Class: secondaryTextClass(tokens), Text: translator.MustText(frontendi18n.MsgMemoryExportEmpty)}))
		return inspectorSection(mode, "export", translator.MustText(frontendi18n.MsgMemoryExportTitle), children...)
	}
	items := make([]ui.Node, 0, len(export.Candidates))
	selectedCount := 0
	for _, candidate := range export.Candidates {
		candidate := candidate
		if candidate.Selected {
			selectedCount++
		}
		label := translator.MustText(kindMessageID(candidate.Record.Content.Kind)) + " " + candidate.Record.ArtifactID.String()
		items = append(items, html.Li(html.Props{
			Data:  map[string]string{"artifact-id": candidate.Record.ArtifactID.String(), "selected": strconv.FormatBool(candidate.Selected)},
			Class: itemClass(tokens),
		},
			primitives.ToggleButton(primitives.ToggleButtonProps{
				ID:      "memory-export-select-" + candidate.Record.ArtifactID.String(),
				Label:   translator.MustText(frontendi18n.MsgMemoryExportSelect, map[string]any{"label": label}),
				Pressed: candidate.Selected, Disabled: export.Busy, Mode: mode,
				OnToggle: func(next bool) {
					if export.OnToggleSelect != nil {
						export.OnToggleSelect(candidate.Record.ArtifactID, next)
					}
				},
			}),
		))
	}
	children = append(children,
		html.Ul(html.Props{
			Data:  map[string]string{"inspector-list": "export-candidates", "count": strconv.Itoa(len(items)), "selected-count": strconv.Itoa(selectedCount)},
			Class: listClass(tokens),
		}, items...),
		primitives.Button(primitives.ButtonProps{
			ID: "memory-export-submit", Label: translator.MustText(frontendi18n.MsgMemoryExportSubmit),
			Primary: true, Disabled: selectedCount == 0, Busy: export.Busy, Mode: mode,
			OnClick: func() { ActivateExport(export) },
		}),
	)
	return inspectorSection(mode, "export", translator.MustText(frontendi18n.MsgMemoryExportTitle), children...)
}

// -----------------------------------------------------------------------
// Shared layout primitives
// -----------------------------------------------------------------------

type definition struct{ label, value string }

func definitionList(tokens design.Tokens, definitions ...definition) ui.Node {
	children := make([]ui.Node, 0, len(definitions)*2)
	for _, item := range definitions {
		children = append(children,
			html.Tag("dt", html.Props{Class: definitionLabelClass(tokens), Text: item.label}),
			html.Tag("dd", html.Props{Class: definitionValueClass(tokens), Text: item.value}),
		)
	}
	return html.Tag("dl", html.Props{Class: definitionGridClass(tokens)}, children...)
}

func inspectorSection(mode primitives.Mode, kind, title string, children ...ui.Node) ui.Node {
	tokens := mode.Tokens()
	content := []ui.Node{html.H3(html.Props{Class: sectionHeadingClass(tokens), Text: title})}
	content = append(content, children...)
	return html.Section(html.Props{
		Aria: map[string]string{"label": title}, Data: map[string]string{"inspector-section": kind},
		Class: sectionClass(tokens),
	}, content...)
}

func rootClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Rhythm.PanelGap)), css.MaxWidth(css.Percent(100)), css.Font(css.FontStack(tokens.Fonts.UI))).String()
}

func sectionClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
		css.Padding(css.Px(tokens.Spacing.MD)), css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
	).String()
}

func sectionHeadingClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)), css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)), css.FontWeight.Semibold).String()
}

func subheadingClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)), css.FontWeight.Semibold).String()
}

func secondaryTextClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight))).String()
}

func identityTextClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.Font(css.FontStack(tokens.Fonts.Code)), css.FontSize(css.Px(tokens.Typography.Code.Size)), css.LineHeightLen(css.Px(tokens.Typography.Code.LineHeight)), css.OverflowWrap.Anywhere).String()
}

func definitionGridClass(tokens design.Tokens) string {
	return css.New(u.Grid, css.GridCols(css.Repeat(2, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))), css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero)).String()
}

func definitionLabelClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)), css.FontWeight.Semibold).String()
}

func definitionValueClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)), css.OverflowWrap.Anywhere).String()
}

func listClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero), css.Padding(css.Px(tokens.Spacing.SM))).String()
}

func itemClass(tokens design.Tokens) string {
	return css.New(css.Padding(css.Px(tokens.Spacing.SM)), css.Bg(css.Hex(string(tokens.Colors.Surface3))), css.Rounded(css.Px(tokens.Geometry.RadiusSmall))).String()
}

func rowClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)), css.Padding(css.Px(tokens.Spacing.SM))).String()
}

func rowSummaryClass(tokens design.Tokens) string {
	return css.New(css.Overflow.Hidden, css.TextOverflowEllipsis(), css.WhiteSpace.NoWrap, css.MinWidth(css.Zero)).String()
}

func rowMetaClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight))).String()
}

func filterGroupClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM))).String()
}

func filterRowClass(tokens design.Tokens) string {
	return css.New(u.Flex, css.FlexWrap.Wrap, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS))).String()
}

func filterHeadingClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)), css.FontWeight.Semibold).String()
}
