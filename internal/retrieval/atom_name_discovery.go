package retrieval

import (
	"context"

	"codeflux.dev/codeflux/internal/atomname"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// discoverByAtomName searches every currently valid atom canonical name,
// normalized phrase, and active alias in the task's project against every
// affected-symbol hint on the task's own fingerprint, BEFORE any
// vector-similarity discovery could ever run (M21-167). It never performs a
// similarity search: internal/atomname.MatchAtomNamesAndAliases is a pure,
// deterministic exact-text match over already-loaded name/alias records,
// mirroring the "structured fields first" ordering every other discovery
// phase in discoverCandidates already applies.
//
// A name/alias match here only refines the ATTRIBUTION of an
// ExecutableAtomReference candidate discoverByKind already admitted for this
// repository: domain.ExecutableAtomReferenceContent is itself
// repository-scoped (M21-067's own structured repository+kind filter is
// what actually admits an atom reference into the candidate set at all), so
// there is no atom reference this phase could newly discover outside that
// repository's own set. This phase's job is M21-168 -- recording WHICH
// canonical name, normalized phrase, or alias text caused an
// already-discovered atom to match, not admitting new candidates from
// outside the repository. atomReferenceRevisions is exactly the slice
// discoverByKind(..., MemoryArtifactKindExecutableAtomReference, ...)
// already returned; this function does not re-query storage for it.
func (service *Service) discoverByAtomName(
	ctx context.Context,
	input PreWorkGateInput,
	atomReferenceRevisions []storage.MemoryArtifactRevisionRecord,
	add func(storage.MemoryArtifactRevisionRecord, storage.MemoryRetrievalCandidateSource, string),
) error {
	if len(input.Task.AffectedSymbols) == 0 || len(atomReferenceRevisions) == 0 {
		return nil
	}

	byAtom := map[domain.AtomID][]storage.MemoryArtifactRevisionRecord{}
	for _, revision := range atomReferenceRevisions {
		if revision.Content.AtomReference == nil {
			continue
		}
		atomID := revision.Content.AtomReference.Atom
		byAtom[atomID] = append(byAtom[atomID], revision)
	}
	if len(byAtom) == 0 {
		return nil
	}

	names, err := service.store.ListAtomNamesByProject(ctx, input.ProjectID)
	if err != nil {
		return err
	}
	aliases, err := service.store.ListActiveAtomNameAliasesByProject(ctx, input.ProjectID)
	if err != nil {
		return err
	}

	nameRecords := make([]atomname.AtomNameRecord, 0, len(names))
	for _, revision := range names {
		if _, relevant := byAtom[revision.AtomID]; !relevant {
			continue
		}
		nameRecords = append(nameRecords, atomNameRecordFromRevision(revision))
	}
	aliasRecords := make([]atomname.AtomNameAliasRecord, 0, len(aliases))
	for _, alias := range aliases {
		if _, relevant := byAtom[alias.AtomID]; !relevant {
			continue
		}
		aliasRecords = append(aliasRecords, atomNameAliasRecordFromStorage(alias))
	}
	if len(nameRecords) == 0 && len(aliasRecords) == 0 {
		return nil
	}

	for _, symbol := range input.Task.AffectedSymbols {
		for _, match := range atomname.MatchAtomNamesAndAliases(symbol, nameRecords, aliasRecords) {
			channel := nameMatchStorageChannel(match.Channel)
			for _, revision := range byAtom[match.AtomID] {
				add(revision, channel, match.MatchedText)
			}
		}
	}
	return nil
}

// nameMatchStorageChannel maps an atomname.NameMatchChannel onto the coarser
// storage.MemoryRetrievalCandidateSource vocabulary, per
// internal/retrievalgate.DiscoveryChannelExactIdentity/
// DiscoveryChannelStructuredFields's own doc comments: canonical-name and
// normalized-phrase matches are exact-identity matches ("the candidate's
// exact fingerprint hash, canonical name, or normalized phrase matched the
// query exactly"); an active-alias match is a structured-field match ("a
// reviewed alias" is explicitly one of DiscoveryChannelStructuredFields's
// own examples).
func nameMatchStorageChannel(channel atomname.NameMatchChannel) storage.MemoryRetrievalCandidateSource {
	switch channel {
	case atomname.NameMatchChannelCanonicalName, atomname.NameMatchChannelNormalizedPhrase:
		return storage.RetrievalCandidateExactMatch
	default: // atomname.NameMatchChannelActiveAlias
		return storage.RetrievalCandidateApplicabilityPass
	}
}

// atomNameRecordFromRevision adapts a storage.AtomNameRevision (this
// package's storage-lane read shape) into the atomname.AtomNameRecord
// MatchAtomNamesAndAliases requires. Every field is the same
// internal/atomname type on both sides (storage persists exactly what
// internal/atomname produced), so this is a lossless, non-computing
// re-shaping, not a reinterpretation.
func atomNameRecordFromRevision(revision storage.AtomNameRevision) atomname.AtomNameRecord {
	return atomname.AtomNameRecord{
		RevisionID:           revision.RevisionID,
		AtomID:               revision.AtomID,
		AtomVersion:          revision.AtomVersion,
		Scope:                revision.Scope,
		SchemaVersion:        revision.SchemaVersion,
		CanonicalName:        revision.CanonicalName,
		DisplayName:          revision.DisplayName,
		NormalizedPhrase:     revision.NormalizedPhrase,
		Rationale:            revision.Rationale,
		RevisionSequence:     revision.RevisionSequence,
		SupersedesRevisionID: revision.SupersedesRevisionID,
		Valid:                revision.Valid,
		CreatedAt:            revision.CreatedAt,
	}
}

// atomNameAliasRecordFromStorage adapts a storage.AtomNameAlias into the
// atomname.AtomNameAliasRecord MatchAtomNamesAndAliases requires, the same
// lossless re-shaping atomNameRecordFromRevision performs above.
func atomNameAliasRecordFromStorage(alias storage.AtomNameAlias) atomname.AtomNameAliasRecord {
	return atomname.AtomNameAliasRecord{
		AtomID:              alias.AtomID,
		AliasText:           alias.AliasText,
		NormalizedAliasText: alias.NormalizedAliasText,
		Source:              alias.Source,
		ActiveFrom:          alias.ActiveFrom,
		ActiveUntil:         alias.ActiveUntil,
		InvalidatedReason:   alias.InvalidatedReason,
	}
}
