package transport

import (
	"context"
	"errors"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CodeCollectionService serves the directory of a repository's code.
//
// It is read-only by construction: the application behind it has no writing
// method to call. No response carries a declaration's body, because this is a
// directory of what the collection offers rather than an editor.
type CodeCollectionService struct {
	codefluxv1.UnimplementedCodeCollectionServiceServer
	application CodeCollectionApplication
}

// NewCodeCollectionService builds the service.
func NewCodeCollectionService(
	application CodeCollectionApplication,
) (*CodeCollectionService, error) {
	if application == nil {
		return nil, errors.New("code collection application is required")
	}
	return &CodeCollectionService{application: application}, nil
}

// ListCodePackages returns the packages the repository contains.
func (service *CodeCollectionService) ListCodePackages(
	ctx context.Context,
	request *codefluxv1.ListCodePackagesRequest,
) (*codefluxv1.ListCodePackagesResponse, error) {
	repositoryID, err := RepositoryIDFromProto(request.GetRepositoryId())
	if err != nil {
		return nil, &RequestValidationError{
			Field: "repository_id", Reason: "must be a repository identity",
		}
	}
	page, err := service.application.ListCodePackages(ctx, CodeCollectionQuery{
		RepositoryID: repositoryID,
		Search:       request.GetSearch(),
		Limit:        int(request.GetPage().GetLimit()),
	})
	if err != nil {
		return nil, mapCodeCollectionError(err, "the code collection could not be read")
	}
	views := make([]*codefluxv1.CodePackageView, 0, len(page.Packages))
	for _, record := range page.Packages {
		views = append(views, &codefluxv1.CodePackageView{
			ImportPath:    record.ImportPath,
			Name:          record.Name,
			ModulePath:    record.ModulePath,
			FileCount:     record.FileCount,
			TestFileCount: record.TestCount,
			SymbolCount:   record.SymbolCount,
			AtomCount:     record.AtomCount,
		})
	}
	return &codefluxv1.ListCodePackagesResponse{
		Revision:      codeCollectionRevisionToProto(page.Revision),
		Packages:      views,
		Page:          &codefluxv1.PageInfo{HasMore: page.Truncated},
		TotalPackages: page.TotalPackages,
		TotalSymbols:  page.TotalSymbols,
		TotalAtoms:    page.TotalAtoms,
	}, nil
}

// ListCodeSymbols returns the declarations matching a query.
func (service *CodeCollectionService) ListCodeSymbols(
	ctx context.Context,
	request *codefluxv1.ListCodeSymbolsRequest,
) (*codefluxv1.ListCodeSymbolsResponse, error) {
	repositoryID, err := RepositoryIDFromProto(request.GetRepositoryId())
	if err != nil {
		return nil, &RequestValidationError{
			Field: "repository_id", Reason: "must be a repository identity",
		}
	}
	page, err := service.application.ListCodeSymbols(ctx, CodeSymbolQuery{
		RepositoryID: repositoryID,
		ImportPath:   request.GetImportPath(),
		Search:       request.GetSearch(),
		ExportedOnly: request.GetExportedOnly(),
		AtomsOnly:    request.GetAtomsOnly(),
		Limit:        int(request.GetPage().GetLimit()),
	})
	if err != nil {
		return nil, mapCodeCollectionError(err, "the code collection could not be read")
	}
	views := make([]*codefluxv1.CodeSymbolView, 0, len(page.Symbols))
	for _, symbol := range page.Symbols {
		views = append(views, codeSymbolToProto(symbol))
	}
	return &codefluxv1.ListCodeSymbolsResponse{
		Revision:     codeCollectionRevisionToProto(page.Revision),
		Symbols:      views,
		Page:         &codefluxv1.PageInfo{HasMore: page.Truncated},
		TotalMatched: page.TotalMatched,
	}, nil
}

// InspectCodeSymbol returns one declaration read closely.
func (service *CodeCollectionService) InspectCodeSymbol(
	ctx context.Context,
	request *codefluxv1.InspectCodeSymbolRequest,
) (*codefluxv1.InspectCodeSymbolResponse, error) {
	repositoryID, err := RepositoryIDFromProto(request.GetRepositoryId())
	if err != nil {
		return nil, &RequestValidationError{
			Field: "repository_id", Reason: "must be a repository identity",
		}
	}
	if request.GetKey() == "" {
		return nil, &RequestValidationError{Field: "key", Reason: "must not be empty"}
	}
	detail, err := service.application.InspectCodeSymbol(ctx, CodeSymbolInspection{
		RepositoryID: repositoryID, Key: request.GetKey(),
	})
	if err != nil {
		return nil, mapCodeCollectionError(err, "the declaration could not be read")
	}
	fields := make([]*codefluxv1.CodeAtomFieldView, 0, len(detail.AtomFields))
	for _, field := range detail.AtomFields {
		fields = append(fields, &codefluxv1.CodeAtomFieldView{
			Label: field.Label,
			Text:  codeCollectionText(field.Text),
			Items: codeCollectionTexts(field.Items),
		})
	}
	return &codefluxv1.InspectCodeSymbolResponse{
		Revision:            codeCollectionRevisionToProto(detail.Revision),
		Symbol:              codeSymbolToProto(detail.Symbol),
		Signature:           codeCollectionText(detail.Signature),
		Documentation:       codeCollectionTexts(detail.Documentation),
		AtomOpeningSentence: detail.AtomOpeningSentence,
		AtomSchemaVersion:   detail.AtomSchemaVersion,
		AtomFields:          fields,
		Callers:             codeReferencesToProto(detail.Callers),
		Callees:             codeReferencesToProto(detail.Callees),
		Tests:               codeReferencesToProto(detail.Tests),
		Implements:          detail.Implements,
		ImplementedBy:       detail.ImplementedBy,
		Source:              codeCollectionText(detail.Source),
		SourceStartLine:     detail.SourceStartLine,
		SourceTruncated:     detail.SourceTruncated,
	}, nil
}

// codeSymbolToProto converts one declaration.
func codeSymbolToProto(symbol CodeSymbolRecord) *codefluxv1.CodeSymbolView {
	return &codefluxv1.CodeSymbolView{
		Key:                   symbol.Key,
		Name:                  symbol.Name,
		Kind:                  symbol.Kind,
		Receiver:              symbol.Receiver,
		ImportPath:            symbol.ImportPath,
		WorkspaceRelativePath: symbol.Path,
		Line:                  symbol.Line,
		Exported:              symbol.Exported,
		Atom:                  symbol.Atom,
		AtomProblem:           symbol.AtomProblem,
	}
}

// codeReferencesToProto converts a bounded call list.
func codeReferencesToProto(
	references []CodeSymbolReference,
) []*codefluxv1.CodeSymbolReferenceView {
	views := make([]*codefluxv1.CodeSymbolReferenceView, 0, len(references))
	for _, reference := range references {
		views = append(views, &codefluxv1.CodeSymbolReferenceView{
			Name:                  reference.Name,
			WorkspaceRelativePath: reference.Path,
			Line:                  reference.Line,
		})
	}
	return views
}

// codeCollectionRevisionToProto states which revision an answer describes.
func codeCollectionRevisionToProto(
	revision CodeCollectionRevision,
) *codefluxv1.CodeCollectionRevisionView {
	return &codefluxv1.CodeCollectionRevisionView{
		Revision: revision.Revision,
		Dirty:    revision.Dirty,
		Warnings: codeCollectionTexts(revision.Warnings),
	}
}

// codeCollectionText wraps one piece of display text.
func codeCollectionText(value string) *codefluxv1.RedactedText {
	if value == "" {
		return nil
	}
	return &codefluxv1.RedactedText{
		Value: value, OriginalBytes: uint64(len(value)),
	}
}

// codeCollectionTexts wraps a list of display text, preserving empty lines.
func codeCollectionTexts(values []string) []*codefluxv1.RedactedText {
	texts := make([]*codefluxv1.RedactedText, 0, len(values))
	for _, value := range values {
		texts = append(texts, &codefluxv1.RedactedText{
			Value: value, OriginalBytes: uint64(len(value)),
		})
	}
	return texts
}

// mapCodeCollectionError maps the application's failures onto safe transport
// errors.
func mapCodeCollectionError(err error, safe string) error {
	var validation *RequestValidationError
	switch {
	case errors.Is(err, ErrCodeSymbolNotFound):
		return status.Error(codes.NotFound, "that declaration is not in this collection")
	case errors.Is(err, ErrWorkspaceTargetNotFound):
		return status.Error(codes.NotFound, "that repository is not open")
	case errors.As(err, &validation):
		return status.Error(codes.InvalidArgument, validation.Error())
	default:
		return status.Error(codes.Internal, safe)
	}
}

var _ codefluxv1.CodeCollectionServiceServer = (*CodeCollectionService)(nil)
