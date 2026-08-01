package doctor

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SectionID names one part of a diagnostic export (M23-037, M23-038).
type SectionID string

const (
	SectionVersions     SectionID = "versions"
	SectionSettings     SectionID = "non-secret-settings"
	SectionHealth       SectionID = "health-results"
	SectionLogs         SectionID = "redacted-logs"
	SectionTaskMetadata SectionID = "task-metadata"

	// The sections below are OFF by default (M23-039). Each carries content
	// that could identify a person or reveal their work, so including one is a
	// decision the user makes per export, not a default they inherit.
	SectionPrompts     SectionID = "prompts"
	SectionSourceDiffs SectionID = "source-diffs"
	SectionTaskContent SectionID = "task-content"
)

// DefaultSections are included unless removed (M23-038).
func DefaultSections() []SectionID {
	return []SectionID{
		SectionVersions, SectionSettings, SectionHealth,
		SectionLogs, SectionTaskMetadata,
	}
}

// SensitiveSections are excluded unless explicitly confirmed (M23-039,
// M23-041).
func SensitiveSections() []SectionID {
	return []SectionID{SectionPrompts, SectionSourceDiffs, SectionTaskContent}
}

// AllSections returns every section.
func AllSections() []SectionID {
	return append(append([]SectionID{}, DefaultSections()...), SensitiveSections()...)
}

// Sensitive reports whether a section needs explicit confirmation.
func (section SectionID) Sensitive() bool {
	for _, candidate := range SensitiveSections() {
		if candidate == section {
			return true
		}
	}
	return false
}

// Describe explains what a section contains and, for a sensitive one, what
// including it reveals.
//
// The description is what a user reads before confirming, so for the sensitive
// sections it says the uncomfortable part plainly rather than softening it.
func (section SectionID) Describe() string {
	switch section {
	case SectionVersions:
		return "application, schema, and toolchain versions"
	case SectionSettings:
		return "configuration values that are not secrets: policy preset, limits, paths' shapes"
	case SectionHealth:
		return "the result of each doctor check, with its remediation"
	case SectionLogs:
		return "recent log lines, already passed through redaction"
	case SectionTaskMetadata:
		return "task counts, states, timings, and costs — no requirement or output text"
	case SectionPrompts:
		return "the full text sent to and received from the model, INCLUDING your requirements " +
			"and any repository content that was in context"
	case SectionSourceDiffs:
		return "the actual changes made to your files, INCLUDING their contents"
	case SectionTaskContent:
		return "requirement text, plan text, and command output as written"
	default:
		return ""
	}
}

// Manifest is the M23-037 declaration of what an export will contain.
//
// It is produced and shown BEFORE anything is written, because a user cannot
// meaningfully consent to a bundle they have not seen the shape of.
type Manifest struct {
	Sections []SectionEntry
	// TotalEstimatedBytes is the whole bundle's expected size (M23-040).
	TotalEstimatedBytes int64
	// ConfirmedSensitive lists the sensitive sections the user explicitly
	// approved (M23-041).
	ConfirmedSensitive []SectionID
}

// SectionEntry is one section in a manifest.
type SectionEntry struct {
	ID SectionID
	// Included is whether this section will be written.
	Included bool
	// Description is what it contains.
	Description string
	// EstimatedBytes is its expected size (M23-040).
	EstimatedBytes int64
	// ItemCount is how many records it holds, so a user can tell "one log
	// line" from "forty thousand".
	ItemCount int
	// RequiresConfirmation is whether including it needs an explicit yes.
	RequiresConfirmation bool
}

// SizeEstimator reports the size and item count of one section.
type SizeEstimator func(SectionID) (bytes int64, items int, err error)

// BuildManifest produces the export declaration (M23-037, M23-039, M23-040).
//
// confirmed is the set of sensitive sections the user explicitly approved. A
// sensitive section not in that set is excluded, and its entry still appears
// in the manifest marked not-included, so a user can see what was left out
// rather than having to know to ask.
func BuildManifest(estimate SizeEstimator, confirmed []SectionID) (Manifest, error) {
	if estimate == nil {
		return Manifest{}, errors.New("a manifest requires a size estimator")
	}
	approved := map[SectionID]bool{}
	for _, section := range confirmed {
		if !section.Sensitive() {
			return Manifest{}, fmt.Errorf(
				"section %q is not sensitive and does not need confirmation", section)
		}
		approved[section] = true
	}

	manifest := Manifest{}
	for _, section := range AllSections() {
		included := !section.Sensitive() || approved[section]
		entry := SectionEntry{
			ID: section, Included: included,
			Description:          section.Describe(),
			RequiresConfirmation: section.Sensitive(),
		}
		if entry.Description == "" {
			return Manifest{}, fmt.Errorf("section %q has no description", section)
		}
		if included {
			bytes, items, err := estimate(section)
			if err != nil {
				return Manifest{}, fmt.Errorf("estimate %q: %w", section, err)
			}
			if bytes < 0 || items < 0 {
				return Manifest{}, fmt.Errorf("section %q estimated a negative size", section)
			}
			entry.EstimatedBytes = bytes
			entry.ItemCount = items
			manifest.TotalEstimatedBytes += bytes
		}
		manifest.Sections = append(manifest.Sections, entry)
	}
	for _, section := range SensitiveSections() {
		if approved[section] {
			manifest.ConfirmedSensitive = append(manifest.ConfirmedSensitive, section)
		}
	}
	sort.Slice(manifest.ConfirmedSensitive, func(left, right int) bool {
		return manifest.ConfirmedSensitive[left] < manifest.ConfirmedSensitive[right]
	})
	return manifest, nil
}

// IncludedSections returns what will actually be written.
func (manifest Manifest) IncludedSections() []SectionID {
	var included []SectionID
	for _, entry := range manifest.Sections {
		if entry.Included {
			included = append(included, entry.ID)
		}
	}
	return included
}

// ExcludedSections returns what will not be written, which is the half a user
// most needs to see before deciding the export is enough.
func (manifest Manifest) ExcludedSections() []SectionID {
	var excluded []SectionID
	for _, entry := range manifest.Sections {
		if !entry.Included {
			excluded = append(excluded, entry.ID)
		}
	}
	return excluded
}

// Preview renders the manifest for a person to read before confirming
// (M23-040).
func (manifest Manifest) Preview() []string {
	lines := make([]string, 0, len(manifest.Sections)+4)
	lines = append(lines, "This export will contain:")
	for _, entry := range manifest.Sections {
		if !entry.Included {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %-20s %6s  %d item(s)  %s",
			entry.ID, humanBytes(entry.EstimatedBytes), entry.ItemCount, entry.Description))
	}
	excluded := manifest.ExcludedSections()
	if len(excluded) > 0 {
		lines = append(lines, "", "It will NOT contain:")
		for _, entry := range manifest.Sections {
			if entry.Included {
				continue
			}
			lines = append(lines, fmt.Sprintf("  %-20s %s", entry.ID, entry.Description))
		}
	}
	lines = append(lines, "",
		fmt.Sprintf("Estimated total: %s", humanBytes(manifest.TotalEstimatedBytes)))
	if len(manifest.ConfirmedSensitive) > 0 {
		names := make([]string, 0, len(manifest.ConfirmedSensitive))
		for _, section := range manifest.ConfirmedSensitive {
			names = append(names, string(section))
		}
		lines = append(lines, fmt.Sprintf(
			"You confirmed including sensitive content: %s", strings.Join(names, ", ")))
	}
	return lines
}

// ErrConfirmationRequired reports an attempt to include sensitive content
// without an explicit yes (M23-041).
var ErrConfirmationRequired = errors.New("including sensitive content requires explicit confirmation")

// AuthorizeSensitive checks a requested sensitive section was confirmed.
//
// The confirmation is per-section and per-export: a user who once agreed to
// include diffs has not agreed to include prompts, and has not agreed forever.
func AuthorizeSensitive(requested []SectionID, confirmed []SectionID) error {
	approved := map[SectionID]bool{}
	for _, section := range confirmed {
		approved[section] = true
	}
	var missing []string
	for _, section := range requested {
		if !section.Sensitive() {
			continue
		}
		if !approved[section] {
			missing = append(missing, string(section))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("%w: %s", ErrConfirmationRequired, strings.Join(missing, ", "))
}

// ScanForSeededSecrets is M23-042.
//
// It scans the rendered bundle for every seeded secret and reports which
// section carried it. Reporting the section rather than the offset is
// deliberate: the fix is always "stop including that section's content", never
// "edit byte 4,192".
func ScanForSeededSecrets(rendered map[SectionID]string, seeded []string) ([]string, error) {
	if len(seeded) == 0 {
		return nil, errors.New(
			"no seeded secrets were supplied, so the scan would pass regardless of the bundle")
	}
	var findings []string
	for section, content := range rendered {
		for _, secret := range seeded {
			if strings.TrimSpace(secret) == "" {
				continue
			}
			if strings.Contains(content, secret) {
				// The finding names the section and the length, never the
				// material: a leak report that quoted it would leak it again.
				findings = append(findings, fmt.Sprintf(
					"section %s carries seeded credential material (%d bytes)",
					section, len(secret)))
			}
		}
	}
	sort.Strings(findings)
	return findings, nil
}

func humanBytes(value int64) string {
	switch {
	case value >= 1<<20:
		return fmt.Sprintf("%.1fMiB", float64(value)/float64(1<<20))
	case value >= 1<<10:
		return fmt.Sprintf("%.1fKiB", float64(value)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", value)
	}
}
