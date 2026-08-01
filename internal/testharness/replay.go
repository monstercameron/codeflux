package testharness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ReplayFixture is a named, redacted session recording (M22-114).
//
// It is JSON on disk so a failing session can be exported, attached to an
// issue, and replayed by someone who was never near the machine that produced
// it. Redaction is asserted at load rather than trusted, because an export
// nobody checked is the one that carries a credential into a bug report.
type ReplayFixture struct {
	Name     string         `json:"name"`
	Redacted bool           `json:"redacted"`
	Events   []ReplayEvent  `json:"events"`
	Snapshot ReplaySnapshot `json:"snapshot"`
}

// ReplayEvent is one recorded event in a fixture.
type ReplayEvent struct {
	Sequence uint64          `json:"sequence"`
	Kind     string          `json:"kind"`
	Revision uint64          `json:"revision"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// ReplaySnapshot is the authoritative state a replay starts from.
type ReplaySnapshot struct {
	ThroughSequence uint64 `json:"through_sequence"`
	TaskState       string `json:"task_state"`
	TaskRevision    uint64 `json:"task_revision"`
}

// Validate rejects a fixture that could not be replayed or that is not safe to
// share.
func (fixture ReplayFixture) Validate() error {
	if strings.TrimSpace(fixture.Name) == "" {
		return errors.New("a replay fixture requires a name")
	}
	if !fixture.Redacted {
		return fmt.Errorf(
			"replay fixture %q is not marked redacted; an unredacted session must never be "+
				"stored as a fixture", fixture.Name)
	}
	if len(fixture.Events) == 0 {
		return fmt.Errorf("replay fixture %q has no events", fixture.Name)
	}
	expected := fixture.Snapshot.ThroughSequence + 1
	for index, event := range fixture.Events {
		if strings.TrimSpace(event.Kind) == "" {
			return fmt.Errorf("replay fixture %q event %d has no kind", fixture.Name, index)
		}
		if event.Sequence != expected {
			return fmt.Errorf(
				"replay fixture %q event %d has sequence %d, expected %d: a fixture with a gap "+
					"cannot distinguish a replay bug from a bad recording",
				fixture.Name, index, event.Sequence, expected)
		}
		expected++
	}
	return nil
}

// LoadReplayFixture reads and validates a fixture from disk (M22-114).
//
// forbidden is the secret material that must not appear anywhere in the file.
// Checking the raw bytes rather than the parsed structure is deliberate: a
// secret hiding in an unrecognised field would survive a structural check.
func LoadReplayFixture(path string, forbidden []string) (ReplayFixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReplayFixture{}, fmt.Errorf("read replay fixture: %w", err)
	}
	for _, secret := range forbidden {
		if secret != "" && strings.Contains(string(raw), secret) {
			return ReplayFixture{}, fmt.Errorf(
				"replay fixture %s contains credential material and must not be used", path)
		}
	}
	var fixture ReplayFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		return ReplayFixture{}, fmt.Errorf("parse replay fixture: %w", err)
	}
	if err := fixture.Validate(); err != nil {
		return ReplayFixture{}, err
	}
	return fixture, nil
}

// SaveReplayFixture writes a validated fixture.
func SaveReplayFixture(path string, fixture ReplayFixture) error {
	if err := fixture.Validate(); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return fmt.Errorf("encode replay fixture: %w", err)
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o600)
}

// ReplayControls govern how a replay is driven (M22-115).
//
// Each control exists to reproduce one real transport condition. Together they
// cover the ways a client's view can diverge from the server's without either
// side crashing, which is the divergence hardest to find any other way.
type ReplayControls struct {
	// StopAtSequence halts after delivering this sequence. Zero replays all.
	StopAtSequence uint64
	// StepEvent delivers one event per Step call instead of running to
	// completion, so a test can inspect state between events.
	StepEvent bool
	// DuplicateSequences are delivered twice. A correct consumer applies each
	// once.
	DuplicateSequences []uint64
	// GapSequences are withheld. A correct consumer detects the gap and asks
	// for repair rather than applying the events after it.
	GapSequences []uint64
	// ReconnectAfterSequence simulates transport loss and re-subscription
	// after this sequence.
	ReconnectAfterSequence uint64
	// SnapshotRepairAtSequence forces a snapshot repair at this point, which
	// is what a consumer must request after a gap.
	SnapshotRepairAtSequence uint64
}

// Validate rejects controls that contradict each other.
func (controls ReplayControls) Validate(fixture ReplayFixture) error {
	highest := fixture.Snapshot.ThroughSequence
	if len(fixture.Events) > 0 {
		highest = fixture.Events[len(fixture.Events)-1].Sequence
	}
	check := func(label string, sequence uint64) error {
		if sequence == 0 {
			return nil
		}
		if sequence <= fixture.Snapshot.ThroughSequence || sequence > highest {
			return fmt.Errorf("%s sequence %d is outside the fixture's range (%d..%d]",
				label, sequence, fixture.Snapshot.ThroughSequence, highest)
		}
		return nil
	}
	if err := check("stop-at", controls.StopAtSequence); err != nil {
		return err
	}
	if err := check("reconnect-after", controls.ReconnectAfterSequence); err != nil {
		return err
	}
	if err := check("snapshot-repair-at", controls.SnapshotRepairAtSequence); err != nil {
		return err
	}
	for _, sequence := range controls.DuplicateSequences {
		if err := check("duplicate", sequence); err != nil {
			return err
		}
	}
	for _, sequence := range controls.GapSequences {
		if err := check("gap", sequence); err != nil {
			return err
		}
		if contains(controls.DuplicateSequences, sequence) {
			return fmt.Errorf(
				"sequence %d is both withheld and duplicated; a fixture cannot describe both",
				sequence)
		}
	}
	return nil
}

// Delivery is one thing that happened during a replay.
type Delivery struct {
	Sequence    uint64
	Kind        string
	Duplicate   bool
	Withheld    bool
	Reconnected bool
	RepairAsked bool
	Applied     bool
}

// ReplayResult is what a replay produced.
type ReplayResult struct {
	Deliveries []Delivery
	// AppliedSequences is the ordered set the consumer actually applied.
	AppliedSequences []uint64
	// GapDetectedAt is the first sequence the consumer refused to apply
	// because of a gap. Zero means no gap was detected.
	GapDetectedAt uint64
	// RepairsRequested counts snapshot repairs the consumer asked for.
	RepairsRequested int
	StoppedEarly     bool
}

// Consumer is the client under replay. It is an interface so the same replay
// can drive the real reducer, a projection comparison, or a graph rebuild.
type Consumer interface {
	// Apply consumes one event and reports whether it was applied. Returning
	// false for an already-seen sequence is correct deduplication; returning
	// an error for a gap is correct refusal.
	Apply(event ReplayEvent) (bool, error)
	// RequestSnapshotRepair is called when the replay forces a repair.
	RequestSnapshotRepair(throughSequence uint64) error
	// Reconnect is called when transport is re-established.
	Reconnect() error
}

// Replay drives a fixture through a consumer under the given controls
// (M22-114, M22-115).
func Replay(
	fixture ReplayFixture,
	controls ReplayControls,
	consumer Consumer,
) (ReplayResult, error) {
	if err := fixture.Validate(); err != nil {
		return ReplayResult{}, err
	}
	if err := controls.Validate(fixture); err != nil {
		return ReplayResult{}, err
	}
	if consumer == nil {
		return ReplayResult{}, errors.New("a replay requires a consumer")
	}
	result := ReplayResult{}

	for _, event := range fixture.Events {
		if contains(controls.GapSequences, event.Sequence) {
			result.Deliveries = append(result.Deliveries, Delivery{
				Sequence: event.Sequence, Kind: event.Kind, Withheld: true,
			})
			continue
		}

		deliveries := 1
		if contains(controls.DuplicateSequences, event.Sequence) {
			deliveries = 2
		}
		for attempt := range deliveries {
			applied, err := consumer.Apply(event)
			delivery := Delivery{
				Sequence: event.Sequence, Kind: event.Kind,
				Duplicate: attempt > 0, Applied: applied,
			}
			if err != nil {
				// A refusal is the correct response to a gap. It is recorded
				// rather than treated as a replay failure.
				delivery.Applied = false
				result.Deliveries = append(result.Deliveries, delivery)
				if result.GapDetectedAt == 0 {
					result.GapDetectedAt = event.Sequence
				}
				break
			}
			result.Deliveries = append(result.Deliveries, delivery)
			if applied && attempt == 0 {
				result.AppliedSequences = append(result.AppliedSequences, event.Sequence)
			}
		}

		if controls.SnapshotRepairAtSequence == event.Sequence {
			if err := consumer.RequestSnapshotRepair(event.Sequence); err != nil {
				return result, fmt.Errorf("snapshot repair at %d: %w", event.Sequence, err)
			}
			result.RepairsRequested++
			result.Deliveries = append(result.Deliveries, Delivery{
				Sequence: event.Sequence, Kind: event.Kind, RepairAsked: true,
			})
		}
		if controls.ReconnectAfterSequence == event.Sequence {
			if err := consumer.Reconnect(); err != nil {
				return result, fmt.Errorf("reconnect after %d: %w", event.Sequence, err)
			}
			result.Deliveries = append(result.Deliveries, Delivery{
				Sequence: event.Sequence, Kind: event.Kind, Reconnected: true,
			})
		}
		if controls.StopAtSequence == event.Sequence {
			result.StoppedEarly = true
			break
		}
		if controls.StepEvent {
			// Stepping returns after each event so the caller can inspect
			// state; the caller resumes by calling Replay with a higher
			// snapshot. Recording it here keeps the mode observable.
			result.StoppedEarly = true
			break
		}
	}
	return result, nil
}

// Projection is a side's view of the world, reduced to comparable facts
// (M22-116).
//
// It is deliberately a flat map of named values rather than a typed struct:
// the server and the client build their views from different code, and a
// shared struct would let a shared bug agree with itself.
type Projection struct {
	Side   string
	Values map[string]string
}

// ProjectionDifference is one disagreement.
type ProjectionDifference struct {
	Key    string
	Server string
	Client string
}

// CompareProjections reports every disagreement between two views (M22-116).
//
// A key present on one side and absent on the other is a difference, not a
// match against empty: silently treating absence as agreement is how a
// client's missing field passes a comparison.
func CompareProjections(server, client Projection) ([]ProjectionDifference, error) {
	if server.Side == "" || client.Side == "" {
		return nil, errors.New("both projections must name their side")
	}
	if server.Side == client.Side {
		return nil, fmt.Errorf("both projections claim to be %q", server.Side)
	}
	keys := map[string]bool{}
	for key := range server.Values {
		keys[key] = true
	}
	for key := range client.Values {
		keys[key] = true
	}
	var differences []ProjectionDifference
	for key := range keys {
		serverValue, serverHas := server.Values[key]
		clientValue, clientHas := client.Values[key]
		if !serverHas {
			serverValue = "(absent)"
		}
		if !clientHas {
			clientValue = "(absent)"
		}
		if serverValue != clientValue {
			differences = append(differences, ProjectionDifference{
				Key: key, Server: serverValue, Client: clientValue,
			})
		}
	}
	sort.Slice(differences, func(left, right int) bool {
		return differences[left].Key < differences[right].Key
	})
	return differences, nil
}

// GraphRevisionDigest is a comparable summary of one rebuilt graph revision
// (M22-117).
type GraphRevisionDigest struct {
	Ordinal   uint64
	NodeIDs   []string
	EdgeKeys  []string
	NodeCount int
	EdgeCount int
}

// NewGraphRevisionDigest normalises node and edge identity so two rebuilds are
// comparable regardless of the order they were produced in.
func NewGraphRevisionDigest(ordinal uint64, nodeIDs, edgeKeys []string) GraphRevisionDigest {
	nodes := append([]string(nil), nodeIDs...)
	edges := append([]string(nil), edgeKeys...)
	sort.Strings(nodes)
	sort.Strings(edges)
	return GraphRevisionDigest{
		Ordinal: ordinal, NodeIDs: nodes, EdgeKeys: edges,
		NodeCount: len(nodes), EdgeCount: len(edges),
	}
}

// CompareGraphRevisions reports whether a rebuilt revision matches the
// original (M22-117).
//
// The graph is a projection of events, so replaying the same events must
// rebuild the same graph. A mismatch means the projection depends on something
// outside the event stream, which makes the graph unexplainable.
func CompareGraphRevisions(original, rebuilt GraphRevisionDigest) error {
	if original.Ordinal != rebuilt.Ordinal {
		return fmt.Errorf("rebuilt revision ordinal %d does not match %d",
			rebuilt.Ordinal, original.Ordinal)
	}
	if original.NodeCount != rebuilt.NodeCount {
		return fmt.Errorf("rebuilt revision has %d nodes, original had %d",
			rebuilt.NodeCount, original.NodeCount)
	}
	if original.EdgeCount != rebuilt.EdgeCount {
		return fmt.Errorf("rebuilt revision has %d edges, original had %d",
			rebuilt.EdgeCount, original.EdgeCount)
	}
	for index := range original.NodeIDs {
		if original.NodeIDs[index] != rebuilt.NodeIDs[index] {
			return fmt.Errorf("rebuilt node %d is %q, original was %q",
				index, rebuilt.NodeIDs[index], original.NodeIDs[index])
		}
	}
	for index := range original.EdgeKeys {
		if original.EdgeKeys[index] != rebuilt.EdgeKeys[index] {
			return fmt.Errorf("rebuilt edge %d is %q, original was %q",
				index, rebuilt.EdgeKeys[index], original.EdgeKeys[index])
		}
	}
	return nil
}

func contains(values []uint64, candidate uint64) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
