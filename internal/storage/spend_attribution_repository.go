package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
)

// SpendSlice is the usage and cost attributed to one grouping key.
//
// Known and unknown are kept apart everywhere. A slice reports the subtotal it
// could price and, separately, how many calls it could not, so a reader sees
// "at least this much, plus four calls nobody could price" rather than a total
// that quietly treats unpriced work as free.
type SpendSlice struct {
	Calls             Count
	InputTokens       Count
	CachedInputTokens Count
	CacheWriteTokens  Count
	OutputTokens      Count
	ReasoningTokens   Count
	// KnownCost is the exact subtotal of the priced calls in this slice.
	// CostKnown is false when the slice's priced calls disagree on currency,
	// because minor units of different currencies do not add up.
	KnownCost         ExactMinorCost
	CostKnown         bool
	UsageUnknownCount Count
	CostUnknownCount  Count
}

// StageSpend is what one stage of the flow cost.
type StageSpend struct {
	Stage pipeline.Number
	Name  string
	Phase pipeline.Phase
	Spend SpendSlice
}

// PhaseSpend is what one movement of the flow cost: the atoms, the molecules,
// the program.
type PhaseSpend struct {
	Phase pipeline.Phase
	Spend SpendSlice
}

// ModelSpend is what one model cost, across every stage that used it.
type ModelSpend struct {
	ProviderID   string
	Model        string
	ModelVersion string
	Spend        SpendSlice
}

// SpendAttribution is one window's spend, sliced the three ways a person asks
// about it: which part of the flow, which stage, and which model.
//
// Attribution to a stage is an approximation and is labelled as one. Provider
// calls do not record the stage that made them, so each call is attributed to
// the first stage whose record was written at or after the call — that is, the
// next stage to finish, which is the stage that was running. The approximation
// degrades if stages are recorded out of order and becomes exact containment
// once PIPE-006 records real start and finish times instead of writing one
// timestamp into both columns. Totals never depend on it: Total is summed from
// the calls themselves, so Unattributed spend is missing from ByStage and
// ByPhase but never from Total.
type SpendAttribution struct {
	Window MetricsWindow
	Total  SpendSlice
	// ByPhase and ByStage carry only the calls a stage claimed, in flow order.
	ByPhase []PhaseSpend
	ByStage []StageSpend
	// ByModel is exact rather than approximated: the model is recorded on the
	// request itself. It is ordered by descending known cost, then by name.
	ByModel []ModelSpend
	// Unattributed is the calls no stage claimed, usually because the task
	// recorded no stage after them. Reported rather than dropped, so ByPhase
	// summing to less than Total is explained instead of merely observed.
	Unattributed SpendSlice
}

// AttributeSpend slices one window's recorded provider spend by flow phase,
// stage, and model.
//
// Codeflux atom documentation (schema v1):
//
//	Purpose:
//	  Answer "what did the atoms cost, what did the molecules cost, what did
//	  the program cost" from the recorded provider ledger, so a person can see
//	  where a task's money went rather than only how much of it went.
//	Use when:
//	  Reporting spend for a closed time window from the local database.
//	Do not use when:
//	  A caller needs live per-request cost during a run; subscribe to session
//	  cost events instead. Do not use it to enforce a budget: the budget ledger
//	  reserves and commits against the authoritative limit, and this is a
//	  read-only summary that lags a call by the width of its own window.
//	Semantics:
//	  Each physical attempt contributes once, through its highest-sequence
//	  accounting row. Stage attribution is approximate and documented on
//	  SpendAttribution. Totals are independent of attribution.
//	Inputs:
//	  - window: a closed UTC interval. Both bounds are required.
//	Outputs:
//	  - A SpendAttribution whose Total covers every settled call in the window
//	    and whose ByPhase, ByStage, and Unattributed partition that same set.
//	Preconditions:
//	  - The operational schema is migrated.
//	Postconditions:
//	  - No slice reports a cost across two currencies as a single figure.
//	Effects:
//	  - Reads SQLite. No mutation.
//	Failure semantics:
//	  - An invalid window is rejected. A malformed stored cost is an error
//	    rather than a silently dropped row.
//	Determinism:
//	  Deterministic for a fixed database and window.
//	Idempotency and retry:
//	  Pure read; safe to retry.
//	Reconciliation and compensation:
//	  None: the provider accounting ledger is the reconciled record.
//	Security and privacy:
//	  Returns counts, identifiers, and money only. No prompt, completion, tool
//	  output, or repository content passes through it.
//	Dependencies and bindings:
//	  provider_attempt_accounting, provider_request_attempts,
//	  provider_logical_requests, and pipeline_stage_records.
//	Complexity and limits:
//	  One grouped scan of the accounting table plus one correlated stage lookup
//	  per group. Bounded by the local prototype database.
//	Examples:
//	  - A window over one task reports the atom phase at two thirds of a minor
//	    unit across eleven stages. A window with no call reports an exact zero,
//	    not an unknown.
//	Verification:
//	  internal/storage/spend_attribution_repository_test.go.
//	Retrieval concepts:
//	  spend by phase, cost per atom, cost per molecule, cost per program,
//	  token cost attribution, model spend breakdown.
//
//codeflux:atom
func (repositories *Repositories) AttributeSpend(
	ctx context.Context,
	window MetricsWindow,
) (SpendAttribution, error) {
	if err := window.Validate(); err != nil {
		return SpendAttribution{}, err
	}
	from, to := window.bounds()
	result := SpendAttribution{Window: window}

	rows, err := repositories.database.sql.QueryContext(
		ctx, attributedSpendQuery, from, to,
	)
	if err != nil {
		return SpendAttribution{}, classify("read attributed spend", err)
	}
	defer func() { _ = rows.Close() }()

	total := newSpendAccumulator()
	unattributed := newSpendAccumulator()
	byStage := map[pipeline.Number]*spendAccumulator{}
	byPhase := map[pipeline.Phase]*spendAccumulator{}
	byModel := map[modelKey]*spendAccumulator{}

	for rows.Next() {
		var group attributedSpendGroup
		if err := group.scan(rows); err != nil {
			return SpendAttribution{}, err
		}
		total.add(group)
		accumulatorFor(byModel, group.modelIdentity()).add(group)

		stage, phase, attributed := group.flowPosition()
		if !attributed {
			unattributed.add(group)
			continue
		}
		accumulatorFor(byStage, stage).add(group)
		accumulatorFor(byPhase, phase).add(group)
	}
	if err := rows.Err(); err != nil {
		return SpendAttribution{}, classify("iterate attributed spend", err)
	}

	if result.Total, err = total.finalise(); err != nil {
		return SpendAttribution{}, err
	}
	if result.Unattributed, err = unattributed.finalise(); err != nil {
		return SpendAttribution{}, err
	}
	if result.ByStage, err = stageSpends(byStage); err != nil {
		return SpendAttribution{}, err
	}
	if result.ByPhase, err = phaseSpends(byPhase); err != nil {
		return SpendAttribution{}, err
	}
	if result.ByModel, err = modelSpends(byModel); err != nil {
		return SpendAttribution{}, err
	}
	return result, nil
}

// attributedSpendQuery groups settled attempts by stage, model, and price
// shape.
//
// The stage is found with a correlated scalar subquery rather than a join, so
// a call can match at most one stage however the recorded intervals fall. A
// join would silently double-count a call that two stage rows both claimed.
const attributedSpendQuery = `
	WITH settled AS (
		SELECT
			attempt_id, usage_known, input_tokens, cached_input_tokens,
			cache_write_tokens, output_tokens, reasoning_tokens,
			cost_known, cost_minor_numerator, cost_minor_denominator,
			currency, created_at_unix_micros,
			ROW_NUMBER() OVER (
				PARTITION BY attempt_id ORDER BY sequence DESC
			) AS recency
		FROM provider_attempt_accounting
	),
	attributed AS (
		SELECT
			settled.usage_known, settled.input_tokens,
			settled.cached_input_tokens, settled.cache_write_tokens,
			settled.output_tokens, settled.reasoning_tokens,
			settled.cost_known, settled.cost_minor_numerator,
			settled.cost_minor_denominator, settled.currency,
			request.provider_id, request.model_identifier,
			request.model_version,
			(
				SELECT stage.stage_number
				FROM pipeline_stage_records AS stage
				WHERE stage.task_id = request.task_id
				  AND stage.finished_at_unix_micros
				      >= settled.created_at_unix_micros
				ORDER BY stage.finished_at_unix_micros ASC,
				         stage.stage_number ASC
				LIMIT 1
			) AS stage_number
		FROM settled
		JOIN provider_request_attempts AS attempt
		  ON attempt.id = settled.attempt_id
		JOIN provider_logical_requests AS request
		  ON request.id = attempt.logical_request_id
		WHERE settled.recency = 1
		  AND settled.created_at_unix_micros BETWEEN ? AND ?
	)
	SELECT
		stage_number, provider_id, model_identifier, model_version,
		currency, cost_minor_denominator,
		COALESCE(sum(CASE WHEN usage_known = 1 THEN input_tokens ELSE 0 END), 0),
		COALESCE(sum(CASE WHEN usage_known = 1 THEN cached_input_tokens ELSE 0 END), 0),
		COALESCE(sum(CASE WHEN usage_known = 1 THEN cache_write_tokens ELSE 0 END), 0),
		COALESCE(sum(CASE WHEN usage_known = 1 THEN output_tokens ELSE 0 END), 0),
		COALESCE(sum(CASE WHEN usage_known = 1 THEN reasoning_tokens ELSE 0 END), 0),
		COALESCE(sum(CASE WHEN cost_known = 1 THEN cost_minor_numerator ELSE 0 END), 0),
		COALESCE(sum(CASE WHEN usage_known = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(sum(CASE WHEN cost_known = 0 THEN 1 ELSE 0 END), 0),
		count(*)
	FROM attributed
	GROUP BY
		stage_number, provider_id, model_identifier, model_version,
		currency, cost_minor_denominator`

type modelKey struct {
	providerID   string
	model        string
	modelVersion string
}

// attributedSpendGroup is one row of the grouped query: every call sharing a
// stage, a model, and a price shape.
type attributedSpendGroup struct {
	stageNumber sql.NullInt64
	providerID  string
	model       string
	version     string
	currency    sql.NullString
	denominator sql.NullInt64

	input       int64
	cachedInput int64
	cacheWrite  int64
	output      int64
	reasoning   int64
	numerator   int64

	usageUnknown int64
	costUnknown  int64
	calls        int64
}

func (group *attributedSpendGroup) scan(rows *sql.Rows) error {
	if err := rows.Scan(
		&group.stageNumber, &group.providerID, &group.model, &group.version,
		&group.currency, &group.denominator,
		&group.input, &group.cachedInput, &group.cacheWrite, &group.output,
		&group.reasoning, &group.numerator,
		&group.usageUnknown, &group.costUnknown, &group.calls,
	); err != nil {
		return classify("scan attributed spend", err)
	}
	return nil
}

func (group attributedSpendGroup) modelIdentity() modelKey {
	return modelKey{
		providerID:   group.providerID,
		model:        group.model,
		modelVersion: group.version,
	}
}

// flowPosition reports the stage and phase this group belongs to. A group with
// no stage, or one outside the flow, is unattributed rather than defaulted.
func (group attributedSpendGroup) flowPosition() (
	pipeline.Number, pipeline.Phase, bool,
) {
	if !group.stageNumber.Valid {
		return 0, "", false
	}
	stage := pipeline.Number(group.stageNumber.Int64)
	phase, found := pipeline.PhaseOf(stage)
	if !found {
		return 0, "", false
	}
	return stage, phase, true
}

// spendAccumulator adds exact rational money without ever holding a float.
type spendAccumulator struct {
	counts   SpendSlice
	cost     *big.Rat
	currency string
	mixed    bool
	priced   bool
}

func newSpendAccumulator() *spendAccumulator {
	return &spendAccumulator{cost: new(big.Rat)}
}

// accumulatorFor returns the accumulator for one grouping key, creating it on
// first use. Indexing the map directly would yield a nil pointer for a key not
// yet seen, which is every key the first time it appears.
func accumulatorFor[Key comparable](
	accumulators map[Key]*spendAccumulator,
	key Key,
) *spendAccumulator {
	accumulator, found := accumulators[key]
	if !found {
		accumulator = newSpendAccumulator()
		accumulators[key] = accumulator
	}
	return accumulator
}

func (accumulator *spendAccumulator) add(group attributedSpendGroup) {
	accumulator.counts.InputTokens.Value += group.input
	accumulator.counts.CachedInputTokens.Value += group.cachedInput
	accumulator.counts.CacheWriteTokens.Value += group.cacheWrite
	accumulator.counts.OutputTokens.Value += group.output
	accumulator.counts.ReasoningTokens.Value += group.reasoning
	accumulator.counts.UsageUnknownCount.Value += group.usageUnknown
	accumulator.counts.CostUnknownCount.Value += group.costUnknown
	accumulator.counts.Calls.Value += group.calls

	if !group.currency.Valid || !group.denominator.Valid ||
		group.denominator.Int64 <= 0 || group.numerator == 0 {
		return
	}
	accumulator.priced = true
	switch {
	case accumulator.currency == "":
		accumulator.currency = group.currency.String
	case accumulator.currency != group.currency.String:
		accumulator.mixed = true
	}
	accumulator.cost.Add(accumulator.cost, new(big.Rat).SetFrac(
		big.NewInt(group.numerator), big.NewInt(group.denominator.Int64)))
}

// slice finalises the accumulated counts into a reportable slice.
func (accumulator *spendAccumulator) finalise() (SpendSlice, error) {
	result := accumulator.counts
	for _, count := range []*Count{
		&result.Calls, &result.InputTokens, &result.CachedInputTokens,
		&result.CacheWriteTokens, &result.OutputTokens, &result.ReasoningTokens,
		&result.UsageUnknownCount, &result.CostUnknownCount,
	} {
		count.Known = true
	}
	if accumulator.mixed {
		return result, nil
	}
	if !accumulator.priced {
		// Nothing priced in this slice. That is an exact zero when no call was
		// made, and the unpriced calls are already counted separately.
		result.CostKnown = true
		return result, nil
	}
	if !accumulator.cost.Num().IsInt64() || !accumulator.cost.Denom().IsInt64() {
		return result, nil
	}
	exact, err := normalizeExactCost(ExactMinorCost{
		Numerator:   accumulator.cost.Num().Int64(),
		Denominator: accumulator.cost.Denom().Int64(),
		Currency:    domain.CurrencyCode(accumulator.currency),
	})
	if err != nil {
		return SpendSlice{}, fmt.Errorf("attributed spend cost: %w", err)
	}
	result.KnownCost = exact
	result.CostKnown = true
	return result, nil
}

func stageSpends(
	accumulators map[pipeline.Number]*spendAccumulator,
) ([]StageSpend, error) {
	spends := make([]StageSpend, 0, len(accumulators))
	for number, accumulator := range accumulators {
		stage, found := pipeline.StageByNumber(number)
		if !found {
			return nil, fmt.Errorf("attributed spend names unknown stage %d", number)
		}
		phase, found := pipeline.PhaseOf(number)
		if !found {
			return nil, fmt.Errorf("stage %d belongs to no phase", number)
		}
		slice, err := accumulator.finalise()
		if err != nil {
			return nil, err
		}
		spends = append(spends, StageSpend{
			Stage: number, Name: stage.Name, Phase: phase, Spend: slice,
		})
	}
	sort.Slice(spends, func(left, right int) bool {
		return spends[left].Stage < spends[right].Stage
	})
	return spends, nil
}

func phaseSpends(
	accumulators map[pipeline.Phase]*spendAccumulator,
) ([]PhaseSpend, error) {
	spends := make([]PhaseSpend, 0, len(accumulators))
	// Built from the ordered phase list so the result reads in flow order
	// rather than in map order.
	for _, phase := range pipeline.Phases {
		accumulator, found := accumulators[phase]
		if !found {
			continue
		}
		slice, err := accumulator.finalise()
		if err != nil {
			return nil, err
		}
		spends = append(spends, PhaseSpend{Phase: phase, Spend: slice})
	}
	return spends, nil
}

func modelSpends(
	accumulators map[modelKey]*spendAccumulator,
) ([]ModelSpend, error) {
	spends := make([]ModelSpend, 0, len(accumulators))
	for key, accumulator := range accumulators {
		slice, err := accumulator.finalise()
		if err != nil {
			return nil, err
		}
		spends = append(spends, ModelSpend{
			ProviderID:   key.providerID,
			Model:        key.model,
			ModelVersion: key.modelVersion,
			Spend:        slice,
		})
	}
	// Most expensive first, because that is the question a person opens this
	// view to answer. Ties break on name so the order is stable.
	sort.Slice(spends, func(left, right int) bool {
		leftCost := ratOf(spends[left].Spend.KnownCost)
		rightCost := ratOf(spends[right].Spend.KnownCost)
		if compared := rightCost.Cmp(leftCost); compared != 0 {
			return compared < 0
		}
		if spends[left].Model != spends[right].Model {
			return spends[left].Model < spends[right].Model
		}
		return spends[left].ModelVersion < spends[right].ModelVersion
	})
	return spends, nil
}

func ratOf(cost ExactMinorCost) *big.Rat {
	if cost.Denominator <= 0 {
		return new(big.Rat)
	}
	return new(big.Rat).SetFrac(
		big.NewInt(cost.Numerator), big.NewInt(cost.Denominator))
}
