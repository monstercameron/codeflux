// Package storage: project-scoped atom documentation discovery text
// (M21-G07, "rich atom comments improve candidate discovery without
// bypassing exact applicability, evidence, or assurance checks").
//
// This read path exists so documentation text can enter DISCOVERY. It
// deliberately returns no eligibility signal of any kind: discovery names
// candidates, and internal/retrievalgate decides whether any of them may be
// used. docs/plan.md §0 prohibits `vector similarity -> applicability or
// authority`, and the same separation applies to descriptive comment text —
// a richly documented atom is easier to FIND, never easier to TRUST.
package storage

import (
	"context"
	"encoding/json"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// atomDocumentationDiscoveryFields are the documentation fields that carry
// genuine retrieval meaning, matching the default embedding-input field set
// atomdoc.ComposeDocumentationEmbeddingInput uses (M21-128/131). Fields that
// describe mechanism rather than intent (Determinism, Complexity and limits,
// Verification, and similar) are excluded: they make text longer without
// making the atom easier to recognise.
//
// "Do not use when" is included deliberately. AGENTS.md calls negative
// selection examples load-bearing precisely because they distinguish an atom
// from its nearest wrong neighbour.
var atomDocumentationDiscoveryFields = []string{
	"Purpose",
	"Use when",
	"Do not use when",
	"Semantics",
	"Retrieval concepts",
}

// AtomDocumentationDiscoveryText is one admitted documentation revision's
// retrieval-bearing text, keyed by the atom it documents.
//
// It carries no assurance, maturity, applicability, or evidence field by
// construction, so a caller cannot mistake "this atom is well documented"
// for "this atom is eligible".
type AtomDocumentationDiscoveryText struct {
	RevisionID   string
	AtomID       domain.AtomID
	AtomVersion  uint32
	ContractHash string
	Fields       map[string]string
}

// ListAtomDocumentationDiscoveryTextByProject returns the retrieval-bearing
// documentation text for every ADMITTED atom documentation revision in one
// project. Invalidated revisions are excluded: M21-134 requires a revision
// whose comment, contract, binding, or evidence validity changed to stop
// influencing retrieval immediately, and silently continuing to surface its
// text would defeat that.
func (repositories *Repositories) ListAtomDocumentationDiscoveryTextByProject(
	ctx context.Context,
	projectID domain.ProjectID,
) ([]AtomDocumentationDiscoveryText, error) {
	if projectID.IsZero() {
		return nil, errors.New("project ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT r.id, r.atom_id, r.atom_version_number, r.contract_hash,
		        f.field_label, f.field_kind, f.is_none, f.field_text, f.field_items_json
		 FROM atom_documentation_revisions AS r
		 JOIN atom_documentation_fields AS f ON f.revision_id = r.id
		 WHERE r.project_id = ?
		   AND r.validation_status = 'admitted'
		   AND f.is_none = 0
		 ORDER BY r.atom_id, r.atom_version_number, r.id, f.field_label`,
		projectID,
	)
	if err != nil {
		return nil, classify("list atom documentation discovery text", err)
	}
	defer rows.Close()

	wanted := map[string]bool{}
	for _, label := range atomDocumentationDiscoveryFields {
		wanted[label] = true
	}

	order := []string{}
	byRevision := map[string]*AtomDocumentationDiscoveryText{}
	for rows.Next() {
		var (
			revisionID   string
			atomID       domain.AtomID
			atomVersion  uint32
			contractHash string
			label        string
			kind         string
			isNone       int
			text         *string
			itemsJSON    *string
		)
		if err := rows.Scan(
			&revisionID, &atomID, &atomVersion, &contractHash,
			&label, &kind, &isNone, &text, &itemsJSON,
		); err != nil {
			return nil, classify("scan atom documentation discovery text", err)
		}
		if !wanted[label] {
			continue
		}
		value := atomDocumentationFieldDiscoveryValue(kind, text, itemsJSON)
		if value == "" {
			continue
		}
		entry, seen := byRevision[revisionID]
		if !seen {
			entry = &AtomDocumentationDiscoveryText{
				RevisionID:   revisionID,
				AtomID:       atomID,
				AtomVersion:  atomVersion,
				ContractHash: contractHash,
				Fields:       map[string]string{},
			}
			byRevision[revisionID] = entry
			order = append(order, revisionID)
		}
		entry.Fields[label] = value
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list atom documentation discovery text", err)
	}

	results := make([]AtomDocumentationDiscoveryText, 0, len(order))
	for _, revisionID := range order {
		results = append(results, *byRevision[revisionID])
	}
	return results, nil
}

// atomDocumentationFieldDiscoveryValue flattens one stored field into plain
// searchable text. List items are joined with a space so a phrase spanning
// two items cannot accidentally match as one.
func atomDocumentationFieldDiscoveryValue(kind string, text *string, itemsJSON *string) string {
	if kind == "prose" {
		if text == nil {
			return ""
		}
		return *text
	}
	if itemsJSON == nil {
		return ""
	}
	var items []string
	if err := json.Unmarshal([]byte(*itemsJSON), &items); err != nil {
		return ""
	}
	joined := ""
	for _, item := range items {
		if item == "" {
			continue
		}
		if joined != "" {
			joined += " "
		}
		joined += item
	}
	return joined
}
