package transport

import (
	"errors"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/protobuf/proto"
)

const uuidV7Fixture = "01890f3c-4a00-7abc-8def-0123456789ab"

type identityProtoCase struct {
	name   string
	prefix string
	kind   codefluxv1.StableIdentityKind
	to     func(string) (*codefluxv1.StableIdentity, error)
	from   func(*codefluxv1.StableIdentity) (string, error)
}

func TestEveryIdentityTypeProtobufRoundTrips(t *testing.T) {
	cases := []identityProtoCase{
		makeIdentityProtoCase("project", "prj", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROJECT, domain.ParseProjectID, ProjectIDToProto, ProjectIDFromProto),
		makeIdentityProtoCase("repository", "repo", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY, domain.ParseRepositoryID, RepositoryIDToProto, RepositoryIDFromProto),
		makeIdentityProtoCase("workspace", "wsp", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, domain.ParseWorkspaceID, WorkspaceIDToProto, WorkspaceIDFromProto),
		makeIdentityProtoCase("thread", "thr", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, domain.ParseThreadID, ThreadIDToProto, ThreadIDFromProto),
		makeIdentityProtoCase("message", "msg", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE, domain.ParseMessageID, MessageIDToProto, MessageIDFromProto),
		makeIdentityProtoCase("task", "tsk", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, domain.ParseTaskID, TaskIDToProto, TaskIDFromProto),
		makeIdentityProtoCase("run", "run", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_RUN, domain.ParseRunID, RunIDToProto, RunIDFromProto),
		makeIdentityProtoCase("event", "evt", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT, domain.ParseEventID, EventIDToProto, EventIDFromProto),
		makeIdentityProtoCase("checkpoint", "ckp", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT, domain.ParseCheckpointID, CheckpointIDToProto, CheckpointIDFromProto),
		makeIdentityProtoCase("approval", "apr", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL, domain.ParseApprovalID, ApprovalIDToProto, ApprovalIDFromProto),
		makeIdentityProtoCase("graph", "grf", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH, domain.ParseGraphID, GraphIDToProto, GraphIDFromProto),
		makeIdentityProtoCase("graph revision", "grv", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION, domain.ParseGraphRevisionID, GraphRevisionIDToProto, GraphRevisionIDFromProto),
		makeIdentityProtoCase("node", "nod", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_NODE, domain.ParseNodeID, NodeIDToProto, NodeIDFromProto),
		makeIdentityProtoCase("edge", "edg", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EDGE, domain.ParseEdgeID, EdgeIDToProto, EdgeIDFromProto),
		makeIdentityProtoCase("validation", "val", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION, domain.ParseValidationID, ValidationIDToProto, ValidationIDFromProto),
		makeIdentityProtoCase("evidence", "evd", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVIDENCE, domain.ParseEvidenceID, EvidenceIDToProto, EvidenceIDFromProto),
		makeIdentityProtoCase("artifact", "art", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ARTIFACT, domain.ParseArtifactID, ArtifactIDToProto, ArtifactIDFromProto),
		makeIdentityProtoCase("atom", "atm", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ATOM, domain.ParseAtomID, AtomIDToProto, AtomIDFromProto),
		makeIdentityProtoCase("model request", "mrq", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MODEL_REQUEST, domain.ParseModelRequestID, ModelRequestIDToProto, ModelRequestIDFromProto),
		makeIdentityProtoCase("provider", "prv", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER, domain.ParseProviderID, ProviderIDToProto, ProviderIDFromProto),
		makeIdentityProtoCase("budget", "bdg", codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_BUDGET, domain.ParseBudgetID, BudgetIDToProto, BudgetIDFromProto),
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			raw := test.prefix + "_" + uuidV7Fixture
			message, err := test.to(raw)
			if err != nil {
				t.Fatalf("to protobuf: %v", err)
			}
			if message.GetKind() != test.kind || message.GetValue() != raw {
				t.Fatalf("protobuf identity = %#v", message)
			}

			wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
			if err != nil {
				t.Fatalf("marshal protobuf: %v", err)
			}
			var decoded codefluxv1.StableIdentity
			if err := proto.Unmarshal(wire, &decoded); err != nil {
				t.Fatalf("unmarshal protobuf: %v", err)
			}
			roundTrip, err := test.from(&decoded)
			if err != nil {
				t.Fatalf("from protobuf: %v", err)
			}
			if roundTrip != raw {
				t.Fatalf("round-trip identity = %q, want %q", roundTrip, raw)
			}

			decoded.Kind = codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_UNSPECIFIED
			if _, err := test.from(&decoded); !errors.Is(err, ErrInvalidIdentityConversion) {
				t.Fatalf("wrong-kind error = %v, want ErrInvalidIdentityConversion", err)
			}
			if _, err := test.from(nil); !errors.Is(err, ErrInvalidIdentityConversion) {
				t.Fatalf("nil error = %v, want ErrInvalidIdentityConversion", err)
			}
		})
	}
}

func TestIdentityToProtoRejectsZeroValue(t *testing.T) {
	if _, err := ProjectIDToProto(domain.ProjectID{}); !errors.Is(err, ErrInvalidIdentityConversion) {
		t.Fatalf("zero conversion error = %v, want ErrInvalidIdentityConversion", err)
	}
}

func makeIdentityProtoCase[T interface{ String() string }](
	name string,
	prefix string,
	kind codefluxv1.StableIdentityKind,
	parse func(string) (T, error),
	to func(T) (*codefluxv1.StableIdentity, error),
	from func(*codefluxv1.StableIdentity) (T, error),
) identityProtoCase {
	return identityProtoCase{
		name:   name,
		prefix: prefix,
		kind:   kind,
		to: func(raw string) (*codefluxv1.StableIdentity, error) {
			value, err := parse(raw)
			if err != nil {
				return nil, err
			}
			return to(value)
		},
		from: func(message *codefluxv1.StableIdentity) (string, error) {
			value, err := from(message)
			if err != nil {
				return "", err
			}
			return value.String(), nil
		},
	}
}
