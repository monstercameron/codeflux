package providers

import (
	"context"
	"encoding/json"
	"time"
)

// ModelStream delivers ordered events until one final response or an error.
type ModelStream interface {
	Recv(context.Context) (StreamEvent, error)
	Close() error
}

type StreamEventKind string

const (
	StreamEventTextDelta     StreamEventKind = "text-delta"
	StreamEventToolCallDelta StreamEventKind = "tool-call-delta"
	StreamEventToolCall      StreamEventKind = "tool-call"
	StreamEventUsage         StreamEventKind = "usage"
	StreamEventMetadata      StreamEventKind = "metadata"
	StreamEventFinal         StreamEventKind = "final"
)

type StreamEvent struct {
	Sequence      int64
	Kind          StreamEventKind
	Text          string
	ToolCallDelta *ToolCallDelta
	ToolCall      *ToolCall
	Usage         *Usage
	Metadata      *RedactedProviderMetadata
	Final         *FinalResponse
	ObservedAt    time.Time
}

type ToolCallDelta struct {
	Index             int
	ID                string
	Name              string
	ArgumentsFragment string
}

type FinalResponse struct {
	Identity      ModelIdentity
	StopReason    StopReason
	Usage         Usage
	Metadata      RedactedProviderMetadata
	PartialEffect PartialEffectEvidence
}

type StopReason string

const (
	StopReasonCompleted     StopReason = "completed"
	StopReasonMaximumTokens StopReason = "maximum-tokens"
	StopReasonToolCalls     StopReason = "tool-calls"
	StopReasonSafety        StopReason = "safety"
	StopReasonCanceled      StopReason = "canceled"
	StopReasonError         StopReason = "error"
	StopReasonUnknown       StopReason = "unknown"
)

// PartialEffectEvidence prevents unsafe automatic retry after visible output
// or tool intent has crossed the provider boundary.
type PartialEffectEvidence struct {
	StreamedOutput bool
	ToolCall       bool
	ProviderAck    bool
	LastSequence   int64
}

type UsageSource string

const (
	UsageSourceProvider  UsageSource = "provider"
	UsageSourceEstimated UsageSource = "estimated"
	UsageSourceUnknown   UsageSource = "unknown"
)

type Usage struct {
	Known             bool
	Source            UsageSource
	InputTokens       int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	OutputTokens      int64
	ReasoningTokens   int64
	ProviderSpecific  json.RawMessage
}
