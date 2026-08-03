package pipeline

// DeclineClass says why a stage that recorded skipped or not-implemented
// established nothing.
//
// The distinction is the whole point of the skip audit (PIPE-042). A figure
// that lumps a check which correctly declined together with a stage nobody has
// built makes an honest run look evasive, and teaches people to stop declining
// honestly. Anything reporting the audit has to keep the three apart.
type DeclineClass string

const (
	// DeclineUnimplemented means nothing established anything for this stage in
	// this run, and no declared check accounts for that.
	DeclineUnimplemented DeclineClass = "unimplemented"
	// DeclinePrincipled means a check examined this run's own facts and
	// legitimately declined, or the stage is one docs/plan.md §33 records as
	// resolved by making it unable to claim.
	DeclinePrincipled DeclineClass = "principled-decline"
	// DeclineNoCheckByDesign means nothing performs this stage because nothing
	// can: human acceptance and delivery are decisions a run cannot make for
	// itself.
	DeclineNoCheckByDesign DeclineClass = "no-check-by-design"
	// DeclineByProfile means the run's declared RunProfile marked this stage
	// inapplicable before the run started (PIPE-046). It takes priority over
	// every other classification: a stage a profile declines was never asked
	// to run, so whatever its own check would otherwise have said about it —
	// principled decline, no-check-by-design, or unimplemented — is beside
	// the point. docs/plan.md §33 calls this the difference between a
	// decision and an excuse, and this is the decision.
	DeclineByProfile DeclineClass = "declined-by-profile"
)

// ClassifyDecline decides which of the three a non-satisfying stage is.
//
// It lives here, rather than beside either of its callers, because it has two:
// the coordinator computes it when writing the audit, and the browser recomputes
// it from the wire, which carries the stage's state but not the classification.
// Two copies of this rule drifted apart the day the second one was written --
// one treated a stage with no declared check as unimplemented and the other
// folded it in with the by-design cases, so the same ledger classified
// differently depending on who was reading it. One implementation is the fix;
// keeping the copies in step by discipline is what failed.
//
// It takes the stage number and state rather than a record type, so neither
// caller has to depend on the other's storage or wire shape.
//
// Ticket is the governing TODO when one applies, and empty otherwise. It is
// returned separately from the class because a reader wants both: the class
// says what kind of gap this is, the ticket says who owns closing it.
func ClassifyDecline(stage Number, state State) (class DeclineClass, ticket string) {
	check, bound := CheckFor(stage)
	if !bound {
		// No declared check at all. That is not a decline anyone made — it is a
		// stage the flow gained without a performer, which ValidateChecks exists
		// to catch and which should never reach a reader as "by design".
		return DeclineUnimplemented, ""
	}
	if !check.MaySatisfy {
		if resolved, ok := Section33Resolved[stage]; ok {
			return DeclinePrincipled, resolved
		}
		return DeclineNoCheckByDesign, ""
	}
	if state == StateSkipped {
		return DeclinePrincipled, check.Unestablished
	}
	return DeclineUnimplemented, ""
}

// ClassifyDeclineForProfile is ClassifyDecline, extended with the run's
// declared profile (PIPE-046).
//
// A stage the profile declines classifies as DeclineByProfile regardless of
// what ClassifyDecline alone would have said about it and regardless of
// whether this run's ledger recorded it skipped or not-implemented: the
// profile decided the stage did not apply before the run reached it, so the
// state the run happened to record for an inapplicable stage carries no
// further information. Every other stage falls through to ClassifyDecline
// unchanged, which is what keeps this an additive extension rather than a
// second, divergent rule: a caller passing ProfileDefault, which declines
// nothing, gets exactly ClassifyDecline's answer for every stage.
func ClassifyDeclineForProfile(
	stage Number,
	state State,
	profile RunProfile,
) (class DeclineClass, ticket string) {
	if reason, declined := profile.DeclinedReason(stage); declined {
		return DeclineByProfile, reason
	}
	return ClassifyDecline(stage, state)
}
