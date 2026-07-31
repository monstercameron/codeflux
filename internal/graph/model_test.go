package graph

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestMinimalGraphVocabularyIsExactAndExhaustive(t *testing.T) {
	assertExactStrings(t, "node classes", AllNodeClasses(), []string{
		"requirement", "plan-region", "atom-operation", "effect",
		"branch-match-merge", "obligation", "artifact-result",
	})
	assertExactStrings(t, "edge classes", AllEdgeClasses(), []string{
		"control", "data-provenance", "evidence-dependency", "retry",
		"reconciliation", "compensation",
	})
	assertExactStrings(t, "node statuses", AllNodeStatuses(), []string{
		"pending", "active", "passed", "warning", "failed", "blocked", "invalidated",
	})
	assertExactStrings(t, "graph modes", AllModes(), []string{"program", "execution", "evidence"})

	if NodeClass("invented").IsValid() || EdgeClass("invented").IsValid() ||
		NodeStatus("invented").IsValid() || Mode("invented").IsValid() {
		t.Fatal("unknown graph vocabulary was accepted")
	}
}

func TestGraphAndRevisionIdentitiesRemainDistinct(t *testing.T) {
	ids := graphFixtureIDs(t)
	createdAt := time.Date(2026, 7, 31, 20, 0, 0, 0, time.UTC)
	identity, err := NewGraph(ids.graph, ids.task, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := NewRevisionMetadata(
		ids.revision, ids.graph, 1, nil, CurrentSchemaVersion, createdAt, SourceLinks{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID() != ids.graph || metadata.GraphID() != ids.graph || metadata.ID() != ids.revision ||
		reflect.TypeOf(identity.ID()) == reflect.TypeOf(metadata.ID()) {
		t.Fatalf("graph=%T %s revision=%T %s", identity.ID(), identity.ID(), metadata.ID(), metadata.ID())
	}
	if !strings.HasPrefix(identity.ID().String(), "grf_") || !strings.HasPrefix(metadata.ID().String(), "grv_") {
		t.Fatal("graph and revision identities lost canonical prefixes")
	}
}

func TestGraphIdentityRecordRejectsMissingScopeAndNonUTCTime(t *testing.T) {
	ids := graphFixtureIDs(t)
	createdAt := time.Date(2026, 7, 31, 20, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		graphID domain.GraphID
		taskID  domain.TaskID
		created time.Time
	}{
		{"empty graph", domain.GraphID{}, ids.task, createdAt},
		{"empty task", ids.graph, domain.TaskID{}, createdAt},
		{"zero time", ids.graph, ids.task, time.Time{}},
		{"non UTC time", ids.graph, ids.task, createdAt.In(time.FixedZone("local", -18000))},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewGraph(test.graphID, test.taskID, test.created); !errors.Is(err, ErrInvalidGraphModel) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRevisionRetainsTypedParentSourcesContractAndTopology(t *testing.T) {
	ids := graphFixtureIDs(t)
	createdAt := time.Date(2026, 7, 31, 20, 1, 0, 0, time.UTC)
	eventInput := []domain.EventID{ids.event}
	stepInput := []PlanStepLink{{PlanRevision: 3, StepID: "step-validation"}}
	sources, err := NewSourceLinks(eventInput, stepInput)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []string{"accepted task requirement"}
	outputs := []string{"bounded validation result"}
	effects := []string{"None: explanatory projection only"}
	contract, err := NewContractSummary("Validate the changed task slice.", inputs, outputs, effects)
	if err != nil {
		t.Fatal(err)
	}
	requirement, err := NewNode(
		ids.nodeOne, NodeClassRequirement, NodeStatusPassed, "Accepted requirement", ContractSummary{}, sources,
	)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := NewNode(
		ids.nodeTwo, NodeClassObligation, NodeStatusActive, "Required validation", contract, sources,
	)
	if err != nil {
		t.Fatal(err)
	}
	edge, err := NewEdge(ids.edge, EdgeClassEvidenceDependency, ids.nodeOne, ids.nodeTwo, sources)
	if err != nil {
		t.Fatal(err)
	}
	parentInput := ids.parent
	metadata, err := NewRevisionMetadata(
		ids.revision, ids.graph, 2, &parentInput, CurrentSchemaVersion, createdAt, sources,
	)
	if err != nil {
		t.Fatal(err)
	}
	nodeInput := []Node{requirement, validation}
	edgeInput := []Edge{edge}
	revision, err := NewRevision(metadata, nodeInput, edgeInput)
	if err != nil {
		t.Fatal(err)
	}

	parentInput = domain.GraphRevisionID{}
	eventInput[0] = domain.EventID{}
	stepInput[0].StepID = "changed"
	inputs[0], outputs[0], effects[0] = "changed", "changed", "changed"
	nodeInput[0], edgeInput[0] = Node{}, Edge{}

	parent, ok := revision.Metadata().ParentID()
	nodes, edges := revision.Nodes(), revision.Edges()
	if !ok || parent != ids.parent || revision.Metadata().Ordinal() != 2 ||
		revision.Metadata().SchemaVersion() != SchemaVersionV1 ||
		revision.Metadata().MetadataPolicy() != MetadataPolicyTypedFieldsOnly ||
		len(nodes) != 2 || len(edges) != 1 ||
		nodes[1].Contract().Purpose() != "Validate the changed task slice." ||
		nodes[1].Contract().Inputs()[0] != "accepted task requirement" ||
		nodes[1].Sources().EventIDs()[0] != ids.event ||
		nodes[1].Sources().PlanSteps()[0].StepID != "step-validation" ||
		edges[0].FromNode() != ids.nodeOne || edges[0].ToNode() != ids.nodeTwo {
		t.Fatalf("revision lost immutable typed facts: metadata=%+v nodes=%+v edges=%+v", revision.Metadata(), nodes, edges)
	}

	nodes[0] = Node{}
	edges[0] = Edge{}
	if revision.Nodes()[0].ID() != ids.nodeOne || revision.Edges()[0].ID() != ids.edge {
		t.Fatal("revision accessors exposed mutable topology slices")
	}
}

func TestRevisionParentageAndIndependentSchemaVersionAreValidated(t *testing.T) {
	ids := graphFixtureIDs(t)
	createdAt := time.Date(2026, 7, 31, 20, 2, 0, 0, time.UTC)
	tests := []struct {
		name    string
		ordinal uint64
		parent  *domain.GraphRevisionID
		version SchemaVersion
		created time.Time
	}{
		{"zero ordinal", 0, nil, SchemaVersionV1, createdAt},
		{"root with parent", 1, &ids.parent, SchemaVersionV1, createdAt},
		{"child without parent", 2, nil, SchemaVersionV1, createdAt},
		{"self parent", 2, &ids.revision, SchemaVersionV1, createdAt},
		{"unknown schema", 1, nil, SchemaVersion(2), createdAt},
		{"local timestamp", 1, nil, SchemaVersionV1, createdAt.In(time.FixedZone("local", 3600))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewRevisionMetadata(
				ids.revision, ids.graph, test.ordinal, test.parent, test.version, test.created, SourceLinks{},
			)
			if !errors.Is(err, ErrInvalidGraphModel) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if !CurrentSchemaVersion.IsValid() || SchemaVersion(0).IsValid() || SchemaVersion(2).IsValid() {
		t.Fatal("graph schema version is not independently and exactly versioned")
	}
}

func TestSourceLinksAndContractSummaryEnforceMetadataBounds(t *testing.T) {
	ids := graphFixtureIDs(t)
	tooManyEvents := make([]domain.EventID, MaximumSourceEventLinks+1)
	for index := range tooManyEvents {
		tooManyEvents[index] = ids.event
	}
	tooManySteps := make([]PlanStepLink, MaximumPlanStepLinks+1)
	for index := range tooManySteps {
		tooManySteps[index] = PlanStepLink{PlanRevision: 1, StepID: "step"}
	}
	for _, test := range []struct {
		name   string
		events []domain.EventID
		steps  []PlanStepLink
	}{
		{"too many events", tooManyEvents, nil},
		{"too many steps", nil, tooManySteps},
		{"duplicate event", []domain.EventID{ids.event, ids.event}, nil},
		{"empty event", []domain.EventID{{}}, nil},
		{"duplicate step", nil, []PlanStepLink{{1, "step"}, {1, "step"}}},
		{"zero plan revision", nil, []PlanStepLink{{0, "step"}}},
		{"untrimmed step", nil, []PlanStepLink{{1, " step"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSourceLinks(test.events, test.steps); !errors.Is(err, ErrInvalidGraphModel) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	tooManyItems := make([]string, MaximumContractItems+1)
	for index := range tooManyItems {
		tooManyItems[index] = strings.Repeat("x", index+1)
	}
	for _, test := range []struct {
		name    string
		purpose string
		inputs  []string
	}{
		{"missing purpose", "", []string{"input"}},
		{"long purpose", strings.Repeat("p", MaximumContractPurposeBytes+1), nil},
		{"too many items", "purpose", tooManyItems},
		{"long item", "purpose", []string{strings.Repeat("i", MaximumContractItemBytes+1)}},
		{"duplicate item", "purpose", []string{"same", "same"}},
		{"control character", "purpose", []string{"unsafe\nitem"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewContractSummary(test.purpose, test.inputs, nil, nil); !errors.Is(err, ErrInvalidGraphModel) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if summary, err := NewContractSummary("", nil, nil, nil); err != nil || !summary.IsZero() {
		t.Fatalf("empty unknown contract = %+v, %v", summary, err)
	}
}

func TestNodeEdgeAndRevisionRejectMalformedStableTopology(t *testing.T) {
	ids := graphFixtureIDs(t)
	createdAt := time.Date(2026, 7, 31, 20, 3, 0, 0, time.UTC)
	metadata, err := NewRevisionMetadata(ids.revision, ids.graph, 1, nil, SchemaVersionV1, createdAt, SourceLinks{})
	if err != nil {
		t.Fatal(err)
	}
	validNode, err := NewNode(ids.nodeOne, NodeClassRequirement, NodeStatusPending, "Requirement", ContractSummary{}, SourceLinks{})
	if err != nil {
		t.Fatal(err)
	}
	otherNode, err := NewNode(ids.nodeTwo, NodeClassEffect, NodeStatusActive, "Run validation", ContractSummary{}, SourceLinks{})
	if err != nil {
		t.Fatal(err)
	}
	validEdge, err := NewEdge(ids.edge, EdgeClassControl, ids.nodeOne, ids.nodeTwo, SourceLinks{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewNode(domain.NodeID{}, NodeClassRequirement, NodeStatusPending, "Requirement", ContractSummary{}, SourceLinks{}); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("empty node error = %v", err)
	}
	if _, err := NewNode(ids.nodeOne, "invented", NodeStatusPending, "Requirement", ContractSummary{}, SourceLinks{}); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("node class error = %v", err)
	}
	if _, err := NewNode(ids.nodeOne, NodeClassRequirement, "invented", "Requirement", ContractSummary{}, SourceLinks{}); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("node status error = %v", err)
	}
	if _, err := NewNode(ids.nodeOne, NodeClassRequirement, NodeStatusPending, " Requirement", ContractSummary{}, SourceLinks{}); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("node display name error = %v", err)
	}
	if _, err := NewEdge(ids.edge, "invented", ids.nodeOne, ids.nodeTwo, SourceLinks{}); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("edge class error = %v", err)
	}
	if _, err := NewEdge(ids.edge, EdgeClassControl, domain.NodeID{}, ids.nodeTwo, SourceLinks{}); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("edge endpoint error = %v", err)
	}
	if _, err := NewRevision(metadata, []Node{validNode, validNode}, nil); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("duplicate node error = %v", err)
	}
	if _, err := NewRevision(metadata, []Node{validNode, otherNode}, []Edge{validEdge, validEdge}); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("duplicate edge error = %v", err)
	}
	absentEdge, err := NewEdge(ids.edge, EdgeClassControl, ids.nodeOne, ids.nodeThree, SourceLinks{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRevision(metadata, []Node{validNode, otherNode}, []Edge{absentEdge}); !errors.Is(err, ErrInvalidGraphModel) {
		t.Fatalf("absent endpoint error = %v", err)
	}
}

func TestGraphSchemaProhibitsArbitraryMetadataMaps(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(Graph{}), reflect.TypeOf(RevisionMetadata{}), reflect.TypeOf(Revision{}),
		reflect.TypeOf(Node{}), reflect.TypeOf(Edge{}), reflect.TypeOf(SourceLinks{}),
		reflect.TypeOf(ContractSummary{}), reflect.TypeOf(PlanStepLink{}),
	}
	for _, schemaType := range types {
		for index := 0; index < schemaType.NumField(); index++ {
			field := schemaType.Field(index)
			if field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Interface {
				t.Fatalf("%s.%s permits arbitrary metadata through %s", schemaType.Name(), field.Name, field.Type)
			}
		}
	}
	if !MetadataPolicyTypedFieldsOnly.IsValid() || MetadataPolicy("arbitrary-map").IsValid() {
		t.Fatal("graph metadata policy did not prohibit arbitrary maps")
	}
	metadataType := reflect.TypeOf(RevisionMetadata{})
	for index := 0; index < metadataType.NumField(); index++ {
		if metadataType.Field(index).PkgPath == "" {
			t.Fatalf("revision metadata field %s is externally mutable", metadataType.Field(index).Name)
		}
	}
}

type fixtureGraphIDs struct {
	graph     domain.GraphID
	revision  domain.GraphRevisionID
	parent    domain.GraphRevisionID
	task      domain.TaskID
	nodeOne   domain.NodeID
	nodeTwo   domain.NodeID
	nodeThree domain.NodeID
	edge      domain.EdgeID
	event     domain.EventID
}

func graphFixtureIDs(t *testing.T) fixtureGraphIDs {
	t.Helper()
	return fixtureGraphIDs{
		graph:     mustGraphIdentity(t, domain.ParseGraphID, "grf_01890f3c-4a00-7abc-8def-0123456789ab"),
		revision:  mustGraphIdentity(t, domain.ParseGraphRevisionID, "grv_01890f3c-4a00-7abc-8def-0123456789ab"),
		parent:    mustGraphIdentity(t, domain.ParseGraphRevisionID, "grv_01890f3c-4a00-7abc-8def-1123456789ab"),
		task:      mustGraphIdentity(t, domain.ParseTaskID, "tsk_01890f3c-4a00-7abc-8def-0123456789ab"),
		nodeOne:   mustGraphIdentity(t, domain.ParseNodeID, "nod_01890f3c-4a00-7abc-8def-0123456789ab"),
		nodeTwo:   mustGraphIdentity(t, domain.ParseNodeID, "nod_01890f3c-4a00-7abc-8def-1123456789ab"),
		nodeThree: mustGraphIdentity(t, domain.ParseNodeID, "nod_01890f3c-4a00-7abc-8def-2123456789ab"),
		edge:      mustGraphIdentity(t, domain.ParseEdgeID, "edg_01890f3c-4a00-7abc-8def-0123456789ab"),
		event:     mustGraphIdentity(t, domain.ParseEventID, "evt_01890f3c-4a00-7abc-8def-0123456789ab"),
	}
}

func mustGraphIdentity[T any](t *testing.T, parse func(string) (T, error), raw string) T {
	t.Helper()
	value, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertExactStrings[T ~string](t *testing.T, name string, got []T, want []string) {
	t.Helper()
	values := make([]string, len(got))
	seen := make(map[string]bool, len(got))
	for index, value := range got {
		values[index] = string(value)
		if value == "" || seen[string(value)] {
			t.Fatalf("%s contains blank or duplicate value %q", name, value)
		}
		seen[string(value)] = true
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("%s = %v, want %v", name, values, want)
	}
}
