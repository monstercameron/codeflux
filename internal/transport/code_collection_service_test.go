package transport

import (
	"context"
	"errors"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// codeCollectionFake answers the service without a repository on disk.
type codeCollectionFake struct {
	packages     CodePackagePage
	packagesErr  error
	symbols      CodeSymbolPage
	symbolsErr   error
	detail       CodeSymbolDetail
	detailErr    error
	lastPackages CodeCollectionQuery
	lastSymbols  CodeSymbolQuery
	lastDetail   CodeSymbolInspection
	files        CodeFilePage
	filesErr     error
	lastFiles    CodeCollectionQuery
	content      CodeFileContent
	contentErr   error
	lastRead     CodeFileRead

	registered     RegisteredAtomPage
	registeredErr  error
	lastRegistered CodeCollectionQuery
}

func (fake *codeCollectionFake) ListCodeFiles(
	_ context.Context,
	query CodeCollectionQuery,
) (CodeFilePage, error) {
	fake.lastFiles = query
	return fake.files, fake.filesErr
}

func (fake *codeCollectionFake) ReadCodeFile(
	_ context.Context,
	read CodeFileRead,
) (CodeFileContent, error) {
	fake.lastRead = read
	return fake.content, fake.contentErr
}

func (fake *codeCollectionFake) ListRegisteredAtoms(
	_ context.Context, query CodeCollectionQuery,
) (RegisteredAtomPage, error) {
	fake.lastRegistered = query
	return fake.registered, fake.registeredErr
}

func (fake *codeCollectionFake) ListCodePackages(
	_ context.Context, query CodeCollectionQuery,
) (CodePackagePage, error) {
	fake.lastPackages = query
	return fake.packages, fake.packagesErr
}

func (fake *codeCollectionFake) ListCodeSymbols(
	_ context.Context, query CodeSymbolQuery,
) (CodeSymbolPage, error) {
	fake.lastSymbols = query
	return fake.symbols, fake.symbolsErr
}

func (fake *codeCollectionFake) InspectCodeSymbol(
	_ context.Context, inspection CodeSymbolInspection,
) (CodeSymbolDetail, error) {
	fake.lastDetail = inspection
	return fake.detail, fake.detailErr
}

func newCodeCollectionForTest(
	t *testing.T, fake *codeCollectionFake,
) (*CodeCollectionService, *codefluxv1.StableIdentity) {
	t.Helper()
	service, err := NewCodeCollectionService(fake)
	if err != nil {
		t.Fatal(err)
	}
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := RepositoryIDToProto(repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	return service, identity
}

func TestTheCodeCollectionServiceRequiresItsApplication(t *testing.T) {
	if _, err := NewCodeCollectionService(nil); err == nil {
		t.Fatal("a collection service with nothing to read must not construct")
	}
}

func TestThePackageListingCarriesTheRevisionItDescribes(t *testing.T) {
	fake := &codeCollectionFake{packages: CodePackagePage{
		Revision: CodeCollectionRevision{
			Revision: "2994deb", Dirty: true, Warnings: []string{"go-list: one package would not load"},
		},
		Packages: []CodePackageRecord{{
			ImportPath: "codeflux.dev/codeflux/internal/policy", Name: "policy",
			FileCount: 4, TestCount: 1, SymbolCount: 44, AtomCount: 2,
		}},
		Truncated: true, TotalPackages: 94, TotalSymbols: 20261, TotalAtoms: 2,
	}}
	service, identity := newCodeCollectionForTest(t, fake)
	response, err := service.ListCodePackages(t.Context(), &codefluxv1.ListCodePackagesRequest{
		RepositoryId: identity, Search: "policy",
		Page: &codefluxv1.PageRequest{Limit: 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastPackages.Search != "policy" || fake.lastPackages.Limit != 25 {
		t.Fatalf("the query lost a field: %+v", fake.lastPackages)
	}
	// A listing that did not name its revision would eventually describe a tree
	// that had moved on.
	if response.GetRevision().GetRevision() != "2994deb" || !response.GetRevision().GetDirty() {
		t.Fatalf("revision view = %+v", response.GetRevision())
	}
	if len(response.GetRevision().GetWarnings()) != 1 {
		t.Fatal("a package that would not load must be reported, not silently missing")
	}
	if len(response.GetPackages()) != 1 {
		t.Fatalf("want one package, got %d", len(response.GetPackages()))
	}
	view := response.GetPackages()[0]
	if view.GetImportPath() != "codeflux.dev/codeflux/internal/policy" ||
		view.GetSymbolCount() != 44 || view.GetAtomCount() != 2 || view.GetTestFileCount() != 1 {
		t.Fatalf("package view lost a field: %+v", view)
	}
	// A bounded page says it is bounded rather than reading as the whole
	// collection.
	if !response.GetPage().GetHasMore() {
		t.Fatal("a truncated page must say so")
	}
	if response.GetTotalPackages() != 94 || response.GetTotalSymbols() != 20261 {
		t.Fatalf("totals lost a field: %+v", response)
	}
}

func TestTheDeclarationListingCarriesEveryFilterThrough(t *testing.T) {
	fake := &codeCollectionFake{symbols: CodeSymbolPage{
		Symbols: []CodeSymbolRecord{{
			Key: "internal/policy/fixed.go#.Select", Name: "Select", Kind: "function",
			ImportPath: "codeflux.dev/codeflux/internal/policy",
			Path:       "internal/policy/fixed.go", Line: 151, Exported: true, Atom: true,
		}, {
			Key: "internal/policy/fixed.go#.brokenAtom", Name: "brokenAtom", Kind: "function",
			Path: "internal/policy/fixed.go", Line: 200,
			AtomProblem: "the atom documentation comment does not parse",
		}},
		TotalMatched: 2,
	}}
	service, identity := newCodeCollectionForTest(t, fake)
	response, err := service.ListCodeSymbols(t.Context(), &codefluxv1.ListCodeSymbolsRequest{
		RepositoryId: identity,
		ImportPath:   "codeflux.dev/codeflux/internal/policy",
		Search:       "sel", ExportedOnly: true, AtomsOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastSymbols.ImportPath != "codeflux.dev/codeflux/internal/policy" ||
		fake.lastSymbols.Search != "sel" || !fake.lastSymbols.ExportedOnly ||
		!fake.lastSymbols.AtomsOnly {
		t.Fatalf("the query lost a filter: %+v", fake.lastSymbols)
	}
	views := response.GetSymbols()
	if len(views) != 2 {
		t.Fatalf("want two declarations, got %d", len(views))
	}
	if !views[0].GetAtom() || views[0].GetKey() == "" || views[0].GetLine() != 151 {
		t.Fatalf("declaration view lost a field: %+v", views[0])
	}
	// A declaration carrying the directive whose documentation did not parse is
	// never reported as an admitted atom.
	if views[1].GetAtom() ||
		views[1].GetAtomProblem() != "the atom documentation comment does not parse" {
		t.Fatalf("unadmitted declaration view = %+v", views[1])
	}
}

func TestInspectingADeclarationCarriesItsDocumentationUnchanged(t *testing.T) {
	fake := &codeCollectionFake{detail: CodeSymbolDetail{
		Symbol: CodeSymbolRecord{
			Key: "internal/policy/fixed.go#.Select", Name: "Select", Atom: true,
		},
		Signature:     "func Select(input SelectionInput) (Snapshot, error)",
		Documentation: []string{"Select returns the frozen baseline.", "", "It performs no I/O."},
		AtomFields: []CodeAtomField{
			{Label: "Purpose", Text: "Choose the model policy a run uses."},
			{Label: "Inputs", Items: []string{"input: the selection inputs"}},
		},
		AtomSchemaVersion: 1,
		Callers: []CodeSymbolReference{
			{Name: "TestSelect", Path: "internal/policy/fixed_test.go", Line: 15},
		},
		Implements: []string{"policy.Selector"},
	}}
	service, identity := newCodeCollectionForTest(t, fake)
	response, err := service.InspectCodeSymbol(t.Context(), &codefluxv1.InspectCodeSymbolRequest{
		RepositoryId: identity, Key: "internal/policy/fixed.go#.Select",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastDetail.Key != "internal/policy/fixed.go#.Select" {
		t.Fatalf("the inspected key was not carried: %+v", fake.lastDetail)
	}
	if response.GetSignature().GetValue() != "func Select(input SelectionInput) (Snapshot, error)" {
		t.Fatalf("signature = %q", response.GetSignature().GetValue())
	}
	// A blank line in a comment is part of what the author wrote, so the
	// documentation crosses the boundary line for line.
	if len(response.GetDocumentation()) != 3 || response.GetDocumentation()[1].GetValue() != "" {
		t.Fatalf("documentation was rewritten: %+v", response.GetDocumentation())
	}
	if len(response.GetAtomFields()) != 2 ||
		response.GetAtomFields()[0].GetText().GetValue() != "Choose the model policy a run uses." ||
		len(response.GetAtomFields()[1].GetItems()) != 1 {
		t.Fatalf("atom fields lost a field: %+v", response.GetAtomFields())
	}
	if len(response.GetCallers()) != 1 || response.GetCallers()[0].GetLine() != 15 {
		t.Fatalf("callers lost a field: %+v", response.GetCallers())
	}
	if len(response.GetImplements()) != 1 {
		t.Fatalf("implements = %+v", response.GetImplements())
	}
}

func TestTheCollectionRefusesWhatItCannotAnswer(t *testing.T) {
	fake := &codeCollectionFake{}
	service, identity := newCodeCollectionForTest(t, fake)

	// An identity of the wrong kind names nothing this service can read.
	if _, err := service.ListCodePackages(t.Context(), &codefluxv1.ListCodePackagesRequest{
		RepositoryId: &codefluxv1.StableIdentity{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: "tsk_1",
		},
	}); err == nil {
		t.Fatal("a non-repository identity must be refused")
	}
	if _, err := service.InspectCodeSymbol(t.Context(), &codefluxv1.InspectCodeSymbolRequest{
		RepositoryId: identity,
	}); err == nil {
		t.Fatal("an inspection with no key must be refused")
	}

	fake.detailErr = ErrCodeSymbolNotFound
	if _, err := service.InspectCodeSymbol(t.Context(), &codefluxv1.InspectCodeSymbolRequest{
		RepositoryId: identity, Key: "gone",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing declaration = %v, want NotFound", err)
	}

	fake.packagesErr = ErrWorkspaceTargetNotFound
	if _, err := service.ListCodePackages(t.Context(), &codefluxv1.ListCodePackagesRequest{
		RepositoryId: identity,
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown repository = %v, want NotFound", err)
	}

	fake.packagesErr = errors.New("go list exploded in a way a browser must not see")
	_, err := service.ListCodePackages(t.Context(), &codefluxv1.ListCodePackagesRequest{
		RepositoryId: identity,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("internal failure = %v, want Internal", err)
	}
	if status.Convert(err).Message() == "go list exploded in a way a browser must not see" {
		t.Fatal("an internal failure must not return the underlying message")
	}
}
