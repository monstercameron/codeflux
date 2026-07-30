package fixture

// ValidateOtherBudget checks a task limit before provider work begins.
//
// Codeflux atom documentation (schema v1):
//
//	Purpose:
//	  Prevent a model request from exceeding the task's approved budget.
//	Use when:
//	  A task is ready to issue a metered model request.
//	Do not use when:
//	  The operation is local and has no metered provider cost.
//	Semantics:
//	  Compare the requested allowance with the task's remaining approved limit.
//	Inputs:
//	  - Task identity, expected revision, and requested token allowance.
//	Outputs:
//	  - An allowed result or a typed insufficient-budget result.
//	Preconditions:
//	  - The caller supplies a current task budget revision.
//	Postconditions:
//	  - An allowed result identifies the checked revision and allowance.
//	Effects:
//	  None: validation reads immutable input values only.
//	Failure semantics:
//	  Reject stale revisions and insufficient allowance without partial work.
//	Determinism:
//	  Equal inputs produce the same result without time dependencies.
//	Idempotency and retry:
//	  Repeating an identical check has no additional effect.
//	Reconciliation and compensation:
//	  None: no external state requires corrective action.
//	Security and privacy:
//	  Accept only task-scoped identifiers and never log provider credentials.
//	Dependencies and bindings:
//	  Depend on the versioned task-budget value contract.
//	Complexity and limits:
//	  Complete in constant time with bounded input sizes.
//	Examples:
//	  - Allow a request within budget; reject a request above the remaining limit.
//	Verification:
//	  Unit cases cover allowed, stale, and insufficient-budget outcomes.
//	Retrieval concepts:
//	  Task budget validation, model request allowance, spending guard.
//
//codeflux:atom
func ValidateTaskBudgetBeforeModelRequest()
