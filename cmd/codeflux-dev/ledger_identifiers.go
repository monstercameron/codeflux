package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// REPO-021: reject a duplicate or malformed ledger identifier before it can
// collide across lanes.
//
// CL- and DL- numbers are assigned by hand, in sequence, by whichever lane
// happens to write the next CHANGELOG or DEVLOG entry. Two lanes working the
// same branch concurrently can pick the same next number without either
// being wrong in isolation, and nothing caught it until the collision was
// live in the tracked files. Deriving collision-free identifiers would need
// coordination this repository does not have -- sequential numbers assigned
// by hand, no shared counter -- so refusing a duplicate in lint is the
// simpler option and the one taken here. A malformed identifier is refused
// for the same reason it cannot be left alone: a value that does not match
// CL-YYYYMMDD-NNN / DL-YYYYMMDD-NNN cannot be compared for collision at all,
// so letting it through would silently defeat the check.
var (
	// ledgerEntriesMarker anchors parsing to the "Entries" section of a
	// ledger file. Both CHANGELOG and DEVLOG open with a documentation
	// preamble that includes a literal field-name template -- bare labels
	// such as "Change-ID:" and "Dev-Log:" with no value after the colon --
	// describing the entry schema. Scanning the whole file for identifier
	// lines reads that template as an entry with a blank identifier, which
	// is not an entry at all. Anchoring to the line that starts the real
	// entries, rather than skipping a fixed number of lines, keeps the rule
	// correct as the preamble grows or is reworded.
	ledgerEntriesMarker = regexp.MustCompile(`(?m)^Entries\r?\n-+\r?\n`)

	changeIdentifierLinePattern = regexp.MustCompile(`(?m)^Change-ID:[ \t]*(.*)$`)
	devLogIdentifierLinePattern = regexp.MustCompile(`(?m)^Dev-Log:[ \t]*(.*)$`)
	changeIdentifierShape       = regexp.MustCompile(`^CL-[0-9]{8}-[0-9]{3}$`)
	devLogIdentifierShape       = regexp.MustCompile(`^DL-[0-9]{8}-[0-9]{3}$`)
)

// legacyChangelogDuplicates and legacyDevlogDuplicates record duplicate
// identifiers this repository already released before REPO-021 existed to
// catch them. AGENTS.md forbids rewriting or deleting a released CHANGELOG
// or DEVLOG entry, so these six collisions cannot be repaired by
// renumbering; they are recorded here as an exact, content-addressed
// exemption instead of a blanket one.
//
// "Exact" is the operative word. A rule that merely tolerated "this
// identifier may appear twice" would let a future lane delete one of these
// entries and introduce an unrelated new collision under the same
// identifier without the count ever changing -- a new mistake hiding behind
// a removed one. Each map entry is instead the SHA-256, in hex, of its own
// entry's exact text, taken from its identifier line up to the next
// identifier line of the same field (or end of file). A duplicate is
// accepted only when the full set of occurrences found in the file matches
// this exact set of hashes, at this exact count; a substitute duplicate
// cannot pass by coincidence of counting.
var legacyChangelogDuplicates = map[string][]string{
	"CL-20260802-028": {
		// line 302: commit 6c59976, "The pre-commit hook runs unit tests
		// for the changed packages instead of the whole repository"
		"098d80756e12da361585a6c4699457b186f5c4f175d1a9571be435f32ea60bff",
		// line 327: commit 49061c4, "The engine ladder grows to 250 rungs
		// and is judged by properties of the work..."
		"38d558d0910e9a545b990f36017592c2aef92c5d9f516a0ee635b5081f7aebfb",
	},
}

var legacyDevlogDuplicates = map[string][]string{
	"DL-20260802-004": {
		// line 2106: "The atom inspector shows the code, the notes, the
		// tests, and what the naming grammar makes of the name"
		"9412c34aeea0b84c30596d9ff97b8f393839f277b0ac1cd9ffa84b51b4f1876e",
		// line 2262: "A repository's own code is browsable, with its
		// documented atoms marked" -- this entry's own text records that
		// it also supersedes a separate collision on DL-20260802-002
		"b3e9a5d6d8f98cc79252c024d039ee6c9ba38c133424adbf30078240038f5e65",
	},
	"DL-20260802-012": {
		"4eddb53f46fab4a38bbab9d52980954064cedc6a7933303ccfe8dbbc46d1bf47",
		"2112cb097730ccd9adc4a27f7412ffb7353c840431ac4d53e27f7f9afa9a910c",
	},
	"DL-20260802-014": {
		"7f404cd49725902bb063c084f250c214b846c792f9fc30fa02400f6f9735c221",
		"0c394185bda81540000f4884c77d098538892b3cd41869683452a6f1c47fd727",
	},
	"DL-20260802-015": {
		"3997834e311f64627e345269c6ae265027dbd6484317670573e21927f508e07a",
		"9d07010a5d178b737d40201e0f24f694997babb19ad2dbee114ed7a6e066ff0c",
	},
	"DL-20260802-038": {
		"c3df595c7e690cdc1e73ec773d961ed47cd98dccba6d9c6a2d5a3868517917a7",
		"ee0bfc5bb783926aab640275c26a9cd2eac34064b6e89a1e99802d6ce258a9a7",
	},
}

// checkLedgerIdentifierCollisions parses the tracked CHANGELOG and DEVLOG,
// past their documentation preamble, for their own declared identifiers --
// CHANGELOG's "Change-ID:" field and DEVLOG's "Dev-Log:" field, not the
// cross-referencing field each carries pointing at the other ledger -- and
// refuses a value that is malformed or that repeats an identifier already
// declared earlier in the same file, unless the repeat is exactly one of
// the six pre-existing collisions recorded above.
func checkLedgerIdentifierCollisions(root string) error {
	if err := checkLedgerFileIdentifiers(
		filepath.Join(root, "CHANGELOG"),
		"CHANGELOG",
		changeIdentifierLinePattern,
		changeIdentifierShape,
		"Change-ID",
		"CL-YYYYMMDD-NNN",
		legacyChangelogDuplicates,
	); err != nil {
		return err
	}
	return checkLedgerFileIdentifiers(
		filepath.Join(root, "DEVLOG"),
		"DEVLOG",
		devLogIdentifierLinePattern,
		devLogIdentifierShape,
		"Dev-Log",
		"DL-YYYYMMDD-NNN",
		legacyDevlogDuplicates,
	)
}

type ledgerOccurrence struct {
	identifier string
	line       int
	hash       string
}

func checkLedgerFileIdentifiers(
	path, displayName string,
	linePattern, shapePattern *regexp.Regexp,
	fieldName, wantShape string,
	legacyDuplicates map[string][]string,
) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", displayName, err)
	}
	marker := ledgerEntriesMarker.FindIndex(content)
	if marker == nil {
		return fmt.Errorf(
			"%s: could not find the \"Entries\" section marker that bounds real entries",
			displayName,
		)
	}
	entries := content[marker[1]:]
	entriesOffset := marker[1]

	locs := linePattern.FindAllSubmatchIndex(entries, -1)
	occurrences := make([]ledgerOccurrence, 0, len(locs))
	for index, loc := range locs {
		blockStart := loc[0]
		blockEnd := len(entries)
		if index+1 < len(locs) {
			blockEnd = locs[index+1][0]
		}
		value := strings.TrimSpace(string(entries[loc[2]:loc[3]]))
		lineNumber := 1 + strings.Count(string(content[:entriesOffset+blockStart]), "\n")
		if value == "" {
			return fmt.Errorf("%s:%d: %s field has no identifier", displayName, lineNumber, fieldName)
		}
		if !shapePattern.MatchString(value) {
			return fmt.Errorf(
				"%s:%d: %s %q does not match %s and cannot be checked for collision",
				displayName, lineNumber, fieldName, value, wantShape,
			)
		}
		sum := sha256.Sum256(entries[blockStart:blockEnd])
		occurrences = append(occurrences, ledgerOccurrence{
			identifier: value,
			line:       lineNumber,
			hash:       hex.EncodeToString(sum[:]),
		})
	}

	byIdentifier := make(map[string][]ledgerOccurrence, len(occurrences))
	for _, occurrence := range occurrences {
		byIdentifier[occurrence.identifier] = append(byIdentifier[occurrence.identifier], occurrence)
	}

	identifiers := make([]string, 0, len(byIdentifier))
	for identifier := range byIdentifier {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)

	for _, identifier := range identifiers {
		group := byIdentifier[identifier]
		if len(group) < 2 {
			continue
		}
		if err := checkLegacyDuplicateIsExact(
			displayName, fieldName, identifier, group, legacyDuplicates[identifier],
		); err != nil {
			return err
		}
	}
	return nil
}

// checkLegacyDuplicateIsExact accepts a duplicate identifier only when the
// full set of occurrence hashes found in the file exactly matches the
// recorded legacy set, at the recorded count. Any other repeat -- a third
// occurrence, a changed entry, or an identifier with no legacy record at
// all -- is refused.
func checkLegacyDuplicateIsExact(
	displayName, fieldName, identifier string,
	group []ledgerOccurrence,
	legacyHashes []string,
) error {
	if len(group) != len(legacyHashes) {
		return fmt.Errorf(
			"%s:%d: %s %s is declared %d times; a new duplicate identifier is not allowed",
			displayName, group[len(group)-1].line, fieldName, identifier, len(group),
		)
	}
	remaining := append([]string(nil), legacyHashes...)
	for _, occurrence := range group {
		matchIndex := -1
		for candidateIndex, hash := range remaining {
			if hash == occurrence.hash {
				matchIndex = candidateIndex
				break
			}
		}
		if matchIndex == -1 {
			return fmt.Errorf(
				"%s:%d: %s %s duplicates an existing identifier and does not match the "+
					"exact legacy collision already released under that identifier; a new "+
					"duplicate identifier is not allowed",
				displayName, occurrence.line, fieldName, identifier,
			)
		}
		remaining = append(remaining[:matchIndex], remaining[matchIndex+1:]...)
	}
	return nil
}
