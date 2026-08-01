package dogfood

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// PacketCount is the number of separately revealable requirement packets
// (M24-109).
const PacketCount = 15

// Packet is one requirement, revealed in sequence (M24-109).
//
// Packets are separate objects rather than one document so a run can be shown
// to have had no access to what comes next. A single file with fifteen
// sections would let an agent read all of them and appear to plan brilliantly.
type Packet struct {
	Ordinal int
	Title   string
	// Requirement is the text handed to the agent, verbatim.
	Requirement string
	// AcceptanceSummary is what the hidden suite will check. It is stored with
	// the packet for the evaluator, and never revealed to the agent.
	AcceptanceSummary string
	// DependsOn are the earlier packets whose accepted result this one builds
	// on.
	DependsOn []int
}

// Validate rejects a packet that could not be revealed or judged.
func (packet Packet) Validate() error {
	switch {
	case packet.Ordinal < 1 || packet.Ordinal > PacketCount:
		return fmt.Errorf("packet ordinal %d is outside 1..%d", packet.Ordinal, PacketCount)
	case strings.TrimSpace(packet.Title) == "":
		return fmt.Errorf("packet %d has no title", packet.Ordinal)
	case strings.TrimSpace(packet.Requirement) == "":
		return fmt.Errorf("packet %d has no requirement", packet.Ordinal)
	case strings.TrimSpace(packet.AcceptanceSummary) == "":
		return fmt.Errorf("packet %d has no acceptance summary", packet.Ordinal)
	}
	for _, dependency := range packet.DependsOn {
		if dependency < 1 || dependency >= packet.Ordinal {
			return fmt.Errorf(
				"packet %d depends on %d, which is not an earlier packet",
				packet.Ordinal, dependency)
		}
	}
	return nil
}

// Packets returns the chronological ReserveFlow requirements (M24-109).
//
// The sequence is deliberately ordinary: each packet is the kind of change a
// small team actually makes next, and several of them invalidate an assumption
// an earlier one was allowed to make. That is the point — a system that only
// handles additive work has not been tested against real product development.
func Packets() []Packet {
	return []Packet{
		{
			Ordinal: 1, Title: "Server lifecycle",
			Requirement: "Run an HTTP server with health and readiness endpoints. Give " +
				"every request an identifier, answer in JSON, and return safe errors.",
			AcceptanceSummary: "clean startup and shutdown, deterministic health, safe errors",
		},
		{
			Ordinal: 2, Title: "Resources",
			Requirement: "Persist resources in SQLite with a name, a capacity, stable " +
				"identity, and timestamps. List them with bounded cursor pagination.",
			AcceptanceSummary: "clean migration, survives restart, stable ordering and cursor",
			DependsOn:         []int{1},
		},
		{
			Ordinal: 3, Title: "Reservations",
			Requirement: "Create a pending reservation and decrement the resource's " +
				"capacity, atomically.",
			AcceptanceSummary: "no oversubscription; a failure rolls back completely",
			DependsOn:         []int{2},
		},
		{
			Ordinal: 4, Title: "Idempotency",
			Requirement: "Accept an idempotency key on creation. A repeated canonical " +
				"request replays the original response; the same key with different input " +
				"is a conflict.",
			AcceptanceSummary: "one reservation per key, original response replayed",
			DependsOn:         []int{3},
		},
		{
			Ordinal: 5, Title: "Transitions",
			Requirement: "Confirm and cancel reservations, against an expected version. " +
				"State what a repeated request does.",
			AcceptanceSummary: "stale versions refused; cancelling releases capacity once",
			DependsOn:         []int{3, 4},
		},
		{
			Ordinal: 6, Title: "Concurrency",
			Requirement: "Keep capacity correct when reservations are created and " +
				"cancelled concurrently, including from separate processes.",
			AcceptanceSummary: "no oversubscription, lost update, duplicate, or deadlock",
			DependsOn:         []int{3, 5},
		},
		{
			Ordinal: 7, Title: "Expiration",
			Requirement: "Expire unconfirmed reservations after fifteen minutes, releasing " +
				"capacity exactly once, with bounded scans and clean shutdown.",
			AcceptanceSummary: "exact-once release at the boundary, safe across crash and restart",
			DependsOn:         []int{5, 6},
		},
		{
			Ordinal: 8, Title: "Outbox",
			Requirement: "Record a state transition as an outbox event in the same " +
				"transaction, and poll it in bounded batches.",
			AcceptanceSummary: "one event per transition, none lost or repeated",
			DependsOn:         []int{5},
		},
		{
			Ordinal: 9, Title: "Webhooks",
			Requirement: "Deliver outbox events to a configured endpoint, signed, with a " +
				"stable delivery identity and bounded retry.",
			AcceptanceSummary: "correct retry classification, no duplicate delivery, no secret leak",
			DependsOn:         []int{8},
		},
		{
			Ordinal: 10, Title: "Authorization",
			Requirement: "Require an API key for administrative operations, and state the " +
				"policy for reservation operations.",
			AcceptanceSummary: "each rejection distinguishable; no key material anywhere",
			DependsOn:         []int{1},
		},
		{
			Ordinal: 11, Title: "Observability",
			Requirement: "Emit correlated structured logs and stable error codes, expose " +
				"local metrics, and make readiness reflect real dependencies.",
			AcceptanceSummary: "a request is traceable end to end; no body or secret is logged",
			DependsOn:         []int{1, 9},
		},
		{
			Ordinal: 12, Title: "Contract",
			Requirement: "Publish an OpenAPI description of exactly the implemented " +
				"behaviour, including errors, pagination, idempotency, and concurrency.",
			AcceptanceSummary: "the description and the runtime match in both directions",
			DependsOn:         []int{4, 5, 10},
		},
		{
			Ordinal: 13, Title: "Defect",
			Requirement: "A reported symptom is reproducible from this revision. Find the " +
				"cause and fix it.",
			AcceptanceSummary: "a reproducing test, a behavioural fix, and no regression",
			DependsOn:         []int{6, 7},
		},
		{
			Ordinal: 14, Title: "Rule change",
			Requirement: "The domain rule for capacity has changed. Apply it everywhere " +
				"it reaches.",
			AcceptanceSummary: "every affected surface updated, and stale evidence invalidated",
			DependsOn:         []int{3, 5, 8, 12},
		},
		{
			Ordinal: 15, Title: "Upgrade",
			Requirement: "Upgrade the pinned dependency and add a column, without changing " +
				"the existing API.",
			AcceptanceSummary: "migration applies once, no data lost, API unchanged",
			DependsOn:         []int{2, 12},
		},
	}
}

// ValidatePackets checks the sequence is complete and well ordered.
func ValidatePackets() error {
	packets := Packets()
	if len(packets) != PacketCount {
		return fmt.Errorf("%d packets are declared, want %d", len(packets), PacketCount)
	}
	seen := map[int]bool{}
	for index, packet := range packets {
		if err := packet.Validate(); err != nil {
			return err
		}
		if packet.Ordinal != index+1 {
			return fmt.Errorf("packet %d is out of order", packet.Ordinal)
		}
		if seen[packet.Ordinal] {
			return fmt.Errorf("packet %d appears twice", packet.Ordinal)
		}
		seen[packet.Ordinal] = true
	}
	// A sequence where nothing depends on anything is a set of unrelated
	// exercises, not a chronological product history.
	dependent := 0
	for _, packet := range packets {
		if len(packet.DependsOn) > 0 {
			dependent++
		}
	}
	if dependent < PacketCount/2 {
		return fmt.Errorf(
			"only %d of %d packets build on an earlier one; the sequence is not chronological",
			dependent, PacketCount)
	}
	return nil
}

// ErrFuturePacket reports an attempt to read a requirement not yet revealed.
var ErrFuturePacket = errors.New("a future requirement packet is not accessible")

// Revealer controls what a run can see (M24-110).
//
// The rule is exact: a task working on packet N may read packets 1..N and
// nothing beyond. Access to a future packet would let the agent design for
// requirements it has not been given, which is the single easiest way to make
// a chronological trial produce a flattering result.
type Revealer struct {
	current int
}

// NewRevealer starts before the first packet.
func NewRevealer() *Revealer { return &Revealer{current: 0} }

// Reveal advances to the next packet.
func (revealer *Revealer) Reveal() (Packet, error) {
	if revealer.current >= PacketCount {
		return Packet{}, errors.New("every packet has already been revealed")
	}
	revealer.current++
	return Packets()[revealer.current-1], nil
}

// Current reports the highest revealed ordinal.
func (revealer *Revealer) Current() int { return revealer.current }

// Get returns one packet if it has been revealed (M24-110).
func (revealer *Revealer) Get(ordinal int) (Packet, error) {
	if ordinal < 1 || ordinal > PacketCount {
		return Packet{}, fmt.Errorf("packet %d does not exist", ordinal)
	}
	if ordinal > revealer.current {
		return Packet{}, fmt.Errorf("%w: packet %d (revealed through %d)",
			ErrFuturePacket, ordinal, revealer.current)
	}
	return Packets()[ordinal-1], nil
}

// Accessible returns every packet a run may currently read.
func (revealer *Revealer) Accessible() []Packet {
	if revealer.current == 0 {
		return nil
	}
	return Packets()[:revealer.current]
}

// InterventionKind names one recorded human action (M24-112).
//
// The set is closed so a run cannot describe an intervention in a way that
// avoids counting it. Every kind here is something a person did that the agent
// did not do for itself.
type InterventionKind string

const (
	InterventionClarification InterventionKind = "clarification"
	InterventionApproval      InterventionKind = "approval"
	InterventionRedirect      InterventionKind = "redirect"
	InterventionDenial        InterventionKind = "denial"
	InterventionRollback      InterventionKind = "rollback"
	InterventionManualCommand InterventionKind = "manual-command"
	InterventionManualEdit    InterventionKind = "manual-source-edit"
	InterventionEvaluator     InterventionKind = "evaluator-action"
	InterventionContamination InterventionKind = "contamination-decision"
)

// AllInterventionKinds returns every recordable action.
func AllInterventionKinds() []InterventionKind {
	return []InterventionKind{
		InterventionClarification, InterventionApproval, InterventionRedirect,
		InterventionDenial, InterventionRollback, InterventionManualCommand,
		InterventionManualEdit, InterventionEvaluator, InterventionContamination,
	}
}

// Valid reports whether a kind is declared.
func (kind InterventionKind) Valid() bool {
	return slices.Contains(AllInterventionKinds(), kind)
}

// Contaminating reports whether this kind ends the no-intervention claim
// (M24-113).
//
// A manual source edit contaminates because after one the diff is no longer
// the agent's work. An approval does not: approving is what the product asks
// the user to do, and counting it as contamination would make the claim
// unachievable by design.
func (kind InterventionKind) Contaminating() bool {
	switch kind {
	case InterventionManualEdit, InterventionManualCommand:
		return true
	default:
		return false
	}
}

// Intervention is one recorded action (M24-112).
type Intervention struct {
	At   time.Time
	Task int
	Kind InterventionKind
	// Detail is what was done, in enough words to reconstruct it.
	Detail string
	// Actor is who did it.
	Actor string
}

// Validate rejects an unusable record.
func (intervention Intervention) Validate() error {
	switch {
	case intervention.At.IsZero():
		return errors.New("an intervention requires a time")
	case intervention.Task < 1 || intervention.Task > PacketCount:
		return fmt.Errorf("intervention names task %d, outside 1..%d",
			intervention.Task, PacketCount)
	case !intervention.Kind.Valid():
		return fmt.Errorf("unknown intervention kind %q", intervention.Kind)
	case strings.TrimSpace(intervention.Detail) == "":
		return fmt.Errorf("a %q intervention has no detail", intervention.Kind)
	case strings.TrimSpace(intervention.Actor) == "":
		return fmt.Errorf("a %q intervention has no actor", intervention.Kind)
	}
	return nil
}

// Ledger is the append-only intervention record (M24-112).
//
// Append-only is the property that matters: a ledger that could be edited
// afterwards would let an inconvenient intervention disappear between the run
// and the report.
type Ledger struct {
	entries []Intervention
	sealed  bool
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger { return &Ledger{} }

// Append records one intervention.
func (ledger *Ledger) Append(intervention Intervention) error {
	if err := intervention.Validate(); err != nil {
		return err
	}
	if ledger.sealed {
		return errors.New("the ledger is sealed and accepts no further entries")
	}
	if len(ledger.entries) > 0 {
		last := ledger.entries[len(ledger.entries)-1]
		if intervention.At.Before(last.At) {
			return fmt.Errorf(
				"an intervention at %s follows one at %s; an append-only ledger cannot go backwards",
				intervention.At, last.At)
		}
	}
	ledger.entries = append(ledger.entries, intervention)
	return nil
}

// Seal closes the ledger at the end of a run.
func (ledger *Ledger) Seal() { ledger.sealed = true }

// Entries returns a copy of the record.
func (ledger *Ledger) Entries() []Intervention {
	entries := make([]Intervention, len(ledger.entries))
	copy(entries, ledger.entries)
	return entries
}

// Count returns how many interventions of a kind were recorded.
func (ledger *Ledger) Count(kind InterventionKind) int {
	count := 0
	for _, entry := range ledger.entries {
		if entry.Kind == kind {
			count++
		}
	}
	return count
}

// Contaminated reports whether the no-intervention exit claim is void
// (M24-113).
func (ledger *Ledger) Contaminated() bool {
	for _, entry := range ledger.entries {
		if entry.Kind.Contaminating() {
			return true
		}
	}
	return false
}

// ContaminationReasons names why the claim is void.
func (ledger *Ledger) ContaminationReasons() []string {
	var reasons []string
	for _, entry := range ledger.entries {
		if !entry.Kind.Contaminating() {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("task %d: %s by %s",
			entry.Task, entry.Kind, entry.Actor))
	}
	sort.Strings(reasons)
	return reasons
}

// Digest is the ledger's content identity, so a report can be tied to the
// ledger it was produced from.
func (ledger *Ledger) Digest() string {
	digest := sha256.New()
	for _, entry := range ledger.entries {
		fmt.Fprintf(digest, "%d\x00%s\x00%s\x00%s\x00%s\n",
			entry.Task, entry.Kind, entry.Actor, entry.Detail,
			entry.At.UTC().Format(time.RFC3339Nano))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// Track is one comparison configuration (M24-115).
type Track struct {
	Name string
	// Description says what this track is testing.
	Description string
	// UsesCodeflux records whether CodeFlux drives the work.
	UsesCodeflux bool
	// HumanWrites records whether a person writes the code.
	HumanWrites bool
	// Manifest freezes the conditions.
	Manifest RunManifest
}

// DeclaredTracks returns the comparison tracks (M24-115).
//
// The requirements and the acceptance authority are identical across tracks.
// Only who does the work changes — otherwise the comparison would be between
// two different problems.
func DeclaredTracks() []Track {
	return []Track{
		{
			Name: "A", Description: "CodeFlux drives every task, with approvals only",
			UsesCodeflux: true, HumanWrites: false,
		},
		{
			Name: "B", Description: "a human writes every task, unaided",
			UsesCodeflux: false, HumanWrites: true,
		},
		{
			Name: "C", Description: "a human writes every task with an ordinary chat assistant",
			UsesCodeflux: false, HumanWrites: true,
		},
		{
			Name: "D", Description: "CodeFlux drives with project memory disabled, isolating " +
				"what memory contributes",
			UsesCodeflux: true, HumanWrites: false,
		},
	}
}

// Validate rejects a track that would not be comparable.
func (track Track) Validate() error {
	switch {
	case strings.TrimSpace(track.Name) == "":
		return errors.New("a track requires a name")
	case strings.TrimSpace(track.Description) == "":
		return fmt.Errorf("track %q has no description", track.Name)
	case !track.UsesCodeflux && !track.HumanWrites:
		return fmt.Errorf("track %q has nobody doing the work", track.Name)
	case track.UsesCodeflux && track.HumanWrites:
		return fmt.Errorf(
			"track %q has both CodeFlux and a human writing code, so its result attributes "+
				"to neither", track.Name)
	}
	return nil
}

// ValidateTracks checks the declared set is comparable.
func ValidateTracks() error {
	tracks := DeclaredTracks()
	if len(tracks) < 3 {
		return fmt.Errorf("%d tracks are declared; a comparison needs at least three", len(tracks))
	}
	names := map[string]bool{}
	codefluxTracks := 0
	humanTracks := 0
	for _, track := range tracks {
		if err := track.Validate(); err != nil {
			return err
		}
		if names[track.Name] {
			return fmt.Errorf("track %q is declared twice", track.Name)
		}
		names[track.Name] = true
		if track.UsesCodeflux {
			codefluxTracks++
		} else {
			humanTracks++
		}
	}
	if codefluxTracks == 0 || humanTracks == 0 {
		return errors.New(
			"a comparison needs at least one CodeFlux track and one human track, or it " +
				"compares nothing")
	}
	return nil
}

// AcceptedChain is the one commit chain every track must advance through
// (M24-111).
type AcceptedChain struct {
	// Commits are the accepted states, one per packet, in order.
	Commits []string
}

// Validate rejects a chain that does not describe the full sequence.
func (chain AcceptedChain) Validate() error {
	if len(chain.Commits) != PacketCount {
		return fmt.Errorf("the accepted chain has %d commits for %d packets",
			len(chain.Commits), PacketCount)
	}
	seen := map[string]int{}
	for index, commit := range chain.Commits {
		if err := validateHex(commit, 40, fmt.Sprintf("accepted commit %d", index+1)); err != nil {
			return err
		}
		if previous, duplicate := seen[commit]; duplicate {
			return fmt.Errorf(
				"packets %d and %d accept the same commit, so one of them changed nothing",
				previous+1, index+1)
		}
		seen[commit] = index
	}
	return nil
}

// EquivalentState reports whether a track reached the accepted state for a
// packet (M24-111).
//
// Equivalence is by ordinal, not by commit hash: two tracks legitimately
// produce different code for the same requirement, and demanding identical
// commits would make the comparison impossible rather than rigorous.
func (chain AcceptedChain) EquivalentState(packet int) (string, error) {
	if err := chain.Validate(); err != nil {
		return "", err
	}
	if packet < 1 || packet > PacketCount {
		return "", fmt.Errorf("packet %d does not exist", packet)
	}
	return chain.Commits[packet-1], nil
}
