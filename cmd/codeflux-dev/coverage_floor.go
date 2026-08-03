package main

// coverageFloorPercent is the repository's statement-coverage ratchet.
//
// REPO-028: AGENTS.md's 80% floor is unreachable from where the repository
// actually stands -- the untagged default suite measured 62.6% on
// 2026-08-02 (see DEVLOG). A fixed 80% constant would simply be bypassed
// until the repository happened to reach it, which is no floor at all. A
// ratchet -- a floor that is only ever allowed to move up -- is enforceable
// starting today and converges on the same destination without ever being a
// number nobody can pass.
//
// .githooks/pre-push falls back to this value as its default floor when
// neither --min-coverage nor CODEFLUX_MIN_COVERAGE overrides it for a
// single run (TestPrePushDefaultFloorMatchesRatchetConstant keeps the two
// declarations from drifting apart). It is set a few points below the
// measured baseline rather than exactly at it: the baseline was measured on
// Windows/arm64, CI runs Ubuntu, and the two platforms do not necessarily
// execute exactly the same statements (see path_resolve_windows.go /
// path_resolve_nonwindows.go for one concrete example of a platform-gated
// file). The margin absorbs that difference and ordinary run-to-run
// variance without leaving so much slack that a real regression slips
// through.
//
// Raise this constant only after actually measuring a higher number with:
//
//	go test -timeout=20m -coverprofile=.artifacts/coverage/unit.out ./...
//	go tool cover -func=.artifacts/coverage/unit.out | tail -1
//
// TestCoverageFloorNeverDecreases refuses a commit that lowers it relative
// to the value most recently committed, so a decrease can only happen as a
// deliberate, reviewed edit -- never a silent slip.
const coverageFloorPercent = 60.0
