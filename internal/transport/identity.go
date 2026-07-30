package transport

import (
	"errors"
	"fmt"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
)

// ErrInvalidIdentityConversion classifies invalid protobuf identity envelopes.
var ErrInvalidIdentityConversion = errors.New("invalid identity conversion")

type textIdentity interface {
	String() string
	IsZero() bool
}

func identityToProto(
	identity textIdentity,
	kind codefluxv1.StableIdentityKind,
) (*codefluxv1.StableIdentity, error) {
	if identity.IsZero() {
		return nil, fmt.Errorf("%w: domain identity is empty", ErrInvalidIdentityConversion)
	}
	return &codefluxv1.StableIdentity{
		Kind:  kind,
		Value: identity.String(),
	}, nil
}

func identityFromProto[T any](
	message *codefluxv1.StableIdentity,
	want codefluxv1.StableIdentityKind,
	parse func(string) (T, error),
) (T, error) {
	var zero T
	if message == nil {
		return zero, fmt.Errorf("%w: protobuf identity is nil", ErrInvalidIdentityConversion)
	}
	if message.GetKind() != want {
		return zero, fmt.Errorf(
			"%w: protobuf kind is %s, want %s",
			ErrInvalidIdentityConversion,
			message.GetKind(),
			want,
		)
	}
	identity, err := parse(message.GetValue())
	if err != nil {
		return zero, fmt.Errorf("%w: %w", ErrInvalidIdentityConversion, err)
	}
	return identity, nil
}

// ProjectIDToProto converts a project identity to its protobuf envelope.
func ProjectIDToProto(value domain.ProjectID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROJECT)
}

// ProjectIDFromProto validates and converts a protobuf project identity.
func ProjectIDFromProto(value *codefluxv1.StableIdentity) (domain.ProjectID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROJECT, domain.ParseProjectID)
}

// RepositoryIDToProto converts a repository identity to its protobuf envelope.
func RepositoryIDToProto(value domain.RepositoryID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY)
}

// RepositoryIDFromProto validates and converts a protobuf repository identity.
func RepositoryIDFromProto(value *codefluxv1.StableIdentity) (domain.RepositoryID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY, domain.ParseRepositoryID)
}

// WorkspaceIDToProto converts a workspace identity to its protobuf envelope.
func WorkspaceIDToProto(value domain.WorkspaceID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE)
}

// WorkspaceIDFromProto validates and converts a protobuf workspace identity.
func WorkspaceIDFromProto(value *codefluxv1.StableIdentity) (domain.WorkspaceID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, domain.ParseWorkspaceID)
}

// ThreadIDToProto converts a thread identity to its protobuf envelope.
func ThreadIDToProto(value domain.ThreadID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD)
}

// ThreadIDFromProto validates and converts a protobuf thread identity.
func ThreadIDFromProto(value *codefluxv1.StableIdentity) (domain.ThreadID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, domain.ParseThreadID)
}

// MessageIDToProto converts a message identity to its protobuf envelope.
func MessageIDToProto(value domain.MessageID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE)
}

// MessageIDFromProto validates and converts a protobuf message identity.
func MessageIDFromProto(value *codefluxv1.StableIdentity) (domain.MessageID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE, domain.ParseMessageID)
}

// TaskIDToProto converts a task identity to its protobuf envelope.
func TaskIDToProto(value domain.TaskID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK)
}

// TaskIDFromProto validates and converts a protobuf task identity.
func TaskIDFromProto(value *codefluxv1.StableIdentity) (domain.TaskID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, domain.ParseTaskID)
}

// RunIDToProto converts a run identity to its protobuf envelope.
func RunIDToProto(value domain.RunID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_RUN)
}

// RunIDFromProto validates and converts a protobuf run identity.
func RunIDFromProto(value *codefluxv1.StableIdentity) (domain.RunID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_RUN, domain.ParseRunID)
}

// EventIDToProto converts an event identity to its protobuf envelope.
func EventIDToProto(value domain.EventID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT)
}

// EventIDFromProto validates and converts a protobuf event identity.
func EventIDFromProto(value *codefluxv1.StableIdentity) (domain.EventID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT, domain.ParseEventID)
}

// CheckpointIDToProto converts a checkpoint identity to its protobuf envelope.
func CheckpointIDToProto(value domain.CheckpointID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT)
}

// CheckpointIDFromProto validates and converts a protobuf checkpoint identity.
func CheckpointIDFromProto(value *codefluxv1.StableIdentity) (domain.CheckpointID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT, domain.ParseCheckpointID)
}

// ApprovalIDToProto converts an approval identity to its protobuf envelope.
func ApprovalIDToProto(value domain.ApprovalID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL)
}

// ApprovalIDFromProto validates and converts a protobuf approval identity.
func ApprovalIDFromProto(value *codefluxv1.StableIdentity) (domain.ApprovalID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL, domain.ParseApprovalID)
}

// GraphIDToProto converts a graph identity to its protobuf envelope.
func GraphIDToProto(value domain.GraphID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH)
}

// GraphIDFromProto validates and converts a protobuf graph identity.
func GraphIDFromProto(value *codefluxv1.StableIdentity) (domain.GraphID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH, domain.ParseGraphID)
}

// GraphRevisionIDToProto converts a graph-revision identity to its protobuf envelope.
func GraphRevisionIDToProto(value domain.GraphRevisionID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION)
}

// GraphRevisionIDFromProto validates and converts a protobuf graph-revision identity.
func GraphRevisionIDFromProto(value *codefluxv1.StableIdentity) (domain.GraphRevisionID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION, domain.ParseGraphRevisionID)
}

// NodeIDToProto converts a node identity to its protobuf envelope.
func NodeIDToProto(value domain.NodeID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_NODE)
}

// NodeIDFromProto validates and converts a protobuf node identity.
func NodeIDFromProto(value *codefluxv1.StableIdentity) (domain.NodeID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_NODE, domain.ParseNodeID)
}

// EdgeIDToProto converts an edge identity to its protobuf envelope.
func EdgeIDToProto(value domain.EdgeID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EDGE)
}

// EdgeIDFromProto validates and converts a protobuf edge identity.
func EdgeIDFromProto(value *codefluxv1.StableIdentity) (domain.EdgeID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EDGE, domain.ParseEdgeID)
}

// ValidationIDToProto converts a validation identity to its protobuf envelope.
func ValidationIDToProto(value domain.ValidationID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION)
}

// ValidationIDFromProto validates and converts a protobuf validation identity.
func ValidationIDFromProto(value *codefluxv1.StableIdentity) (domain.ValidationID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION, domain.ParseValidationID)
}

// EvidenceIDToProto converts an evidence identity to its protobuf envelope.
func EvidenceIDToProto(value domain.EvidenceID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVIDENCE)
}

// EvidenceIDFromProto validates and converts a protobuf evidence identity.
func EvidenceIDFromProto(value *codefluxv1.StableIdentity) (domain.EvidenceID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVIDENCE, domain.ParseEvidenceID)
}

// ArtifactIDToProto converts an artifact identity to its protobuf envelope.
func ArtifactIDToProto(value domain.ArtifactID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ARTIFACT)
}

// ArtifactIDFromProto validates and converts a protobuf artifact identity.
func ArtifactIDFromProto(value *codefluxv1.StableIdentity) (domain.ArtifactID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ARTIFACT, domain.ParseArtifactID)
}

// AtomIDToProto converts an atom identity to its protobuf envelope.
func AtomIDToProto(value domain.AtomID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ATOM)
}

// AtomIDFromProto validates and converts a protobuf atom identity.
func AtomIDFromProto(value *codefluxv1.StableIdentity) (domain.AtomID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ATOM, domain.ParseAtomID)
}

// ModelRequestIDToProto converts a model-request identity to its protobuf envelope.
func ModelRequestIDToProto(value domain.ModelRequestID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MODEL_REQUEST)
}

// ModelRequestIDFromProto validates and converts a protobuf model-request identity.
func ModelRequestIDFromProto(value *codefluxv1.StableIdentity) (domain.ModelRequestID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MODEL_REQUEST, domain.ParseModelRequestID)
}

// ProviderIDToProto converts a provider identity to its protobuf envelope.
func ProviderIDToProto(value domain.ProviderID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER)
}

// ProviderIDFromProto validates and converts a protobuf provider identity.
func ProviderIDFromProto(value *codefluxv1.StableIdentity) (domain.ProviderID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER, domain.ParseProviderID)
}

// BudgetIDToProto converts a budget identity to its protobuf envelope.
func BudgetIDToProto(value domain.BudgetID) (*codefluxv1.StableIdentity, error) {
	return identityToProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_BUDGET)
}

// BudgetIDFromProto validates and converts a protobuf budget identity.
func BudgetIDFromProto(value *codefluxv1.StableIdentity) (domain.BudgetID, error) {
	return identityFromProto(value, codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_BUDGET, domain.ParseBudgetID)
}
