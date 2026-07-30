package providers

import (
	"context"
	"encoding/json"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// ModelProvider is the normalized coordinator-owned provider boundary.
type ModelProvider interface {
	ProviderIdentity() ProviderIdentity
	ListModels(context.Context) ([]ModelDescriptor, error)
	Capabilities(context.Context, ModelIdentity) (ModelCapabilities, error)
	Stream(context.Context, ModelRequest) (ModelStream, error)
	Cancel(context.Context, domain.ModelRequestID) error
}

// ProviderIdentity records the exact adapter and provider revision used.
type ProviderIdentity struct {
	Adapter         string
	AdapterVersion  string
	Provider        string
	ProviderVersion string
}

// ModelIdentity records both the configured model and returned revision.
type ModelIdentity struct {
	Provider ProviderIdentity
	Model    string
	Revision string
}

// RequestIdentity remains unchanged across physical retries.
type RequestIdentity struct {
	ModelRequestID domain.ModelRequestID
	Provider       ProviderIdentity
	Model          ModelIdentity
	Idempotency    RequestIdempotency
	IdempotencyKey string
	RequestHash    string
}

// ModelDescriptor is one discovered model and its advertised capabilities.
type ModelDescriptor struct {
	Identity     ModelIdentity
	DisplayName  string
	Capabilities ModelCapabilities
}

// ModelCapabilities are evidence captured at discovery/configuration time.
type ModelCapabilities struct {
	Tools               bool
	StructuredOutput    bool
	ContextTokens       int64
	MaximumOutputTokens int64
	ImageInput          bool
	Streaming           bool
	PromptCaching       bool
	ReasoningControls   []string
}

// ModelRequest is one immutable normalized logical request.
type ModelRequest struct {
	Identity         RequestIdentity
	Messages         []Message
	Tools            []ToolDeclaration
	StructuredOutput *StructuredOutputRequirement
	MaximumTokens    int64
	Temperature      *float64
	ReasoningEffort  string
	Idempotency      RequestIdempotency
	Deadline         time.Time
}

// MessageRole is the normalized participant role.
type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleDeveloper MessageRole = "developer"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

// ContentKind identifies a normalized message part.
type ContentKind string

const (
	ContentKindText       ContentKind = "text"
	ContentKindImage      ContentKind = "image"
	ContentKindToolCall   ContentKind = "tool-call"
	ContentKindToolResult ContentKind = "tool-result"
)

type Message struct {
	Role    MessageRole
	Content []ContentPart
}

type ContentPart struct {
	Kind       ContentKind
	Text       string
	Image      *ImageInput
	ToolCall   *ToolCall
	ToolResult *ToolResult
}

type ImageInput struct {
	MediaType string
	Data      []byte
	URL       string
	Detail    string
}

type ToolDeclaration struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type ToolResult struct {
	CallID  string
	Content []ContentPart
	IsError bool
}

type StructuredOutputRequirement struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Strict      bool
}

// RequestIdempotency describes the provider's declared deduplication scope.
type RequestIdempotency struct {
	ProviderSupported bool
	Key               string
	ProviderScope     string
}

// RedactedProviderMetadata retains safe provider evidence only.
type RedactedProviderMetadata struct {
	RequestID   string
	ResponseID  string
	Fingerprint string
	Fields      map[string]string
}
