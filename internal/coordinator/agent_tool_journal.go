package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

// agentToolJournal is the durable record of every tool a run asked for and
// what came back.
//
// It is separate from the narration the person reads. The narration says what
// happened in words; this says it in facts that can be checked later — which
// arguments, whose authority, which plan step, and what the result hashed to.
// A run whose journal is empty cannot be audited, and its work cannot be
// attributed to the plan it was supposed to be carrying out.
type agentToolJournal struct {
	repositories *storage.Repositories
	taskID       domain.TaskID
	runID        domain.RunID
	// provider and model name what the requests in this journal were made
	// against. A tool record binds to the model request that asked for it, and
	// that request binds to a registered, configured, priced provider.
	provider storage.RegisteredProvider
	model    providers.ModelIdentity
	// planned is the set of model requests already recorded, so a turn that
	// makes two tool calls records one request rather than two.
	planned map[domain.ModelRequestID]bool

	// attribution remembers which plan step each tool request belongs to.
	//
	// The executor is handed a request with no plan step on it — deliberately,
	// because running a command must not depend on planning vocabulary. The
	// step is known here, at the only point that sees both, so the diagram can
	// bind what ran to the step that called for it.
	mutex       sync.Mutex
	attribution map[string]toolAttribution

	// failure is why the durable record is incomplete, if it ever is.
	//
	// A refused journal entry is reported rather than swallowed, but it does
	// not stop the work: refusing to edit a file over a missing audit row would
	// be a worse answer than doing the work and saying plainly what was not
	// recorded. The run reports this at the end, so the gap is visible instead
	// of being discovered later as an empty table.
	failure error
}

// Failure reports why the durable journal is incomplete, if it is.
func (journal *agentToolJournal) Failure() error {
	if journal == nil {
		return nil
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	return journal.failure
}

// note keeps the first reason the durable record could not be written.
func (journal *agentToolJournal) note(err error) error {
	if err == nil {
		return nil
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	if journal.failure == nil {
		journal.failure = err
	}
	return nil
}

// toolAttribution is the plan step one tool request was made for.
type toolAttribution struct {
	PlanRevision uint64
	PlanStepID   string
}

// newAgentToolJournal builds the journal for one run.
func newAgentToolJournal(
	repositories *storage.Repositories,
	taskID domain.TaskID,
	runID domain.RunID,
	provider storage.RegisteredProvider,
	model providers.ModelIdentity,
) *agentToolJournal {
	return &agentToolJournal{
		repositories: repositories, taskID: taskID, runID: runID,
		provider: provider, model: model,
		attribution: map[string]toolAttribution{},
		planned:     map[domain.ModelRequestID]bool{},
	}
}

// recordModelRequest records the model round a tool call came out of.
//
// A tool request names the model request that asked for it, and the store
// requires that request to exist and to belong to the same task and run. The
// loop reports the model request identity but nothing recorded it, so every
// tool record was refused for naming a request that was never written down.
func (journal *agentToolJournal) recordModelRequest(
	ctx context.Context,
	modelRequestID domain.ModelRequestID,
) error {
	if modelRequestID.IsZero() {
		return errors.New("a tool call must name the model request that asked for it")
	}
	journal.mutex.Lock()
	already := journal.planned[modelRequestID]
	journal.mutex.Unlock()
	if already {
		return nil
	}
	runID := journal.runID
	if _, err := journal.repositories.PlanProviderLogicalRequest(
		ctx, storage.PlanProviderLogicalRequest{
			ID: modelRequestID, TaskID: journal.taskID, RunID: &runID,
			ProviderID:                      journal.provider.ProviderID,
			ProviderConfigurationRevisionID: journal.provider.ConfigurationRevisionID,
			AdapterName:                     journal.model.Provider.Adapter,
			AdapterVersion:                  journal.model.Provider.AdapterVersion,
			ProviderVersion:                 journal.model.Provider.ProviderVersion,
			ModelIdentifier:                 journal.model.Model,
			ModelVersion:                    journal.model.Revision,
			PricingRevisionID:               &journal.provider.PricingRevisionID,
			// The request body does not reach this seam, so this digests the
			// request's identity. It tells two requests apart, which is what
			// the column is used for here; it is not an attestation of what
			// was sent, and nothing reads it as one.
			RequestSHA256:  digestOf(modelRequestID.String()),
			IdempotencyKey: "agent-model-request:" + modelRequestID.String(),
		},
	); err != nil {
		return fmt.Errorf("record the model request: %w", err)
	}
	journal.mutex.Lock()
	journal.planned[modelRequestID] = true
	journal.mutex.Unlock()
	return nil
}

// PersistToolStart records the request before the tool is allowed to run.
//
// It is written first, and deliberately: a tool that starts and never returns
// must still leave evidence that it was asked for, or recovery has nothing to
// reconcile against.
func (journal *agentToolJournal) PersistToolStart(
	ctx context.Context,
	record agentloop.ToolStartRecord,
) error {
	if journal == nil || journal.repositories == nil {
		return nil
	}
	journal.remember(record.RequestID, toolAttribution{
		PlanRevision: record.PlanRevision, PlanStepID: record.PlanStepID,
	})
	if err := journal.recordModelRequest(ctx, record.ModelRequestID); err != nil {
		return journal.note(err)
	}
	// The authority decision is deliberately not carried here. The router
	// resolves this run's authority from policy without recording a decision
	// row, so naming its identity would point the record at a decision that
	// does not exist — which the store rejects, and rightly: a tool record
	// citing an unrecorded approval is worse than one citing none.
	var decision *string
	_, err := journal.repositories.RecordAgentToolRequest(
		ctx, storage.RecordAgentToolRequest{
			ID:                    record.RequestID,
			TaskID:                record.TaskID,
			RunID:                 record.RunID,
			PlanRevision:          record.PlanRevision,
			PlanStepID:            record.PlanStepID,
			ModelRequestID:        record.ModelRequestID,
			ToolName:              string(record.ToolName),
			ToolSchemaVersion:     uint64(record.ToolSchemaVersion),
			ArgumentsRedactedJSON: record.ArgumentsRedactedJSON,
			ArgumentsSHA256:       record.ArgumentsSHA256,
			PermissionDecisionID:  decision,
			IdempotencyKey: agentExecutionKey(
				"agent-tool-start-", record.RunID.String(), record.RequestID),
		})
	if err != nil {
		return journal.note(fmt.Errorf("record the tool request: %w", err))
	}
	return nil
}

// PersistToolResult records what the tool returned.
func (journal *agentToolJournal) PersistToolResult(
	ctx context.Context,
	record agentloop.ToolResultRecord,
) error {
	if journal == nil || journal.repositories == nil {
		return nil
	}
	body, err := json.Marshal(record.Result)
	if err != nil {
		return fmt.Errorf("encode the tool result: %w", err)
	}
	digest := strings.TrimSpace(record.ResultSHA256)
	if digest == "" {
		sum := sha256.Sum256(body)
		digest = hex.EncodeToString(sum[:])
	}
	resultID, err := domain.NewEventID()
	if err != nil {
		return err
	}
	_, err = journal.repositories.RecordAgentToolResult(
		ctx, storage.RecordAgentToolResult{
			ID:                 resultID.String(),
			ToolRequestID:      record.RequestID,
			State:              agentToolResultState(record.Result),
			ResultRedactedJSON: string(body),
			ResultSHA256:       digest,
			IdempotencyKey: agentExecutionKey(
				"agent-tool-result-", record.RunID.String(), record.RequestID),
		})
	if err != nil {
		return journal.note(fmt.Errorf("record the tool result: %w", err))
	}
	return nil
}

// remember stores the plan step one request was made for.
func (journal *agentToolJournal) remember(
	requestID string,
	attribution toolAttribution,
) {
	if strings.TrimSpace(requestID) == "" {
		return
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	journal.attribution[requestID] = attribution
}

// attributionOf reports the plan step one tool request belongs to.
func (journal *agentToolJournal) attributionOf(
	requestID string,
) (toolAttribution, bool) {
	if journal == nil {
		return toolAttribution{}, false
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	found, present := journal.attribution[requestID]
	return found, present
}

// agentToolResultState maps a tool result onto the closed durable vocabulary.
func agentToolResultState(result executor.ToolResult) storage.AgentToolResultState {
	switch {
	case result.Cancelled:
		return storage.AgentToolResultCancelled
	case result.TimedOut:
		// A tool that ran out of time may or may not have had its effect. The
		// vocabulary has a word for exactly that, and calling it a failure
		// would claim knowledge nobody has.
		return storage.AgentToolResultOutcomeUnknown
	case result.ExitCode == 0:
		return storage.AgentToolResultSucceeded
	default:
		return storage.AgentToolResultFailed
	}
}
