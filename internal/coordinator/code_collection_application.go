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
	"codeflux.dev/codeflux/internal/atomname"
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
	// searchable is every admitted atom with the text a search reads, and
	// search is the index built over it. The index is built once per map
	// rather than per query.
	searchable []atomSearchDocument
	search     *atomSearchIndex
	// files is every file the map recorded, in path order, and fileByPath is
	// the lookup a read is allowed to resolve against.
	files      []transport.CodeFileRecord
	fileByPath map[string]transport.CodeFileRecord
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
	page := transport.CodeSymbolPage{
		Revision: collected.revisionView(), TotalAtoms: collected.totalAtoms,
	}
	// An atom search reads the documented promises, not just the declaration
	// name: "which one says it is not safe to retry" is the question people
	// actually arrive with, and no name answers it. Ordinary symbol listing
	// keeps the name match it has always had.
	if query.AtomsOnly && search != "" {
		matched, ranked, searchErr := collected.searchAtoms(query, search, limit)
		if searchErr == nil && len(ranked) > 0 {
			page.Symbols = ranked
			page.TotalMatched = matched
			page.Truncated = uint32(len(ranked)) < matched
			page.SearchQuery = AtomSearchSQL(search, limit)
			return page, nil
		}
	}
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
	detail.NamingChecked, detail.NamingFinding = checkAtomNaming(symbol)
	detail.Structure = read.structure
	if symbol.Atom {
		detail.DocumentedFields, detail.MissingFields = splitSchemaFields(read.fields)
		// The query is offered only for an atom, because only an atom is in the
		// index. Showing a read for a row that is not there would be a
		// statement that returns nothing and looks like a fault.
		detail.AccessQuery = AtomSelectSQL(symbol.Key)
		detail.IndexSchema = AtomSchemaSQL()
	}
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
			evicted := application.order[0]
			// The evicted map owns an open in-memory database. Dropping the
			// reference without closing it would leak one connection per
			// repository somebody browsed.
			if stale, ok := application.collected[evicted]; ok {
				stale.search.Close()
			}
			delete(application.collected, evicted)
			application.order = application.order[1:]
		}
	}
	if superseded, ok := application.collected[key]; ok && superseded != built {
		superseded.search.Close()
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
		fileByPath:  map[string]transport.CodeFileRecord{},
	}
	for _, warning := range mapped.Warnings {
		collected.warnings = append(collected.warnings, warning.Stage+": "+warning.Message)
	}
	indexCollectedSymbols(collected, mapped, atomsByPath(root, mapped))
	// A failed index is not a failed map. Search then falls back to matching
	// names, which is what it did before the index existed; refusing the whole
	// collection because its search index would not build would take away the
	// listing to protect the search.
	if index, indexErr := newAtomSearchIndex(collected.searchable); indexErr == nil {
		collected.search = index
	}
	return collected, nil
}

// searchAtoms answers one atom search from the full-text index, in rank order.
//
// The package filter is applied after ranking rather than inside the query: it
// is a coarse narrowing a person applies with a control, and pushing it into
// the match expression would let a package name in the query silently act as a
// filter.
func (collected *collectedRepository) searchAtoms(
	query transport.CodeSymbolQuery,
	search string,
	limit int,
) (matched uint32, ranked []transport.CodeSymbolRecord, err error) {
	hits, err := collected.search.Search(search, transport.MaximumCodePage)
	if err != nil {
		return 0, nil, err
	}
	for _, hit := range hits {
		symbol, present := collected.byKey[hit.Key]
		if !present {
			continue
		}
		if query.ImportPath != "" && symbol.ImportPath != query.ImportPath {
			continue
		}
		if query.ExportedOnly && !symbol.Exported {
			continue
		}
		// The reason travels with the row: a result list that cannot say why
		// an atom is in it is one a person has to take on faith.
		symbol.MatchedName = hit.MatchedName
		symbol.MatchedPromise = hit.Promise
		matched++
		if len(ranked) < limit {
			ranked = append(ranked, symbol)
		}
	}
	return matched, ranked, nil
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
	structure       transport.CodeSymbolStructure
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
			read.structure = measureDeclaration(typed)
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
func atomsByPath(root string, mapped workspace.RepositoryMap) map[string]map[string]atomAdmission {
	admitted := map[string]map[string]atomAdmission{}
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
			entry := atomAdmission{}
			// The comment is parsed here anyway to decide admission, so the
			// documented text it yields is captured rather than discarded: it
			// is what the search index is built from, and parsing every atom
			// comment a second time to read it back would double the cost of
			// opening the page.
			parsedComment, err := atomdoc.ParseAtomDocumentationComment(candidate)
			if err != nil {
				entry.Problem = "the atom documentation comment does not parse"
			} else {
				entry.Documentation = atomDocumentationText(parsedComment)
			}
			if admitted[file.Path] == nil {
				admitted[file.Path] = map[string]atomAdmission{}
			}
			admitted[file.Path][candidate.Identifier] = entry
		}
	}
	return admitted
}

// atomAdmission is what parsing one atom's comment established: whether it was
// admitted, and the text a search can look through.
type atomAdmission struct {
	Problem       string
	Documentation string
}

// atomDocumentationText flattens an atom's documented promises into the text a
// search index reads. Labels are kept, because "retry" is a word people search
// for and it is a label before it is prose.
func atomDocumentationText(parsed atomdoc.ParsedComment) string {
	var builder strings.Builder
	builder.WriteString(parsed.OpeningSentence)
	for _, field := range parsed.Fields {
		builder.WriteString("\n")
		builder.WriteString(field.Label)
		builder.WriteString(": ")
		builder.WriteString(strings.Join(field.BodyLines, " "))
	}
	return strings.TrimSpace(builder.String())
}

// indexCollectedSymbols builds the listing and the call index over one map.
func indexCollectedSymbols(
	collected *collectedRepository,
	mapped workspace.RepositoryMap,
	admitted map[string]map[string]atomAdmission,
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
		admission, isAtom := admitted[symbol.Path][symbol.Name]
		problem := admission.Problem
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
			collected.searchable = append(collected.searchable, atomSearchDocument{
				Key: record.Key, Name: record.Name, Receiver: record.Receiver,
				ImportPath: record.ImportPath, Path: record.Path,
				Documentation: admission.Documentation,
			})
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
	// The file list is built from the map's own files rather than from the
	// packages, because a repository holds files no package claims -- a
	// generated file, a testdata fixture, a file that would not compile -- and
	// a tree drawn only from packages would quietly leave them out.
	perFileSymbols := map[string]uint32{}
	perFileAtoms := map[string]uint32{}
	for _, symbol := range collected.symbols {
		perFileSymbols[symbol.Path]++
		if symbol.Atom {
			perFileAtoms[symbol.Path]++
		}
	}
	for _, file := range mapped.Files {
		record := transport.CodeFileRecord{
			Path:        file.Path,
			Kind:        file.Kind,
			Generated:   file.Generated,
			ImportPath:  packageByPath[file.Path],
			SymbolCount: perFileSymbols[file.Path],
			AtomCount:   perFileAtoms[file.Path],
		}
		collected.files = append(collected.files, record)
		collected.fileByPath[record.Path] = record
	}
	sort.SliceStable(collected.files, func(first, second int) bool {
		return collected.files[first].Path < collected.files[second].Path
	})
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
// actually there.
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
	// The slice starts at the declaration, not at its comment. The comment is
	// returned separately as the notes the author wrote, and repeating forty
	// lines of it above the code would push the body off the screen on the one
	// surface that exists to show the body.
	_ = doc
	start := node.Pos()
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

// splitSchemaFields reports which of the schema's canonical fields an atom
// declares and which it leaves out.
//
// The schema declares nineteen fields, and an atom that documents twelve of
// them is a different thing from one that documents all nineteen. Only the
// list of the missing seven says which promises were never made — which is the
// question an author has when their atom looks documented and still fails a
// stage.
func splitSchemaFields(declared []transport.CodeAtomField) (documented, missing []string) {
	written := make(map[string]bool, len(declared))
	for _, field := range declared {
		written[strings.ToLower(strings.TrimSpace(field.Label))] = true
	}
	for _, label := range atomdoc.CanonicalFieldLabels() {
		if written[strings.ToLower(label)] {
			documented = append(documented, label)
			continue
		}
		missing = append(missing, label)
	}
	return documented, missing
}

// measureDeclaration reports the structural facts the pipeline itself measures
// about a function.
//
// It calls the same analysis the run's own atom stages call — the deepest loop
// nesting that StageAtomComplexity turns into a time bound, the decision points
// that StageAtomCaseSynthesis turns into a count of cases to cover, and the
// purity test StageAtoms applies. Measuring it twice with two different
// definitions would let the page and the pipeline disagree about the same
// declaration, so this reads the pipeline's answer rather than its own.
func measureDeclaration(declaration *ast.FuncDecl) transport.CodeSymbolStructure {
	if declaration == nil || declaration.Body == nil {
		return transport.CodeSymbolStructure{}
	}
	_, branches, loopDepth := describeBody(declaration, map[string]bool{})
	return transport.CodeSymbolStructure{
		Measured:  true,
		TimeBound: complexityLabel(loopDepth),
		// The space claim is the one the pipeline records: nothing here
		// allocates beyond what it was handed, and a measured growth is what a
		// later run would have to disagree with.
		SpaceClaim: "bounded by the input it is given",
		LoopDepth:  uint32(loopDepth),
		Branches:   uint32(branches),
		Pure:       !callsAnythingImpure(declaration),
	}
}

// checkAtomNaming runs the product's own atom-naming grammar against an
// admitted atom's name.
//
// The grammar is already defined and enforced nowhere a person can see: it
// asks that an executable atom begin with a recognized action verb, that an
// initialism be an established abbreviation, that a name not carry a provider
// name or a guarantee claim. A name is the part of a contract a caller cannot
// avoid reading, so what the product thinks of it belongs beside the contract.
// Only admitted atoms are checked: an ordinary declaration never claimed to
// follow this grammar.
func checkAtomNaming(symbol transport.CodeSymbolRecord) (checked bool, finding string) {
	if !symbol.Atom {
		return false, ""
	}
	name, err := atomname.NewCanonicalName(symbol.Name)
	if err != nil {
		return true, err.Error()
	}
	kind := atomname.NameSubjectKindExecutableAtom
	if symbol.Kind != "function" && symbol.Kind != "method" {
		kind = atomname.NameSubjectKindNonExecutableAtom
	}
	if err := atomname.ValidateNamingGrammar(
		name, kind, atomname.DefaultNamingLexicon(), nil,
	); err != nil {
		return true, err.Error()
	}
	return true, ""
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
