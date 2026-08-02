//go:build js && wasm

package main

import (
	"context"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/codecollection"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// openBrowserCollectionConnection dials the coordinator over the session
// bridge.
func openBrowserCollectionConnection(ctx context.Context) (*grpc.ClientConn, error) {
	return grpctunnel.DialContext(
		ctx, sessionclient.BridgePath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// listMountedCodePackages asks which packages a repository contains.
func listMountedCodePackages(
	ctx context.Context,
	repositoryID string,
) (codeCollectionAnswer, error) {
	connection, err := openBrowserCollectionConnection(ctx)
	if err != nil {
		return codeCollectionAnswer{}, err
	}
	defer func() { _ = connection.Close() }()
	response, err := codefluxv1.NewCodeCollectionServiceClient(connection).ListCodePackages(
		ctx,
		&codefluxv1.ListCodePackagesRequest{
			RepositoryId: repositoryIdentityFor(repositoryID),
		},
	)
	if err != nil {
		return codeCollectionAnswer{}, err
	}
	return projectCodePackages(response), nil
}

// listMountedCodeSymbols asks which declarations match a filter.
func listMountedCodeSymbols(
	ctx context.Context,
	repositoryID string,
	filter mountedSymbolFilter,
) (codeSymbolAnswer, error) {
	connection, err := openBrowserCollectionConnection(ctx)
	if err != nil {
		return codeSymbolAnswer{}, err
	}
	defer func() { _ = connection.Close() }()
	response, err := codefluxv1.NewCodeCollectionServiceClient(connection).ListCodeSymbols(
		ctx,
		&codefluxv1.ListCodeSymbolsRequest{
			RepositoryId: repositoryIdentityFor(repositoryID),
			ImportPath:   filter.ImportPath,
			Search:       filter.Search,
			ExportedOnly: filter.ExportedOnly,
			AtomsOnly:    filter.AtomsOnly,
		},
	)
	if err != nil {
		return codeSymbolAnswer{}, err
	}
	return projectCodeSymbols(response), nil
}

// inspectMountedCodeSymbol reads one declaration closely.
func inspectMountedCodeSymbol(
	ctx context.Context,
	repositoryID string,
	key string,
) (codecollection.Detail, error) {
	connection, err := openBrowserCollectionConnection(ctx)
	if err != nil {
		return codecollection.Detail{}, err
	}
	defer func() { _ = connection.Close() }()
	response, err := codefluxv1.NewCodeCollectionServiceClient(connection).InspectCodeSymbol(
		ctx,
		&codefluxv1.InspectCodeSymbolRequest{
			RepositoryId: repositoryIdentityFor(repositoryID), Key: key,
		},
	)
	if err != nil {
		return codecollection.Detail{}, err
	}
	return projectCodeSymbolDetail(response), nil
}
