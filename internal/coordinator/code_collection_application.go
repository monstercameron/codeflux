package coordinator

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"codeflux.dev/codeflux/internal/workspace"
)

// maximumCollectedRepositories bounds how many repository maps are held at
// once.
//
// A map is large and a coordinator runs at most four active tasks, so holding
// one per repository somebody browsed would grow with use rather than with
// work.
const maximumCollectedRepositories = 4

// codeCollectionApplication answers what code a repository contains.
//
// The repository map was built, tested, and never called by anything in the
// product: nothing read a package, a declaration, or a call. This is its first
// production consumer, and it is deliberately read-only. It reports what the
// map recorded and what the source says, and it never writes to either.
//
// Two authorities meet here. Which packages and declarations exist is the
// mapper's answer, derived from the Go toolchain at an exact revision. What a
// declaration says about itself — its signature, its comment, and its atom
// documentation when it carries the directive — is the source file's, read at
// inspection time. Neither is guessed from the other.
type codeCollectionApplication struct {
	repositories *storage.Repositories
	worktrees    *gitwork.Service
	runner       workspace.CommandRunner

	mutex     sync.Mutex
	collected map[string]*collectedRepository
	order     []string
}

// collectedRepository is one repository's map and the index built over it.
type collectedRepository struct {
	revision    string
	dirty       bool
	root        string
	warnings    []string
	packages    []transport.CodePackageRecord
	symbols     []transport.CodeSymbolRecord
	byKey       map[string]transport.CodeSymbolRecord
	callers     map[string][]transport.CodeSymbolReference
	callees     map[string][]transport.CodeSymbolReference
	implements  map[string][]string
	implemented map[string][]string
	totalAtoms  uint32
}

// newCodeCollectionApplication composes the mapper with the repository store.
func newCodeCollectionApplication(
	repositories *storage.Repositories,
	worktrees *gitwork.Service,
	runner workspace.CommandRunner,
) (*codeCollectionApplication, error) {
	if repositories == nil {
		return nil, errors.New("repositories are required")
	}
	if runner == nil {
		runner = workspace.ExecRunner{}
	}
	return &codeCollectionApplication{
		repositories: repositories,
		worktrees:    worktrees,
		runner:       runner,
		collected:    map[string]*collectedRepository{},
	}, nil
}

// ListCodePackages returns the packages the repository contains.
func (application *codeCollectionApplication) ListCodePackages(
	ctx context.Context,
	query transport.CodeCollectionQuery,
) (transport.CodePackagePage, error) {
	collected, err := application.collect(ctx, query.RepositoryID)
	if err != nil {
		return transport.CodePackagePage{}, err
	}
	limit := boundedCodeLimit(query.Limit)
	search := strings.ToLower(strings.TrimSpace(query.Search))
	page := transport.CodePackagePage{
		Revision:      collected.revisionView(),
		TotalPackages: uint32(len(collected.packages)),
		TotalSymbols:  uint32(len(collected.symbols)),
		TotalAtoms:    collected.totalAtoms,
	}
	for _, record := range collected.packages {
		if search != "" && !strings.Contains(strings.ToLower(record.ImportPath), search) {
			continue
		}
		if len(page.Packages) == limit {
			page.Truncated = true
			break
		}
		page.Packages = append(page.Packages, record)
	}
	return page, nil
}

// ListCodeSymbols returns the declarations matching a query.
func (application *codeCollectionApplication) ListCodeSymbols(
	ctx context.Context,
	query transport.CodeSymbolQuery,
) (transport.CodeSymbolPage, error) {
	collected, err := application.collect(ctx, query.RepositoryID)
	if err != nil {
		return transport.CodeSymbolPage{}, err
	}
	limit := boundedCodeLimit(query.Limit)
	search := strings.ToLower(strings.TrimSpace(query.Search))
	page := transport.CodeSymbolPage{Revision: collected.revisionView()}
	for _, symbol := range collected.symbols {
		if query.ImportPath != "" && symbol.ImportPath != query.ImportPath {
			continue
		}
		if query.ExportedOnly && !symbol.Exported {
			continue
		}
		if query.AtomsOnly && !symbol.Atom {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(symbol.Name), search) {
			continue
		}
		page.TotalMatched++
		if len(page.Symbols) == limit {
			page.Truncated = true
			continue
		}
		page.Symbols = append(page.Symbols, symbol)
	}
	return page, nil
}

// InspectCodeSymbol reads one declaration closely.
func (application *codeCollectionApplication) InspectCodeSymbol(
	ctx context.Context,
	inspection transport.CodeSymbolInspection,
) (transport.CodeSymbolDetail, error) {
	collected, err := application.collect(ctx, inspection.RepositoryID)
	if err != nil {
		return transport.CodeSymbolDetail{}, err
	}
	symbol, present := collected.byKey[inspection.Key]
	if !present {
		return transport.CodeSymbolDetail{}, transport.ErrCodeSymbolNotFound
	}
	// What exercises a declaration and what depends on it are different
	// questions: a test naming this atom is evidence about it, and a caller is
	// a consequence of changing it.
	callers, tests := partitionTestReferences(collected.callers[symbol.Name])
	detail := transport.CodeSymbolDetail{
		Revision: collected.revisionView(),
		Symbol:   symbol,
		Callers:  callers,
		Callees:  collected.callees[symbol.Name],
		Tests:    tests,
	}
	detail.Implements = collected.implements[symbol.Name]
	detail.ImplementedBy = collected.implemented[symbol.Name]
	// The signature and the comment are read from the file rather than stored,
	// because they are what the declaration says about itself and the map does
	// not record them.
	read, readErr := readDeclaration(collected.root, symbol)
	if readErr != nil {
		// A declaration whose source could not be read is still a real entry in
		// the collection. Reporting the failure as a missing symbol would say
		// the code does not exist.
		detail.Documentation = []string{
			"This declaration's source could not be read at this revision.",
		}
		return detail, nil
	}
	detail.Signature = read.signature
	detail.Documentation = read.documentation
	detail.AtomOpeningSentence = read.openingSentence
	detail.AtomSchemaVersion = read.schemaVersion
	detail.AtomFields = read.fields
	detail.Source = read.source
	detail.SourceStartLine = read.sourceStartLine
	detail.SourceTruncated = read.sourceTruncated
	return detail, nil
}

// collect returns the repository's map, building it when the revision moved.
func (application *codeCollectionApplication) collect(
	ctx context.Context,
	repositoryID domain.RepositoryID,
) (*collectedRepository, error) {
	if repositoryID.IsZero() {
		return nil, transport.ErrWorkspaceTargetNotFound
	}
	repository, err := application.repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, translateWorkspaceError(err)
	}
	state, err := application.readState(ctx, repository.CanonicalPath)
	if err != nil {
		return nil, err
	}
	key := repositoryID.String()
	application.mutex.Lock()
	existing, present := application.collected[key]
	application.mutex.Unlock()
	if present && existing.revision == state.HeadRevision && existing.dirty == state.Dirty {
		return existing, nil
	}
	built, err := application.build(ctx, repository.CanonicalPath, state)
	if err != nil {
		return nil, err
	}
	application.mutex.Lock()
	defer application.mutex.Unlock()
	if _, replacing := application.collected[key]; !replacing {
		application.order = append(application.order, key)
		for len(application.order) > maximumCollectedRepositories {
			delete(application.collected, application.order[0])
			application.order = application.order[1:]
		}
	}
	application.collected[key] = built
	return built, nil
}

// readState reports what Git says about the repository right now.
func (application *codeCollectionApplication) readState(
	ctx context.Context,
	root string,
) (gitwork.RepositoryState, error) {
	if application.worktrees == nil {
		return gitwork.RepositoryState{}, errors.New("no Git service is available")
	}
	return application.worktrees.ReadRepositoryState(ctx, root)
}

// build maps the repository and indexes what the map recorded.
func (application *codeCollectionApplication) build(
	ctx context.Context,
	root string,
	state gitwork.RepositoryState,
) (*collectedRepository, error) {
	revision := state.HeadRevision
	if revision == "" {
		// The mapper binds its answer to a revision and refuses without one. A
		// repository with no recorded revision is reported rather than mapped
		// against nothing.
		return nil, errors.New("the repository has no current revision")
	}
	mapped, err := workspace.BuildRepositoryMap(
		ctx,
		workspace.RepositorySnapshot{
			CanonicalRoot: root,
			HeadRevision:  revision,
			GitIdentity:   revision,
		},
		application.runner,
	)
	if err != nil {
		return nil, err
	}
	collected := &collectedRepository{
		revision:    revision,
		dirty:       state.Dirty,
		root:        root,
		byKey:       map[string]transport.CodeSymbolRecord{},
		callers:     map[string][]transport.CodeSymbolReference{},
		callees:     map[string][]transport.CodeSymbolReference{},
		implements:  map[string][]string{},
		implemented: map[string][]string{},
	}
	for _, warning := range mapped.Warnings {
		collected.warnings = append(collected.warnings, warning.Stage+": "+warning.Message)
	}
	indexCollectedSymbols(collected, mapped, atomsByPath(root, mapped))
	return collected, nil
}

// revisionView states which revision an answer describes.
func (collected *collectedRepository) revisionView() transport.CodeCollectionRevision {
	return transport.CodeCollectionRevision{
		Revision: collected.revision,
		Dirty:    collected.dirty,
		Warnings: collected.warnings,
	}
}

// boundedCodeLimit keeps a caller's page inside the service's bound.
func boundedCodeLimit(limit int) int {
	if limit <= 0 || limit > transport.MaximumCodePage {
		return transport.MaximumCodePage
	}
	return limit
}

// codeSymbolKey identifies one declaration stably across two reads of the same
// revision.
//
// It is built from the file, the receiver, and the name rather than the line,
// because a line moves when anything above it changes and a key that moves is
// a key that breaks a link somebody kept open.
func codeSymbolKey(path, receiver, name string) string {
	return path + "#" + receiver + "." + name
}

// declarationRead is what one source file says about a declaration.
type declarationRead struct {
	signature       string
	documentation   []string
	openingSentence string
	schemaVersion   uint32
	fields          []transport.CodeAtomField
	source          string
	sourceStartLine uint32
	sourceTruncated bool
}

// maximumDeclarationLines bounds the source one declaration returns.
//
// A directory of code has to stay a directory: a thousand-line function is a
// problem to fix rather than something to page through here, so the read stops
// and says it stopped.
const maximumDeclarationLines = 400

// readDeclaration reads one declaration's signature, comment, and atom
// documentation from the file that declares it.
func readDeclaration(
	root string,
	symbol transport.CodeSymbolRecord,
) (declarationRead, error) {
	fileSet := token.NewFileSet()
	absolute := filepath.Join(root, filepath.FromSlash(symbol.Path))
	parsed, err := parser.ParseFile(fileSet, absolute, nil, parser.ParseComments)
	if err != nil {
		return declarationRead{}, err
	}
	for _, declaration := range parsed.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Name.Name != symbol.Name ||
				receiverName(typed) != symbol.Receiver {
				continue
			}
			read := describeDeclaration(fileSet, typed.Doc, functionSignature(fileSet, typed))
			return withDeclarationSource(read, absolute, fileSet, typed.Doc, typed), nil
		case *ast.GenDecl:
			for _, specification := range typed.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != symbol.Name {
					continue
				}
				doc := typeSpec.Doc
				if doc == nil {
					doc = typed.Doc
				}
				read := describeDeclaration(fileSet, doc, typeSignature(fileSet, typeSpec))
				return withDeclarationSource(read, absolute, fileSet, doc, typed), nil
			}
		}
	}
	return declarationRead{}, transport.ErrCodeSymbolNotFound
}

// describeDeclaration renders a declaration's comment and its atom fields.
func describeDeclaration(
	fileSet *token.FileSet,
	doc *ast.CommentGroup,
	signature string,
) declarationRead {
	read := declarationRead{signature: signature}
	if doc == nil {
		return read
	}
	for _, line := range strings.Split(strings.TrimRight(doc.Text(), "\n"), "\n") {
		read.documentation = append(read.documentation, line)
	}
	if !atomDirectivePresent(doc) {
		return read
	}
	parsedComment, err := atomdoc.ParseAtomDocumentationComment(atomdoc.SourceCandidate{
		Identifier: "", DocGroup: doc, Position: fileSet.Position(doc.Pos()),
	})
	if err != nil {
		// A declaration that carries the directive but whose documentation does
		// not parse is not presented as documented. The comment is still shown,
		// unchanged, so a reader can see what is actually written there.
		return read
	}
	read.openingSentence = parsedComment.OpeningSentence
	read.schemaVersion = parsedComment.SchemaVersion
	for _, field := range parsedComment.Fields {
		body := strings.TrimRight(strings.Join(field.BodyLines, "\n"), "\n")
		entry := transport.CodeAtomField{Label: field.Label}
		for _, line := range field.BodyLines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") {
				entry.Items = append(entry.Items, strings.TrimSpace(trimmed[2:]))
			}
		}
		if len(entry.Items) == 0 {
			entry.Text = strings.TrimSpace(body)
		}
		read.fields = append(read.fields, entry)
	}
	return read
}

// functionSignature renders a function declaration without its body.
func functionSignature(fileSet *token.FileSet, declaration *ast.FuncDecl) string {
	shallow := *declaration
	shallow.Body = nil
	shallow.Doc = nil
	return printNode(fileSet, &shallow)
}

// typeSignature renders a type declaration's name and shape.
func typeSignature(fileSet *token.FileSet, specification *ast.TypeSpec) string {
	shallow := *specification
	shallow.Doc = nil
	shallow.Comment = nil
	return "type " + printNode(fileSet, &shallow)
}

// printNode renders one syntax node as source.
func printNode(fileSet *token.FileSet, node ast.Node) string {
	builder := &strings.Builder{}
	if err := (&printer.Config{Mode: printer.UseSpaces, Tabwidth: 4}).Fprint(
		builder, fileSet, node,
	); err != nil {
		return ""
	}
	return strings.TrimSpace(builder.String())
}

// receiverName reports the receiver type a method is declared on.
func receiverName(declaration *ast.FuncDecl) string {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		return ""
	}
	switch typed := declaration.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if identifier, ok := typed.X.(*ast.Ident); ok {
			return identifier.Name
		}
	case *ast.Ident:
		return typed.Name
	}
	return ""
}

// atomDirectivePresent reports whether a comment group admits an atom.
func atomDirectivePresent(group *ast.CommentGroup) bool {
	if group == nil {
		return false
	}
	for _, comment := range group.List {
		if strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")) == "codeflux:atom" {
			return true
		}
	}
	return false
}

// atomsByPath reports which declarations in the repository are admitted atoms.
//
// Only files that contain the directive are parsed. Parsing every Go file in a
// repository to answer a directory listing would make opening the page cost as
// much as a build.
func atomsByPath(root string, mapped workspace.RepositoryMap) map[string]map[string]string {
	admitted := map[string]map[string]string{}
	for _, file := range mapped.Files {
		// Generated source is skipped: an atom directive in a file nobody wrote
		// by hand is not a declaration somebody documented.
		if file.Generated {
			continue
		}
		if file.Kind != "source" && file.Kind != "test" {
			continue
		}
		content, err := readBoundedSource(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil || !strings.Contains(content, "//codeflux:atom") {
			continue
		}
		fileSet := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fileSet, file.Path, content, parser.ParseComments)
		if parseErr != nil {
			continue
		}
		candidates, locateErr := atomdoc.LocateAtomDeclarationCandidates(fileSet, parsed)
		if locateErr != nil {
			// A directive that binds to no declaration is a problem in the
			// source, not in this listing. The file's real atoms are still
			// reported.
			continue
		}
		for _, candidate := range candidates {
			problem := ""
			if _, err := atomdoc.ParseAtomDocumentationComment(candidate); err != nil {
				problem = "the atom documentation comment does not parse"
			}
			if admitted[file.Path] == nil {
				admitted[file.Path] = map[string]string{}
			}
			admitted[file.Path][candidate.Identifier] = problem
		}
	}
	return admitted
}

// indexCollectedSymbols builds the listing and the call index over one map.
func indexCollectedSymbols(
	collected *collectedRepository,
	mapped workspace.RepositoryMap,
	admitted map[string]map[string]string,
) {
	packageByPath := map[string]string{}
	fileCounts := map[string]uint32{}
	testCounts := map[string]uint32{}
	for _, pkg := range mapped.Packages {
		for _, file := range pkg.SourceFiles {
			packageByPath[file] = pkg.ImportPath
		}
		for _, file := range pkg.TestFiles {
			packageByPath[file] = pkg.ImportPath
		}
		fileCounts[pkg.ImportPath] = uint32(len(pkg.SourceFiles))
		testCounts[pkg.ImportPath] = uint32(len(pkg.TestFiles))
	}
	symbolCounts := map[string]uint32{}
	atomCounts := map[string]uint32{}
	for _, symbol := range mapped.Symbols {
		importPath := packageByPath[symbol.Path]
		problem, isAtom := admitted[symbol.Path][symbol.Name]
		record := transport.CodeSymbolRecord{
			Key:         codeSymbolKey(symbol.Path, symbol.Receiver, symbol.Name),
			Name:        symbol.Name,
			Kind:        symbol.Kind,
			Receiver:    symbol.Receiver,
			ImportPath:  importPath,
			Path:        symbol.Path,
			Line:        uint32(symbol.Line),
			Exported:    symbol.Exported,
			Atom:        isAtom && problem == "",
			AtomProblem: problem,
		}
		collected.symbols = append(collected.symbols, record)
		collected.byKey[record.Key] = record
		symbolCounts[importPath]++
		if record.Atom {
			atomCounts[importPath]++
			collected.totalAtoms++
		}
	}
	for _, pkg := range mapped.Packages {
		collected.packages = append(collected.packages, transport.CodePackageRecord{
			ImportPath:  pkg.ImportPath,
			Name:        pkg.Name,
			Directory:   pkg.Directory,
			ModulePath:  pkg.ModulePath,
			FileCount:   fileCounts[pkg.ImportPath],
			TestCount:   testCounts[pkg.ImportPath],
			SymbolCount: symbolCounts[pkg.ImportPath],
			AtomCount:   atomCounts[pkg.ImportPath],
		})
	}
	sort.SliceStable(collected.packages, func(first, second int) bool {
		return collected.packages[first].ImportPath < collected.packages[second].ImportPath
	})
	sort.SliceStable(collected.symbols, func(first, second int) bool {
		left, right := collected.symbols[first], collected.symbols[second]
		if left.ImportPath != right.ImportPath {
			return left.ImportPath < right.ImportPath
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Line < right.Line
	})
	for _, call := range mapped.Calls {
		collected.callers[call.Callee] = appendBoundedReference(
			collected.callers[call.Callee],
			transport.CodeSymbolReference{
				Name: call.Caller, Path: call.Path, Line: uint32(call.Line),
			},
		)
		collected.callees[call.Caller] = appendBoundedReference(
			collected.callees[call.Caller],
			transport.CodeSymbolReference{
				Name: call.Callee, Path: call.Path, Line: uint32(call.Line),
			},
		)
	}
	for _, implementation := range mapped.Implementations {
		collected.implements[implementation.Concrete] = append(
			collected.implements[implementation.Concrete], implementation.Interface,
		)
		collected.implemented[implementation.Interface] = append(
			collected.implemented[implementation.Interface], implementation.Concrete,
		)
	}
}

// maximumRecordedReferences bounds one declaration's call list.
//
// A declaration called from four hundred places is real, and a detail panel
// listing all four hundred is not readable.
const maximumRecordedReferences = 50

// appendBoundedReference keeps a call list finite.
func appendBoundedReference(
	existing []transport.CodeSymbolReference,
	reference transport.CodeSymbolReference,
) []transport.CodeSymbolReference {
	if len(existing) >= maximumRecordedReferences {
		return existing
	}
	return append(existing, reference)
}

// withDeclarationSource attaches the declaration exactly as written.
//
// The text is sliced out of the file rather than reprinted from the syntax
// tree: a reprint is the compiler's formatting of the code, not the code, and
// a reader comparing a promise against its implementation needs what is
// actually there. The comment above the declaration is included, because the
// promise and the code that keeps it are one thing to read.
func withDeclarationSource(
	read declarationRead,
	absolute string,
	fileSet *token.FileSet,
	doc *ast.CommentGroup,
	node ast.Node,
) declarationRead {
	content, err := readBoundedSource(absolute)
	if err != nil {
		return read
	}
	start := node.Pos()
	if doc != nil && doc.Pos().IsValid() && doc.Pos() < start {
		start = doc.Pos()
	}
	startPosition := fileSet.Position(start)
	endPosition := fileSet.Position(node.End())
	if !startPosition.IsValid() || !endPosition.IsValid() ||
		startPosition.Offset < 0 || endPosition.Offset > len(content) ||
		startPosition.Offset >= endPosition.Offset {
		return read
	}
	text := strings.ReplaceAll(content[startPosition.Offset:endPosition.Offset], "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > maximumDeclarationLines {
		lines = lines[:maximumDeclarationLines]
		read.sourceTruncated = true
	}
	read.source = strings.Join(lines, "\n")
	read.sourceStartLine = uint32(startPosition.Line)
	return read
}

// partitionTestReferences splits references declared in test files from the
// rest, by the one rule Go itself uses: the file name ends in _test.go.
func partitionTestReferences(
	references []transport.CodeSymbolReference,
) (callers, tests []transport.CodeSymbolReference) {
	for _, reference := range references {
		if strings.HasSuffix(reference.Path, "_test.go") {
			tests = append(tests, reference)
			continue
		}
		callers = append(callers, reference)
	}
	return callers, tests
}

// maximumSourceBytes bounds one source file read.
const maximumSourceBytes = 4 << 20

// readBoundedSource reads one source file without letting an unexpected file
// size become the coordinator's memory.
func readBoundedSource(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maximumSourceBytes {
		return "", fmt.Errorf(
			"source file exceeds %s bytes", strconv.Itoa(maximumSourceBytes),
		)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

var _ transport.CodeCollectionApplication = (*codeCollectionApplication)(nil)
