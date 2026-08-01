//go:build js && wasm

package main

import (
	"context"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/shell"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// decorateMountedReviewDecisions makes the four review decisions live.
//
// They were presented as permanently unavailable against a coordinator that
// implements all four. This is the wiring that closes that gap; everything it
// decides lives in review_mutations.go, which is testable natively.
func decorateMountedReviewDecisions(
	decisions shell.ReviewDecisionProps,
	scope reviewMutationScope,
	state ui.State[mountedReviewMutationState],
	ref ui.Ref[mountedReviewMutationState],
	reload func(),
) shell.ReviewDecisionProps {
	return bindMountedReviewDecisions(
		decisions, scope, state.Get(),
		func(kind mountedReviewMutationKind) {
			beginMountedReviewDecision(kind, scope, state, ref, reload)
		},
	)
}

func beginMountedReviewDecision(
	kind mountedReviewMutationKind,
	scope reviewMutationScope,
	state ui.State[mountedReviewMutationState],
	ref ui.Ref[mountedReviewMutationState],
	reload func(),
) {
	current := ref.Get()
	current, started := prepareMountedReviewMutation(
		current, kind, scope.ReviewRevision, composer.NewIdempotencyKey)
	if !started {
		return
	}
	ref.Set(current)
	state.Set(current)
	ui.SafeGo("execute authoritative review decision", func() {
		// A review decision applies a Git change, so it is given more room
		// than an ordinary control command: timing out early would leave the
		// outcome unknown for something that may well have committed.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		committed, err := executeMountedReviewDecision(ctx, kind, scope, string(current.Key))
		ui.PostAsync(func() {
			next := settleMountedReviewMutation(current, committed, err)
			ref.Set(next)
			state.Set(next)
			if reload != nil {
				reload()
			}
		})
	})
}

func executeMountedReviewDecision(
	ctx context.Context,
	kind mountedReviewMutationKind,
	scope reviewMutationScope,
	key string,
) (bool, error) {
	connection, err := grpctunnel.DialContext(
		ctx,
		sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return false, err
	}
	defer connection.Close()
	client := reviewMutationClient(codefluxv1.NewReviewServiceClient(connection))
	return executeMountedReviewMutationWithClient(ctx, client, kind, scope, key)
}

// mountedReviewDecisionBinder returns the binder the timeline hands to
// bindAuthoritativeTimelineActions.
//
// A nil scope produces a nil binder, which leaves every projected refusal in
// place. That is deliberate: without the loaded review identity there is
// nothing safe to send, and showing an enabled control that could only fail
// would be worse than showing why it is unavailable.
func mountedReviewDecisionBinder(
	scope reviewMutationScope,
	state ui.State[mountedReviewMutationState],
	ref ui.Ref[mountedReviewMutationState],
	reload func(),
) reviewDecisionBinder {
	if !scope.Complete() {
		return nil
	}
	return func(decisions shell.ReviewDecisionProps) shell.ReviewDecisionProps {
		return decorateMountedReviewDecisions(decisions, scope, state, ref, reload)
	}
}
