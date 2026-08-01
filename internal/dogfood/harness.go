package dogfood

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// FixtureEpoch anchors every evaluator-controlled clock (M24-116).
//
// A fixed epoch is what makes an expiration boundary testable: "fifteen
// minutes from now" cannot be asserted, while "fifteen minutes from the epoch"
// can be, exactly, on both sides.
var FixtureEpoch = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

// EvaluatorClock is a clock the evaluator advances by hand (M24-116).
//
// Nothing in a test may sleep. A test that waited for a real fifteen minutes
// would be untestable, and one that waited for a shortened interval would be
// testing a different system.
type EvaluatorClock struct {
	mutex sync.Mutex
	now   time.Time
}

// NewEvaluatorClock starts at the epoch.
func NewEvaluatorClock() *EvaluatorClock {
	return &EvaluatorClock{now: FixtureEpoch}
}

// Now returns the current fixture time without advancing it.
func (clock *EvaluatorClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

// Advance moves the clock forward.
func (clock *EvaluatorClock) Advance(duration time.Duration) error {
	if duration < 0 {
		return errors.New("an evaluator clock never moves backwards")
	}
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
	return nil
}

// AdvanceTo moves the clock to an exact instant.
func (clock *EvaluatorClock) AdvanceTo(instant time.Time) error {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	if instant.Before(clock.now) {
		return fmt.Errorf("cannot move the clock back from %s to %s", clock.now, instant)
	}
	clock.now = instant
	return nil
}

// ExpirationWindow is the reservation expiry the trial specifies.
const ExpirationWindow = 15 * time.Minute

// BoundaryInstants returns the three instants an expiry test must check
// (M24-116).
//
// Just before, exactly at, and just after. Testing only "well after" would
// pass against an implementation that expires early, and testing only "well
// before" would pass against one that never expires at all.
func BoundaryInstants(created time.Time) (before, at, after time.Time) {
	deadline := created.Add(ExpirationWindow)
	return deadline.Add(-time.Nanosecond), deadline, deadline.Add(time.Nanosecond)
}

// IdentityKind names one thing the evaluator supplies a stable identity for
// (M24-117).
type IdentityKind string

const (
	IdentityResource    IdentityKind = "resource"
	IdentityReservation IdentityKind = "reservation"
	IdentityOutboxEvent IdentityKind = "outbox-event"
	IdentityDelivery    IdentityKind = "delivery"
)

// AllIdentityKinds returns every kind the harness controls.
func AllIdentityKinds() []IdentityKind {
	return []IdentityKind{
		IdentityResource, IdentityReservation, IdentityOutboxEvent, IdentityDelivery,
	}
}

// IdentityFixture issues stable, ordered identities (M24-117).
//
// Stable identities are what let an evaluator assert "this exact delivery was
// attempted twice" rather than "two deliveries happened", which is the whole
// difference between detecting a duplicate and counting one.
type IdentityFixture struct {
	mutex    sync.Mutex
	counters map[IdentityKind]int
}

// NewIdentityFixture returns a fixture with every counter at zero.
func NewIdentityFixture() *IdentityFixture {
	return &IdentityFixture{counters: map[IdentityKind]int{}}
}

// Next issues the next identity of a kind.
func (fixture *IdentityFixture) Next(kind IdentityKind) (string, error) {
	if !slices.Contains(AllIdentityKinds(), kind) {
		return "", fmt.Errorf("unknown identity kind %q", kind)
	}
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	fixture.counters[kind]++
	return fmt.Sprintf("%s-%04d", kind, fixture.counters[kind]), nil
}

// Issued reports how many identities of a kind were handed out.
func (fixture *IdentityFixture) Issued(kind IdentityKind) int {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	return fixture.counters[kind]
}

// AmbiguityMode is one webhook receiver behaviour (M24-119).
//
// Each is a real failure a receiver exhibits, and each demands a different
// correct response. The dangerous one is AcceptedThenTimeout: the receiver
// acted, and the sender cannot know it.
type AmbiguityMode string

const (
	ModeAccepted            AmbiguityMode = "accepted"
	ModeAcceptedThenTimeout AmbiguityMode = "accepted-then-timeout"
	ModeConnectionRefused   AmbiguityMode = "connection-refused"
	ModeSlowResponse        AmbiguityMode = "slow-response"
	ModeTerminal4xx         AmbiguityMode = "terminal-4xx"
	ModeRetryable5xx        AmbiguityMode = "retryable-5xx"
	ModeDuplicateReceipt    AmbiguityMode = "duplicate-receipt"
)

// AllAmbiguityModes returns every declared receiver behaviour.
func AllAmbiguityModes() []AmbiguityMode {
	return []AmbiguityMode{
		ModeAccepted, ModeAcceptedThenTimeout, ModeConnectionRefused,
		ModeSlowResponse, ModeTerminal4xx, ModeRetryable5xx, ModeDuplicateReceipt,
	}
}

// Retryable reports whether a sender may retry after this outcome.
func (mode AmbiguityMode) Retryable() bool {
	switch mode {
	case ModeConnectionRefused, ModeRetryable5xx, ModeSlowResponse:
		return true
	case ModeTerminal4xx, ModeAccepted, ModeDuplicateReceipt:
		return false
	case ModeAcceptedThenTimeout:
		// Not retryable: the receiver accepted it. Retrying would deliver
		// twice, and the sender has no way to find out which happened.
		return false
	default:
		return false
	}
}

// Ambiguous reports whether the sender can determine what happened.
func (mode AmbiguityMode) Ambiguous() bool {
	return mode == ModeAcceptedThenTimeout || mode == ModeSlowResponse
}

// Receipt is one delivery the mock receiver observed (M24-118).
type Receipt struct {
	DeliveryID  string
	Signature   string
	Headers     map[string]string
	PayloadHash string
	At          time.Time
	Mode        AmbiguityMode
}

// MockReceiver records deliveries without revealing its assertions (M24-118).
//
// The receiver records; the evaluator asserts. Keeping the assertions out of
// the receiver is what stops the system under test learning what it will be
// judged on from the thing it is talking to.
type MockReceiver struct {
	mutex    sync.Mutex
	receipts []Receipt
	mode     AmbiguityMode
	clock    *EvaluatorClock
}

// NewMockReceiver returns a receiver in the given mode.
func NewMockReceiver(mode AmbiguityMode, clock *EvaluatorClock) (*MockReceiver, error) {
	if !slices.Contains(AllAmbiguityModes(), mode) {
		return nil, fmt.Errorf("unknown ambiguity mode %q", mode)
	}
	if clock == nil {
		return nil, errors.New("a mock receiver requires an evaluator clock")
	}
	return &MockReceiver{mode: mode, clock: clock}, nil
}

// Deliver records one delivery and returns the scripted outcome.
func (receiver *MockReceiver) Deliver(
	deliveryID, signature, payloadHash string,
	headers map[string]string,
) (AmbiguityMode, error) {
	if strings.TrimSpace(deliveryID) == "" {
		return "", errors.New("a delivery requires an identity, or duplication cannot be detected")
	}
	receiver.mutex.Lock()
	defer receiver.mutex.Unlock()
	copied := make(map[string]string, len(headers))
	for name, value := range headers {
		copied[name] = value
	}
	receiver.receipts = append(receiver.receipts, Receipt{
		DeliveryID: deliveryID, Signature: signature, Headers: copied,
		PayloadHash: payloadHash, At: receiver.clock.Now(), Mode: receiver.mode,
	})
	return receiver.mode, nil
}

// Receipts returns everything observed.
func (receiver *MockReceiver) Receipts() []Receipt {
	receiver.mutex.Lock()
	defer receiver.mutex.Unlock()
	receipts := make([]Receipt, len(receiver.receipts))
	copy(receipts, receiver.receipts)
	return receipts
}

// DuplicateDeliveries returns identities delivered more than once.
//
// The required answer for every mode except a deliberate duplicate-receipt
// test is an empty slice.
func (receiver *MockReceiver) DuplicateDeliveries() []string {
	receiver.mutex.Lock()
	defer receiver.mutex.Unlock()
	counts := map[string]int{}
	for _, receipt := range receiver.receipts {
		counts[receipt.DeliveryID]++
	}
	var duplicates []string
	for identity, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, identity)
		}
	}
	sort.Strings(duplicates)
	return duplicates
}

// CrashPoint names a place the harness can kill the process (M24-122).
//
// Each pair straddles a commit: before it, nothing durable happened; after it,
// something did. The gap between them is where every duplicate-effect bug
// lives.
type CrashPoint string

const (
	CrashBeforeReservationCommit CrashPoint = "before-reservation-commit"
	CrashAfterReservationCommit  CrashPoint = "after-reservation-commit"
	CrashBeforeExpirySelection   CrashPoint = "before-expiration-selection"
	CrashAfterExpiryCommit       CrashPoint = "after-expiration-commit"
	CrashBeforeOutboxClaim       CrashPoint = "before-outbox-claim"
	CrashAfterOutboxClaim        CrashPoint = "after-outbox-claim"
	CrashBeforeReceiverAccept    CrashPoint = "before-receiver-acceptance"
	CrashAfterReceiverAccept     CrashPoint = "after-receiver-acceptance"
	CrashBeforeDeliveryCommit    CrashPoint = "before-delivery-state-commit"
	CrashAfterDeliveryCommit     CrashPoint = "after-delivery-state-commit"
	CrashBeforeMigrationCommit   CrashPoint = "before-migration-commit"
	CrashAfterMigrationCommit    CrashPoint = "after-migration-commit"
)

// AllCrashPoints returns every declared crash point.
func AllCrashPoints() []CrashPoint {
	return []CrashPoint{
		CrashBeforeReservationCommit, CrashAfterReservationCommit,
		CrashBeforeExpirySelection, CrashAfterExpiryCommit,
		CrashBeforeOutboxClaim, CrashAfterOutboxClaim,
		CrashBeforeReceiverAccept, CrashAfterReceiverAccept,
		CrashBeforeDeliveryCommit, CrashAfterDeliveryCommit,
		CrashBeforeMigrationCommit, CrashAfterMigrationCommit,
	}
}

// DurableBefore reports whether anything was committed before this point.
func (point CrashPoint) DurableBefore() bool {
	return strings.HasPrefix(string(point), "after-")
}

// ConcurrencyRace names one race the drivers exercise (M24-120, M24-121).
type ConcurrencyRace string

const (
	RaceSameResource    ConcurrencyRace = "same-resource"
	RaceSameIdempotency ConcurrencyRace = "same-idempotency-key"
	RaceSQLiteLock      ConcurrencyRace = "sqlite-lock"
	RaceStaleVersion    ConcurrencyRace = "stale-version"
	RaceWorkerOwnership ConcurrencyRace = "worker-ownership"
	RaceShutdown        ConcurrencyRace = "shutdown"
)

// InProcessRaces are the races a single-process driver can exercise
// (M24-120).
func InProcessRaces() []ConcurrencyRace {
	return []ConcurrencyRace{RaceSameResource, RaceSameIdempotency}
}

// MultiProcessRaces need genuinely separate processes (M24-121).
//
// They cannot be simulated in one process: a SQLite lock conflict, a worker
// ownership dispute, and a shutdown race all depend on separate OS processes
// contending for the same file.
func MultiProcessRaces() []ConcurrencyRace {
	return []ConcurrencyRace{
		RaceSQLiteLock, RaceStaleVersion, RaceWorkerOwnership, RaceShutdown,
	}
}

// AllRaces returns every declared race.
func AllRaces() []ConcurrencyRace {
	return append(append([]ConcurrencyRace{}, InProcessRaces()...), MultiProcessRaces()...)
}

// KeyFixtureKind names one API-key fixture (M24-123).
type KeyFixtureKind string

const (
	KeyMalformed     KeyFixtureKind = "malformed"
	KeyMissing       KeyFixtureKind = "missing"
	KeyInvalid       KeyFixtureKind = "invalid"
	KeyRevoked       KeyFixtureKind = "revoked"
	KeyScopeMismatch KeyFixtureKind = "scope-mismatch"
	KeyValid         KeyFixtureKind = "valid"
)

// AllKeyFixtures returns every declared key fixture.
func AllKeyFixtures() []KeyFixtureKind {
	return []KeyFixtureKind{
		KeyMalformed, KeyMissing, KeyInvalid, KeyRevoked, KeyScopeMismatch, KeyValid,
	}
}

// KeyFixture is one synthetic API key (M24-123).
type KeyFixture struct {
	Kind KeyFixtureKind
	// Material is the key text. Every one is synthetic and self-identifying.
	Material string
	// ExpectedStatus is what a correct implementation returns.
	ExpectedStatus int
	// MustBeDistinguishable records that this rejection must not be collapsed
	// into a generic 401: a caller cannot fix a revoked key by re-sending it.
	MustBeDistinguishable bool
}

// KeyFixtures returns the declared fixtures (M24-123).
func KeyFixtures() []KeyFixture {
	return []KeyFixture{
		{KeyValid, "rf-fixture-not-a-real-key-valid-0001", 200, false},
		{KeyMissing, "", 401, true},
		{KeyMalformed, "not-a-key", 401, true},
		{KeyInvalid, "rf-fixture-not-a-real-key-unknown-0002", 401, true},
		{KeyRevoked, "rf-fixture-not-a-real-key-revoked-0003", 401, true},
		{KeyScopeMismatch, "rf-fixture-not-a-real-key-readonly-0004", 403, true},
	}
}

// Validate rejects a fixture that is not obviously synthetic.
func (fixture KeyFixture) Validate() error {
	if !slices.Contains(AllKeyFixtures(), fixture.Kind) {
		return fmt.Errorf("unknown key fixture kind %q", fixture.Kind)
	}
	if fixture.Kind == KeyMissing {
		if fixture.Material != "" {
			return errors.New("the missing-key fixture carries material")
		}
		return nil
	}
	if fixture.Kind != KeyMalformed &&
		!strings.Contains(strings.ToLower(fixture.Material), "not-a-real") {
		return fmt.Errorf(
			"key fixture %q does not identify itself as synthetic; a fixture that looks "+
				"like a real key becomes one in a bug report", fixture.Kind)
	}
	if fixture.ExpectedStatus < 200 || fixture.ExpectedStatus > 599 {
		return fmt.Errorf("key fixture %q expects status %d", fixture.Kind, fixture.ExpectedStatus)
	}
	return nil
}

// SecretMarkerSurface names a place a seeded marker must be searched for
// (M24-124).
type SecretMarkerSurface string

const (
	SurfaceCredentialStore SecretMarkerSurface = "credential-store"
	SurfaceCallbackConfig  SecretMarkerSurface = "callback-configuration"
	SurfaceRequestBody     SecretMarkerSurface = "request-body"
	SurfaceToolOutput      SecretMarkerSurface = "tool-output"
	SurfaceDatabase        SecretMarkerSurface = "application-database"
	SurfaceLogs            SecretMarkerSurface = "logs"
	SurfaceEvents          SecretMarkerSurface = "durable-events"
	SurfaceDiagnostics     SecretMarkerSurface = "diagnostic-export"
	SurfaceDisplayed       SecretMarkerSurface = "displayed-interface"
)

// AllSecretSurfaces returns every surface a leak could reach (M24-124).
func AllSecretSurfaces() []SecretMarkerSurface {
	return []SecretMarkerSurface{
		SurfaceCredentialStore, SurfaceCallbackConfig, SurfaceRequestBody,
		SurfaceToolOutput, SurfaceDatabase, SurfaceLogs, SurfaceEvents,
		SurfaceDiagnostics, SurfaceDisplayed,
	}
}

// SecretMarkers are the synthetic markers seeded into the run (M24-124).
//
// Each is seeded into a different place so a leak names where it came from.
// One marker everywhere would prove a leak happened and nothing about where.
func SecretMarkers() map[SecretMarkerSurface]string {
	markers := map[SecretMarkerSurface]string{}
	for _, surface := range []SecretMarkerSurface{
		SurfaceCredentialStore, SurfaceCallbackConfig,
		SurfaceRequestBody, SurfaceToolOutput,
	} {
		markers[surface] = "rf-marker-not-a-real-secret-" + string(surface)
	}
	return markers
}

// LeakFinding is one marker found where it must not be.
type LeakFinding struct {
	Marker  SecretMarkerSurface
	FoundIn SecretMarkerSurface
}

// ScanSurfaces searches each rendered surface for every seeded marker
// (M24-124).
func ScanSurfaces(rendered map[SecretMarkerSurface]string) ([]LeakFinding, error) {
	markers := SecretMarkers()
	if len(markers) == 0 {
		return nil, errors.New("no markers were seeded, so the scan would pass regardless")
	}
	if len(rendered) == 0 {
		return nil, errors.New("no surfaces were rendered, so nothing was actually scanned")
	}
	var findings []LeakFinding
	for surface, content := range rendered {
		for origin, marker := range markers {
			// A marker appearing on the surface it was seeded into is not a
			// leak: that is where it lives.
			if surface == origin {
				continue
			}
			if strings.Contains(content, marker) {
				findings = append(findings, LeakFinding{Marker: origin, FoundIn: surface})
			}
		}
	}
	sort.Slice(findings, func(left, right int) bool {
		if findings[left].FoundIn != findings[right].FoundIn {
			return findings[left].FoundIn < findings[right].FoundIn
		}
		return findings[left].Marker < findings[right].Marker
	})
	return findings, nil
}

// SnapshotKind names one database state the harness provides (M24-125).
type SnapshotKind string

const (
	SnapshotEmpty                SnapshotKind = "empty"
	SnapshotPriorSchema          SnapshotKind = "prior-schema"
	SnapshotPopulated            SnapshotKind = "populated"
	SnapshotInterruptedMigration SnapshotKind = "interrupted-migration"
	SnapshotUnsupportedNewer     SnapshotKind = "unsupported-newer-schema"
)

// AllSnapshots returns every declared snapshot.
func AllSnapshots() []SnapshotKind {
	return []SnapshotKind{
		SnapshotEmpty, SnapshotPriorSchema, SnapshotPopulated,
		SnapshotInterruptedMigration, SnapshotUnsupportedNewer,
	}
}

// MustRefuse reports whether opening this snapshot must be refused.
func (snapshot SnapshotKind) MustRefuse() bool {
	return snapshot == SnapshotUnsupportedNewer
}

// ContractAspect names one thing the OpenAPI verifier compares (M24-126).
type ContractAspect string

const (
	AspectPaths          ContractAspect = "paths"
	AspectMethods        ContractAspect = "methods"
	AspectRequestSchema  ContractAspect = "request-schemas"
	AspectResponseSchema ContractAspect = "response-schemas"
	AspectStatusCodes    ContractAspect = "status-codes"
	AspectPagination     ContractAspect = "pagination"
	AspectIdempotency    ContractAspect = "idempotency"
	AspectConcurrency    ContractAspect = "concurrency-headers"
	AspectErrorEnvelope  ContractAspect = "error-envelopes"
)

// AllContractAspects returns everything the verifier must compare (M24-126).
func AllContractAspects() []ContractAspect {
	return []ContractAspect{
		AspectPaths, AspectMethods, AspectRequestSchema, AspectResponseSchema,
		AspectStatusCodes, AspectPagination, AspectIdempotency,
		AspectConcurrency, AspectErrorEnvelope,
	}
}

// ContractMismatch is one difference between the description and the runtime.
type ContractMismatch struct {
	Aspect      ContractAspect
	Described   string
	Actual      string
	Explanation string
}

// VerifyContract compares a description against runtime behaviour (M24-126).
//
// Both directions are checked. A description that omits a real endpoint is as
// wrong as one that describes an endpoint that does not exist: the first
// hides behaviour from a caller, the second promises behaviour that is not
// there.
func VerifyContract(described, actual map[ContractAspect][]string) ([]ContractMismatch, error) {
	if len(described) == 0 || len(actual) == 0 {
		return nil, errors.New("both a description and a runtime observation are required")
	}
	var mismatches []ContractMismatch
	for _, aspect := range AllContractAspects() {
		describedValues, describedOK := described[aspect]
		actualValues, actualOK := actual[aspect]
		if !describedOK || !actualOK {
			mismatches = append(mismatches, ContractMismatch{
				Aspect:    aspect,
				Described: fmt.Sprintf("%v", describedValues),
				Actual:    fmt.Sprintf("%v", actualValues),
				Explanation: "this aspect was not compared at all, so the contract is " +
					"unverified for it",
			})
			continue
		}
		missing := difference(describedValues, actualValues)
		extra := difference(actualValues, describedValues)
		for _, value := range missing {
			mismatches = append(mismatches, ContractMismatch{
				Aspect: aspect, Described: value, Actual: "(absent)",
				Explanation: "described but not implemented; a caller would rely on it",
			})
		}
		for _, value := range extra {
			mismatches = append(mismatches, ContractMismatch{
				Aspect: aspect, Described: "(absent)", Actual: value,
				Explanation: "implemented but not described; a caller cannot discover it " +
					"and it can change without notice",
			})
		}
	}
	sort.Slice(mismatches, func(left, right int) bool {
		if mismatches[left].Aspect != mismatches[right].Aspect {
			return mismatches[left].Aspect < mismatches[right].Aspect
		}
		return mismatches[left].Described < mismatches[right].Described
	})
	return mismatches, nil
}

func difference(left, right []string) []string {
	present := make(map[string]bool, len(right))
	for _, value := range right {
		present[value] = true
	}
	var only []string
	for _, value := range left {
		if !present[value] {
			only = append(only, value)
		}
	}
	sort.Strings(only)
	return only
}

// SuiteKind separates the two test suites (M24-127, M24-128).
type SuiteKind string

const (
	// SuiteVisible gives the agent legitimate local feedback.
	SuiteVisible SuiteKind = "visible"
	// SuiteHidden judges the result and is never readable by the agent.
	SuiteHidden SuiteKind = "hidden"
)

// FrozenSuite is one sealed test suite (M24-127, M24-128, M24-130).
type FrozenSuite struct {
	Kind SuiteKind
	// Packet is the requirement it covers.
	Packet int
	// TreeHash seals its contents (M24-130).
	TreeHash string
	// FrozenAt is when it was sealed. A hidden suite must be sealed BEFORE the
	// run: one written afterwards can be shaped to whatever the agent produced.
	FrozenAt time.Time
	// AssertsBehaviour records the M24-129 review verdict: the suite checks
	// required behaviour rather than a preferred implementation shape.
	AssertsBehaviour bool
	// ReviewNote is what the reviewer concluded.
	ReviewNote string
}

// Validate rejects a suite that could not be trusted to judge a run.
func (suite FrozenSuite) Validate(runStartedAt time.Time) error {
	switch {
	case suite.Kind != SuiteVisible && suite.Kind != SuiteHidden:
		return fmt.Errorf("unknown suite kind %q", suite.Kind)
	case suite.Packet < 1 || suite.Packet > PacketCount:
		return fmt.Errorf("suite covers packet %d, outside 1..%d", suite.Packet, PacketCount)
	}
	if err := validateHex(suite.TreeHash, 64, "suite tree hash"); err != nil {
		return err
	}
	if suite.FrozenAt.IsZero() {
		return fmt.Errorf("the %s suite for packet %d has no freeze time",
			suite.Kind, suite.Packet)
	}
	if suite.Kind == SuiteHidden {
		// M24-128: sealed before the run, or it can be shaped to the result.
		if !suite.FrozenAt.Before(runStartedAt) {
			return fmt.Errorf(
				"the hidden suite for packet %d was frozen at %s, not before the run began "+
					"at %s; a suite written afterwards can be shaped to whatever was produced",
				suite.Packet, suite.FrozenAt, runStartedAt)
		}
		// M24-129: reviewed for behaviour rather than shape.
		if !suite.AssertsBehaviour {
			return fmt.Errorf(
				"the hidden suite for packet %d was not reviewed as asserting required "+
					"behaviour; a suite that encodes a preferred implementation shape fails "+
					"a correct solution", suite.Packet)
		}
		if strings.TrimSpace(suite.ReviewNote) == "" {
			return fmt.Errorf("the hidden suite for packet %d has no review note", suite.Packet)
		}
	}
	return nil
}

// SealedArtifacts are the hashes that make post-run edits detectable
// (M24-130).
type SealedArtifacts struct {
	EvaluatorRepository string
	RequirementPackets  string
	VisibleFixtures     string
	HiddenFixtures      string
	ScoringConfig       string
	SealedAt            time.Time
}

// SealedArtifactFields names each hash, so a missing one is reported by name.
func SealedArtifactFields() []string {
	return []string{
		"evaluator-repository", "requirement-packets", "visible-fixtures",
		"hidden-fixtures", "scoring-configuration",
	}
}

// Validate rejects an incomplete seal.
func (sealed SealedArtifacts) Validate() error {
	values := map[string]string{
		"evaluator-repository":  sealed.EvaluatorRepository,
		"requirement-packets":   sealed.RequirementPackets,
		"visible-fixtures":      sealed.VisibleFixtures,
		"hidden-fixtures":       sealed.HiddenFixtures,
		"scoring-configuration": sealed.ScoringConfig,
	}
	var missing []string
	for _, field := range SealedArtifactFields() {
		value := values[field]
		if strings.TrimSpace(value) == "" {
			missing = append(missing, field)
			continue
		}
		if err := validateHex(value, 64, field+" hash"); err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(
			"the seal omits %s; an unhashed artifact can be edited after the run without "+
				"anyone being able to tell", strings.Join(missing, ", "))
	}
	if sealed.SealedAt.IsZero() {
		return errors.New("the seal has no time")
	}
	return nil
}

// DetectTampering compares a seal against the current hashes (M24-130).
func DetectTampering(sealed SealedArtifacts, current map[string]string) ([]string, error) {
	if err := sealed.Validate(); err != nil {
		return nil, err
	}
	expected := map[string]string{
		"evaluator-repository":  sealed.EvaluatorRepository,
		"requirement-packets":   sealed.RequirementPackets,
		"visible-fixtures":      sealed.VisibleFixtures,
		"hidden-fixtures":       sealed.HiddenFixtures,
		"scoring-configuration": sealed.ScoringConfig,
	}
	var changed []string
	for _, field := range SealedArtifactFields() {
		actual, present := current[field]
		if !present {
			changed = append(changed, field+" (not re-hashed, so a change cannot be ruled out)")
			continue
		}
		if actual != expected[field] {
			changed = append(changed, field)
		}
	}
	sort.Strings(changed)
	return changed, nil
}
