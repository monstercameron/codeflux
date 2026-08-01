package timelinecard

import "codeflux.dev/codeflux/web/frontend/design"

// DesignKind maps a timeline card's Kind to the typed design-system content
// category (design.Kind) used to color-code it with a per-kind icon chip.
// This package stays renderer-neutral: it returns the presentation-neutral
// mapping so any renderer (currently web/frontend/timelineview) can resolve
// design.KindPresentationFor without this model package importing HTML/CSS.
//
// The mapping favors the mockup reference vocabulary (Code / Test / Plan /
// Evidence / Memory / Forecast / Execution / Validation) over a literal
// one-to-one translation of every internal card Kind, because several
// internal kinds are naturally instances of one visual category: a tool
// activity or diff summary both read as "Code" work; a checkpoint or
// recovery record both read as "Memory" (durable, backward-looking state).
func (kind Kind) DesignKind() (design.Kind, bool) {
	switch kind {
	case KindForecast:
		return design.KindForecast, true
	case KindPlan, KindPlanRevision:
		return design.KindPlan, true
	case KindTool, KindDiff:
		return design.KindCode, true
	case KindValidation:
		return design.KindValidation, true
	case KindCompletion:
		return design.KindEvidence, true
	case KindCheckpoint, KindRecovery, KindContext:
		return design.KindMemory, true
	case KindApproval, KindTaskState, KindGraphChange:
		return design.KindExecution, true
	default:
		return "", false
	}
}

// DesignKind resolves the card's own Kind through Kind.DesignKind.
func (card Card) DesignKind() (design.Kind, bool) {
	return card.Kind.DesignKind()
}
