package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const ledgerPreamble = "TITLE\n\nPurpose\n-------\nSome preamble prose.\n\n" +
	"Entry fields\n------------\nChange-ID:\nCommit:\nDate:\n\nEntries\n-------\n"

func writeLedgerFixture(t *testing.T, root, changelogEntries, devlogEntries string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, "CHANGELOG"), ledgerPreamble+changelogEntries)
	writeTestFile(t, filepath.Join(root, "DEVLOG"), ledgerPreamble+devlogEntries)
}

func TestCheckLedgerIdentifierCollisionsPassesOnCleanFixture(t *testing.T) {
	root := t.TempDir()
	writeLedgerFixture(t, root,
		"Change-ID: CL-20260802-001\nDev-Log: DL-20260802-001\n\n"+
			"Change-ID: CL-20260802-002\nDev-Log: DL-20260802-002\n",
		"Dev-Log: DL-20260802-001\nChange-ID: CL-20260802-001\n\n"+
			"Dev-Log: DL-20260802-002\nChange-ID: CL-20260802-002\n",
	)
	if err := checkLedgerIdentifierCollisions(root); err != nil {
		t.Fatalf("clean ledger fixture rejected: %v", err)
	}
}

// TestCheckLedgerIdentifierCollisionsIgnoresFieldTemplatePreamble is the
// discriminating regression for the "Entries" anchor: both real ledgers
// open with a documentation preamble whose field-name template contains a
// bare "Change-ID:" / "Dev-Log:" label with no value. A rule that scans the
// whole file reads that label as an entry with a blank identifier and fails
// every ledger unconditionally; anchoring to the "Entries" marker must
// still pass a fixture that reproduces the same preamble shape with valid
// entries after it.
func TestCheckLedgerIdentifierCollisionsIgnoresFieldTemplatePreamble(t *testing.T) {
	root := t.TempDir()
	writeLedgerFixture(t, root,
		"Change-ID: CL-20260802-001\n",
		"Dev-Log: DL-20260802-001\n",
	)
	if err := checkLedgerIdentifierCollisions(root); err != nil {
		t.Fatalf("preamble field template was misread as an entry: %v", err)
	}
}

func TestCheckLedgerIdentifierCollisionsFiresOnDuplicateChangeID(t *testing.T) {
	root := t.TempDir()
	writeLedgerFixture(t, root,
		"Change-ID: CL-20260802-001\nType: first\n\nChange-ID: CL-20260802-001\nType: second\n",
		"Dev-Log: DL-20260802-001\n",
	)
	err := checkLedgerIdentifierCollisions(root)
	if err == nil {
		t.Fatal("duplicate Change-ID was accepted")
	}
	if !strings.Contains(err.Error(), "CHANGELOG") || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("duplicate Change-ID error = %v, want CHANGELOG duplicate finding", err)
	}
}

func TestCheckLedgerIdentifierCollisionsFiresOnDuplicateDevLog(t *testing.T) {
	root := t.TempDir()
	writeLedgerFixture(t, root,
		"Change-ID: CL-20260802-001\n",
		"Dev-Log: DL-20260802-001\nStatus: first\n\nDev-Log: DL-20260802-001\nStatus: second\n",
	)
	err := checkLedgerIdentifierCollisions(root)
	if err == nil {
		t.Fatal("duplicate Dev-Log was accepted")
	}
	if !strings.Contains(err.Error(), "DEVLOG") || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("duplicate Dev-Log error = %v, want DEVLOG duplicate finding", err)
	}
}

func TestCheckLedgerIdentifierCollisionsFiresOnMalformedChangeID(t *testing.T) {
	root := t.TempDir()
	writeLedgerFixture(t, root,
		"Change-ID: CL-2026080-001\n",
		"Dev-Log: DL-20260802-001\n",
	)
	err := checkLedgerIdentifierCollisions(root)
	if err == nil {
		t.Fatal("malformed Change-ID was accepted")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("malformed Change-ID error = %v, want shape-mismatch finding", err)
	}
}

func TestCheckLedgerIdentifierCollisionsFiresOnMalformedDevLog(t *testing.T) {
	root := t.TempDir()
	writeLedgerFixture(t, root,
		"Change-ID: CL-20260802-001\n",
		"Dev-Log: DL-20260802-1\n",
	)
	err := checkLedgerIdentifierCollisions(root)
	if err == nil {
		t.Fatal("malformed Dev-Log was accepted")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("malformed Dev-Log error = %v, want shape-mismatch finding", err)
	}
}

// TestCheckLedgerIdentifierCollisionsRejectsSubstituteLegacyDuplicate proves
// the legacy exemption is content-addressed rather than a bare count. A
// fixture that reuses one of this repository's six pre-existing duplicate
// identifiers, but with entry text that does not match either of the two
// hashes recorded for it, must still fail -- exercising exactly the failure
// mode the exact-hash design exists to catch: a new collision hiding behind
// a legacy one because both happen to sum to the same count.
func TestCheckLedgerIdentifierCollisionsRejectsSubstituteLegacyDuplicate(t *testing.T) {
	root := t.TempDir()
	writeLedgerFixture(t, root,
		"Change-ID: CL-20260802-028\nType: not the real first entry\n\n"+
			"Change-ID: CL-20260802-028\nType: not the real second entry either\n",
		"Dev-Log: DL-20260802-001\n",
	)
	err := checkLedgerIdentifierCollisions(root)
	if err == nil {
		t.Fatal("substitute duplicate under a legacy identifier was accepted")
	}
	if !strings.Contains(err.Error(), "CHANGELOG") || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("substitute legacy duplicate error = %v, want CHANGELOG duplicate finding", err)
	}
}

// TestCheckLedgerIdentifierCollisionsPassesOnRealRepositoryLedgers is the
// non-vacuous positive case: the actual tracked CHANGELOG and DEVLOG, which
// carry six pre-existing duplicate identifiers this rule cannot repair
// (AGENTS.md forbids rewriting a released entry), must still pass because
// each one matches its recorded legacy exemption exactly.
func TestCheckLedgerIdentifierCollisionsPassesOnRealRepositoryLedgers(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	if err := checkLedgerIdentifierCollisions(root); err != nil {
		t.Fatalf("real repository ledgers rejected: %v", err)
	}
}
