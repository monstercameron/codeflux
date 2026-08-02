package memoryinspector

import (
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
)

// KindLabel returns the reader-facing name of one memory artifact kind.
//
// It is exported so surfaces outside this package name a kind the same way the
// inspector does. Two screens calling the same artifact "reviewed command" and
// "verified command" would read as two different things.
func KindLabel(translator frontendi18n.Translator, kind domain.MemoryArtifactKind) string {
	return translatorOrEnglish(translator).MustText(kindMessageID(kind))
}

// MaturityLabel returns the reader-facing name of one governed maturity state.
func MaturityLabel(translator frontendi18n.Translator, maturity domain.MaturityState) string {
	return translatorOrEnglish(translator).MustText(maturityMessageID(maturity))
}

// MaturityTone returns the status tone that carries a maturity state's
// standing, so authority-bearing and quarantined memory never look alike.
func MaturityTone(maturity domain.MaturityState) design.Status {
	return maturityStatusTone(maturity)
}

// DesignKindFor returns the typed-content color a memory artifact kind is
// presented in.
func DesignKindFor(kind domain.MemoryArtifactKind) design.Kind {
	return designKindForArtifactKind(kind)
}
