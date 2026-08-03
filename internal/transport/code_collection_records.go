package transport

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// CodeCollectionApplication is the narrow surface the code collection service
// needs.
//
// It is an interface so the service can be exercised without a repository on
// disk, and so a directory of code cannot reach past reading one.
type CodeCollectionApplication interface {
	// ListCodePackages returns the packages the repository map recorded.
	ListCodePackages(context.Context, CodeCollectionQuery) (CodePackagePage, error)
	// ListCodeSymbols returns the declarations in one package, or every
	// declaration matching a query when no package is named.
	ListCodeSymbols(context.Context, CodeSymbolQuery) (CodeSymbolPage, error)
	// InspectCodeSymbol returns one declaration with its signature, its
	// documentation, its atom fields when it is an admitted atom, and what
	// calls it.
	InspectCodeSymbol(context.Context, CodeSymbolInspection) (CodeSymbolDetail, error)
	// ListCodeFiles returns every file the map recorded, so a client can draw
	// the collection as the tree it is on disk.
	ListCodeFiles(context.Context, CodeCollectionQuery) (CodeFilePage, error)
	// ReadCodeFile returns one file's text and what it declares.
	ReadCodeFile(context.Context, CodeFileRead) (CodeFileContent, error)
	// ListRegisteredAtoms returns the atoms the repository's project has
	// registered, which is the persisted collection rather than the one parsed
	// out of the working tree.
	ListRegisteredAtoms(context.Context, CodeCollectionQuery) (RegisteredAtomPage, error)
}

// RegisteredAtomRecord is one persisted atom-documentation revision.
type RegisteredAtomRecord struct {
	AtomID string
	// Name is empty when the atom has documentation but no naming revision.
	// The empty string is carried through rather than substituted, because a
	// surface that prints an identity where a name belongs reads as though the
	// atom were named that.
	Name                     string
	Purpose                  string
	Authoring                string
	SchemaVersion            uint32
	SourceRepositoryRevision string
	RevisionID               string
}

// RegisteredAtomPage is one bounded page of registered atoms.
type RegisteredAtomPage struct {
	Atoms           []RegisteredAtomRecord
	Truncated       bool
	TotalRegistered uint32
}

// CodeFileRecord is one file in the collection.
type CodeFileRecord struct {
	Path string
	// Kind is what the mapper recorded -- "source", "test" -- and Generated
	// marks a file nobody wrote by hand.
	Kind        string
	Generated   bool
	ImportPath  string
	SymbolCount uint32
	AtomCount   uint32
}

// CodeFilePage is one bounded page of files.
type CodeFilePage struct {
	Revision   CodeCollectionRevision
	Files      []CodeFileRecord
	Truncated  bool
	TotalFiles uint32
}

// CodeFileRead names one file to read.
type CodeFileRead struct {
	RepositoryID domain.RepositoryID
	// Path is workspace-relative and must be one the map recorded. Resolving an
	// arbitrary path would turn a directory listing into a way to read anything
	// the coordinator can reach.
	Path string
}

// CodeFileContent is one file as it is on disk, bounded.
type CodeFileContent struct {
	Revision CodeCollectionRevision
	File     CodeFileRecord
	Text     string
	Lines    uint32
	// Truncated reports that the file continues past what was returned, so a
	// reader is told rather than left to assume they saw all of it.
	Truncated    bool
	Declarations []CodeSymbolRecord
}

// CodeCollectionQuery names the repository whose collection is being read.
type CodeCollectionQuery struct {
	RepositoryID domain.RepositoryID
	// Search filters packages by import path. An empty search lists them all,
	// bounded.
	Search string
	Limit  int
}

// CodeSymbolQuery names the declarations being listed.
//
// ImportPath and Search are alternatives a caller may combine: a package with
// a search inside it, a search across the repository, or one whole package.
type CodeSymbolQuery struct {
	RepositoryID domain.RepositoryID
	ImportPath   string
	Search       string
	// ExportedOnly hides declarations a package does not offer to callers.
	ExportedOnly bool
	// AtomsOnly narrows the listing to admitted atoms, which is the atom
	// directory this collection contains rather than a separate surface.
	AtomsOnly bool
	Limit     int
}

// CodeSymbolInspection names one declaration.
type CodeSymbolInspection struct {
	RepositoryID domain.RepositoryID
	// Key is the identity ListCodeSymbols returned. It is opaque to a caller
	// and is never a path a caller can act on directly.
	Key string
}

// CodeCollectionRevision states which repository revision an answer describes.
//
// Every answer carries it, because a directory of code that does not say which
// revision it read is a directory that will eventually describe a tree that has
// moved on.
type CodeCollectionRevision struct {
	Revision string
	// Dirty reports that the working tree has uncommitted changes, so the map
	// describes the revision rather than exactly what is on disk.
	Dirty bool
	// Warnings are the bounded, redacted problems the mapper recorded. A
	// package that would not load is reported rather than silently missing.
	Warnings []string
}

// CodePackageRecord is one Go package in the collection.
type CodePackageRecord struct {
	ImportPath  string
	Name        string
	Directory   string
	ModulePath  string
	FileCount   uint32
	TestCount   uint32
	SymbolCount uint32
	// AtomCount is how many of the package's declarations are admitted atoms.
	AtomCount uint32
}

// CodePackagePage is one bounded page of packages.
type CodePackagePage struct {
	Revision CodeCollectionRevision
	Packages []CodePackageRecord
	// Truncated reports that the bound was reached, so a caller says so rather
	// than presenting a partial list as the whole collection.
	Truncated bool
	// TotalPackages is the whole collection's size, so a bounded page can state
	// what it is a page of.
	TotalPackages uint32
	TotalSymbols  uint32
	TotalAtoms    uint32
}

// CodeSymbolRecord is one declaration.
type CodeSymbolRecord struct {
	Key        string
	Name       string
	Kind       string
	Receiver   string
	ImportPath string
	Path       string
	Line       uint32
	Exported   bool
	// Atom reports that the declaration carries the atom directive and its
	// documentation parsed. A declaration that carries the directive but whose
	// documentation could not be parsed is reported through AtomProblem
	// instead, because calling it an atom would claim admission it never had.
	Atom        bool
	AtomProblem string
	// Summary is the declaration's first documentation sentence, or empty when
	// it has none. It is never generated.
	Summary string
	// MatchedName and MatchedPromise say why a search returned this row: the
	// term is in the declaration's own name, or it is in the documented text
	// quoted here. They are empty outside a search.
	MatchedName    bool
	MatchedPromise string
}

// CodeSymbolPage is one bounded page of declarations.
type CodeSymbolPage struct {
	Revision  CodeCollectionRevision
	Symbols   []CodeSymbolRecord
	Truncated bool
	// TotalMatched is how many declarations matched before the bound.
	TotalMatched uint32
	// TotalAtoms is the whole collection's atom count, whatever this query
	// matched, so a filtered answer can still say what it is a part of.
	TotalAtoms uint32
	// SearchQuery is the SQL this search performed. It is empty when nothing
	// was searched.
	SearchQuery string
}

// CodeSymbolReference is one place a declaration is named.
type CodeSymbolReference struct {
	Name string
	Path string
	Line uint32
}

// CodeAtomField is one documented field of an admitted atom.
type CodeAtomField struct {
	Label string
	Text  string
	Items []string
}

// CodeSymbolStructure is what the pipeline's own atom stages measure about a
// declaration: how deeply it loops, how much it branches, and whether it
// reaches outside its arguments.
//
// Measured separates "not an atom, so nothing was measured" from "measured,
// and the answer is zero". A declaration with no loops is O(1); one that was
// never looked at is unknown, and those are not the same claim.
type CodeSymbolStructure struct {
	Measured   bool
	TimeBound  string
	SpaceClaim string
	LoopDepth  uint32
	Branches   uint32
	Pure       bool
}

// CodeSymbolDetail is one declaration read closely.
type CodeSymbolDetail struct {
	Revision CodeCollectionRevision
	Symbol   CodeSymbolRecord
	// Signature is the declaration line without its body.
	Signature string
	// Documentation is the declaration's own comment, unchanged. Nothing here
	// is summarized or generated.
	Documentation []string
	// AtomOpeningSentence and AtomFields are present only for an admitted atom.
	AtomOpeningSentence string
	AtomSchemaVersion   uint32
	AtomFields          []CodeAtomField
	// Source is the declaration exactly as written, comment included, and
	// SourceStartLine is the file line it begins at. A promise stated in a
	// comment is only worth as much as the code under it.
	Source          string
	SourceStartLine uint32
	SourceTruncated bool
	Callers         []CodeSymbolReference
	Callees         []CodeSymbolReference
	// Tests are declarations in test files that reference this one. They are
	// separated from Callers because "what exercises this" and "what depends
	// on this" are different questions, and only one of them is evidence.
	Tests []CodeSymbolReference
	// NamingChecked reports that the atom-naming grammar was run against this
	// name, and NamingFinding states the violation when there is one. A name is
	// the part of an atom's contract a caller cannot avoid reading.
	NamingChecked bool
	NamingFinding string
	// Structure is the pipeline's own structural measurement of this
	// declaration.
	Structure CodeSymbolStructure
	// DocumentedFields and MissingFields split the schema's canonical field
	// list by whether this atom declares them.
	DocumentedFields []string
	MissingFields    []string
	// AccessQuery reads this atom back out of the coordinator's atom index, and
	// IndexSchema is the table it runs against.
	AccessQuery   string
	IndexSchema   string
	Implements    []string
	ImplementedBy []string
}

// ErrCodeSymbolNotFound reports a declaration key that names nothing in the
// collection at this revision.
var ErrCodeSymbolNotFound = errors.New("code symbol not found")

// MaximumCodePage bounds one page of packages or declarations.
//
// A directory with no bound is one that returns the whole repository at once,
// which for a large collection means a page that takes longer to render the
// more code there is.
const MaximumCodePage = 500

// MaximumCodeFilePage bounds one page of files.
//
// It is far larger than a page of packages because a file row is a path and
// five numbers, and because a tree missing most of a repository is not that
// repository. A thousand-file project is ordinary; truncating it at a
// package-sized page would show an arbitrary slice of the alphabet and call it
// the directory structure.
const MaximumCodeFilePage = 5000
