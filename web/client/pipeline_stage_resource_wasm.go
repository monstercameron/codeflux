//go:build js && wasm

package main

import (
	"context"
	"errors"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/pipelineledger"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	errPipelineStageResourceScopeUnavailable  = errors.New("authoritative pipeline stage scope is unavailable")
	errPipelineStageResourceBridgeUnavailable = errors.New("authoritative pipeline stage bridge is unavailable")
)

type pipelineStageResourceClient interface {
	ListPipelineStages(context.Context, *codefluxv1.ListPipelineStagesRequest, ...grpc.CallOption) (*codefluxv1.ListPipelineStagesResponse, error)
}

type pipelineStageResourceLease struct {
	client pipelineStageResourceClient
	close  func() error
}

type pipelineStageResourceClientOpener func(context.Context) (pipelineStageResourceLease, error)

// pipelineStageResource is what useMountedPipelineLedger renders from: the
// decoded rows plus the attempt the coordinator actually resolved, since a
// caller that asked for attempt zero must be able to see which attempt it
// got (PIPE-006b's own ListPipelineStagesResponse.attempt exists for this).
type pipelineStageResource struct {
	Rows    []pipelineledger.StageRow
	Attempt uint64
}

// loadPipelineStageResource reads one task attempt's recorded ledger over
// PipelineStageService.ListPipelineStages (PIPE-006a).
//
// attempt zero asks the application layer to resolve its own default, not
// "the latest attempt" -- there is no such concept in this product yet
// (PIPE-006b's own documented limitation) -- so this surface does not invent
// one either.
func loadPipelineStageResource(
	ctx context.Context,
	opener pipelineStageResourceClientOpener,
	taskID domain.TaskID,
	attempt uint64,
) (pipelineStageResource, error) {
	if opener == nil || taskID.IsZero() {
		return pipelineStageResource{}, errPipelineStageResourceScopeUnavailable
	}
	lease, err := opener(ctx)
	if err != nil {
		return pipelineStageResource{}, err
	}
	if lease.client == nil || lease.close == nil {
		return pipelineStageResource{}, errPipelineStageResourceBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.ListPipelineStages(ctx, &codefluxv1.ListPipelineStagesRequest{
		TaskId: taskIdentity(taskID), Attempt: attempt,
	})
	if err != nil {
		return pipelineStageResource{}, err
	}
	rows, resolvedAttempt, err := decodePipelineStageRows(response)
	if err != nil {
		return pipelineStageResource{}, err
	}
	return pipelineStageResource{Rows: rows, Attempt: resolvedAttempt}, nil
}

func openBrowserPipelineStageResourceClient(ctx context.Context) (pipelineStageResourceLease, error) {
	connection, err := grpctunnel.DialContext(ctx, sessionclient.BridgePath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return pipelineStageResourceLease{}, err
	}
	return pipelineStageResourceLease{
		client: codefluxv1.NewPipelineStageServiceClient(connection), close: connection.Close,
	}, nil
}
