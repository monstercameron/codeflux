package workspace

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	maxMapFileBytes  = 2 << 20
	maxMapTotalBytes = 64 << 20
)

var generatedFilePattern = regexp.MustCompile(`(?i)^// Code generated .* DO NOT EDIT\.$`)

var ErrNoGoModule = errors.New("repository contains no Go module")

type RepositoryMap struct {
	RepositoryIdentity string
	RepositoryRevision string
	MapRevision        string
	Modules            []GoModule
	Packages           []GoPackage
	Files              []MappedFile
	Symbols            []GoSymbol
	References         []GoReference
	Calls              []GoCall
	Implementations    []GoImplementation
	Commands           []SuggestedCommand
	Instructions       []RepositoryInstruction
	Warnings           []MapWarning
}

type GoModule struct {
	Path      string
	GoVersion string
	Directory string
	GoModPath string
}

type GoPackage struct {
	ImportPath  string
	Name        string
	Directory   string
	ModulePath  string
	SourceFiles []string
	TestFiles   []string
	Imports     []string
	BuildTarget string
}

type MappedFile struct {
	Path             string
	SHA256           string
	Kind             string
	Generated        bool
	BuildConstraints []string
}

type GoSymbol struct {
	Name     string
	Kind     string
	Receiver string
	Path     string
	Line     int
	Exported bool
}

type GoReference struct {
	Name           string
	Path           string
	Line           int
	ResolvedSymbol string
}

type GoCall struct {
	Caller string
	Callee string
	Path   string
	Line   int
}

type GoImplementation struct {
	Interface string
	Concrete  string
	Methods   []string
}

type SuggestedCommand struct {
	Kind             string
	Arguments        []string
	Source           string
	RequiresApproval bool
}

type RepositoryInstruction struct {
	Path             string
	Kind             string
	Trust            string
	RequiresApproval bool
}

type MapWarning struct {
	Path    string
	Stage   string
	Message string
}

type goListPackage struct {
	ImportPath     string
	Name           string
	Dir            string
	GoFiles        []string
	CgoFiles       []string
	TestGoFiles    []string
	XTestGoFiles   []string
	IgnoredGoFiles []string
	InvalidGoFiles []string
	Imports        []string
	TestImports    []string
	Target         string
	Module         *struct {
		Path      string
		Dir       string
		GoMod     string
		GoVersion string
	}
	Error *struct {
		Err string
	}
}

// BuildRepositoryMap constructs a deterministic, revision-bound map. It uses
// fixed go-list arguments with network-disabled execution and continues past
// individual package parse failures by recording bounded warnings.
func BuildRepositoryMap(
	ctx context.Context,
	snapshot RepositorySnapshot,
	runner CommandRunner,
) (RepositoryMap, error) {
	if runner == nil {
		return RepositoryMap{}, errors.New("command runner must not be nil")
	}
	if snapshot.CanonicalRoot == "" || !validRevision(snapshot.HeadRevision) ||
		snapshot.GitIdentity == "" {
		return RepositoryMap{}, errors.New("repository snapshot is incomplete")
	}

	moduleFiles, instructionFiles, projectFiles, err := discoverMapInputs(snapshot.CanonicalRoot)
	if err != nil {
		return RepositoryMap{}, err
	}
	if len(moduleFiles) == 0 {
		return RepositoryMap{}, ErrNoGoModule
	}
	result := RepositoryMap{
		RepositoryIdentity: snapshot.GitIdentity,
		RepositoryRevision: snapshot.HeadRevision,
	}
	packageIndex := make(map[string]GoPackage)
	moduleIndex := make(map[string]GoModule)
	for _, moduleFile := range moduleFiles {
		directory := filepath.Dir(filepath.Join(snapshot.CanonicalRoot, filepath.FromSlash(moduleFile)))
		packages, warnings, listErr := listGoPackages(ctx, runner, snapshot.CanonicalRoot, directory)
		result.Warnings = append(result.Warnings, warnings...)
		if listErr != nil {
			result.Warnings = append(result.Warnings, MapWarning{
				Path:    moduleFile,
				Stage:   "go-list",
				Message: boundedMessage(listErr.Error()),
			})
			continue
		}
		for _, listed := range packages {
			relativeDirectory, inside := repositoryRelative(snapshot.CanonicalRoot, listed.Dir)
			if !inside {
				continue
			}
			key := listed.ImportPath + "\x00" + relativeDirectory
			pkg := convertListedPackage(snapshot.CanonicalRoot, listed)
			if existing, found := packageIndex[key]; found {
				pkg.SourceFiles = sortedUnique(append(existing.SourceFiles, pkg.SourceFiles...))
				pkg.TestFiles = sortedUnique(append(existing.TestFiles, pkg.TestFiles...))
				pkg.Imports = sortedUnique(append(existing.Imports, pkg.Imports...))
			}
			packageIndex[key] = pkg
			if listed.Module != nil {
				moduleDirectory, moduleInside := repositoryRelative(snapshot.CanonicalRoot, listed.Module.Dir)
				goModPath, goModInside := repositoryRelative(snapshot.CanonicalRoot, listed.Module.GoMod)
				if moduleInside && goModInside {
					moduleIndex[listed.Module.Path+"\x00"+moduleDirectory] = GoModule{
						Path:      listed.Module.Path,
						GoVersion: listed.Module.GoVersion,
						Directory: moduleDirectory,
						GoModPath: goModPath,
					}
				}
			}
		}
	}
	for _, pkg := range packageIndex {
		result.Packages = append(result.Packages, pkg)
	}
	for _, module := range moduleIndex {
		result.Modules = append(result.Modules, module)
	}
	sortRepositoryMap(&result)

	fileSet := token.NewFileSet()
	definitions := make(map[string][]GoSymbol)
	typeMethods := make(map[string]map[string]struct{})
	interfaceMethods := make(map[string][]string)
	totalBytes := int64(0)
	for _, pkg := range result.Packages {
		files := sortedUnique(append(append([]string(nil), pkg.SourceFiles...), pkg.TestFiles...))
		for _, relative := range files {
			mapped, parsed, readBytes, mapErr := parseMappedGoFile(
				fileSet,
				snapshot.CanonicalRoot,
				relative,
			)
			totalBytes += readBytes
			if totalBytes > maxMapTotalBytes {
				return RepositoryMap{}, fmt.Errorf("repository map input exceeds %d bytes", maxMapTotalBytes)
			}
			if mapped.Path != "" {
				result.Files = append(result.Files, mapped)
			}
			if mapErr != nil {
				result.Warnings = append(result.Warnings, MapWarning{
					Path: relative, Stage: "parse", Message: boundedMessage(mapErr.Error()),
				})
				continue
			}
			indexAST(fileSet, relative, parsed, &result, definitions, typeMethods, interfaceMethods)
		}
	}
	addNonGoMapInputs(snapshot.CanonicalRoot, append(append(moduleFiles, instructionFiles...), projectFiles...), &result)
	resolveReferences(&result, definitions)
	result.Implementations = resolveImplementations(interfaceMethods, typeMethods)
	result.Commands = discoverCommands(projectFiles)
	result.Instructions = classifyInstructions(instructionFiles)
	sortRepositoryMap(&result)
	result.MapRevision = calculateMapRevision(result)
	return result, nil
}

func discoverMapInputs(root string) ([]string, []string, []string, error) {
	var modules, instructions, projectFiles []string
	visited := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited++
		if visited > maxDiscoveryFiles {
			return ErrOutputLimit
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && relative != "." {
			switch entry.Name() {
			case ".git", ".artifacts", "vendor", "node_modules":
				return filepath.SkipDir
			}
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		slashed := filepath.ToSlash(relative)
		switch entry.Name() {
		case "go.mod":
			modules = append(modules, slashed)
		case "go.work":
			projectFiles = append(projectFiles, slashed)
		case "AGENTS.md", "CLAUDE.md", "CODEX.md":
			instructions = append(instructions, slashed)
		case "Makefile", "Taskfile.yml", "Taskfile.yaml", ".golangci.yml", ".golangci.yaml":
			projectFiles = append(projectFiles, slashed)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("discover Go repository inputs: %w", err)
	}
	slices.Sort(modules)
	slices.Sort(instructions)
	slices.Sort(projectFiles)
	return modules, instructions, projectFiles, nil
}

func listGoPackages(
	ctx context.Context,
	runner CommandRunner,
	root string,
	directory string,
) ([]goListPackage, []MapWarning, error) {
	result, runErr := runner.Run(ctx, directory, "go", "list", "-e", "-json", "./...")
	if runErr != nil && len(result.Stdout) == 0 {
		return nil, nil, fmt.Errorf("run bounded go list: %w: %s", runErr, boundedDiagnostic(result.Stderr))
	}
	decoder := json.NewDecoder(strings.NewReader(string(result.Stdout)))
	var packages []goListPackage
	var warnings []MapWarning
	for {
		var listed goListPackage
		err := decoder.Decode(&listed)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return packages, warnings, fmt.Errorf("decode go list: %w", err)
		}
		packages = append(packages, listed)
		if listed.Error != nil {
			relative, _ := repositoryRelative(root, listed.Dir)
			warnings = append(warnings, MapWarning{
				Path: relative, Stage: "go-list", Message: boundedMessage(listed.Error.Err),
			})
		}
	}
	return packages, warnings, nil
}

func convertListedPackage(root string, listed goListPackage) GoPackage {
	directory, _ := repositoryRelative(root, listed.Dir)
	sourceFiles := make(
		[]string,
		0,
		len(listed.GoFiles)+len(listed.CgoFiles)+len(listed.IgnoredGoFiles)+len(listed.InvalidGoFiles),
	)
	allSourceFiles := append(append(append(append(
		[]string(nil),
		listed.GoFiles...,
	), listed.CgoFiles...), listed.IgnoredGoFiles...), listed.InvalidGoFiles...)
	for _, name := range allSourceFiles {
		sourceFiles = append(sourceFiles, joinRepositoryPath(directory, name))
	}
	var testFiles []string
	for _, name := range append(append([]string(nil), listed.TestGoFiles...), listed.XTestGoFiles...) {
		testFiles = append(testFiles, joinRepositoryPath(directory, name))
	}
	modulePath := ""
	if listed.Module != nil {
		modulePath = listed.Module.Path
	}
	target, _ := repositoryRelative(root, listed.Target)
	if target == "" {
		target = listed.ImportPath
	}
	return GoPackage{
		ImportPath:  listed.ImportPath,
		Name:        listed.Name,
		Directory:   directory,
		ModulePath:  modulePath,
		SourceFiles: sortedUnique(sourceFiles),
		TestFiles:   sortedUnique(testFiles),
		Imports:     sortedUnique(append(append([]string(nil), listed.Imports...), listed.TestImports...)),
		BuildTarget: target,
	}
}

func parseMappedGoFile(
	fileSet *token.FileSet,
	root string,
	relative string,
) (MappedFile, *ast.File, int64, error) {
	if !safeRelativePath(relative) {
		return MappedFile{}, nil, 0, errors.New("unsafe mapped path")
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Stat(path)
	if err != nil {
		return MappedFile{}, nil, 0, err
	}
	if info.Size() > maxMapFileBytes {
		return MappedFile{}, nil, info.Size(), fmt.Errorf("file exceeds %d bytes", maxMapFileBytes)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return MappedFile{}, nil, 0, err
	}
	digest := sha256.Sum256(content)
	mapped := MappedFile{
		Path:             relative,
		SHA256:           hex.EncodeToString(digest[:]),
		Kind:             mappedFileKind(relative),
		Generated:        isGeneratedGo(content),
		BuildConstraints: readBuildConstraints(content),
	}
	parsed, parseErr := parser.ParseFile(fileSet, relative, content, parser.ParseComments|parser.AllErrors)
	return mapped, parsed, int64(len(content)), parseErr
}

func indexAST(
	fileSet *token.FileSet,
	relative string,
	file *ast.File,
	result *RepositoryMap,
	definitions map[string][]GoSymbol,
	typeMethods map[string]map[string]struct{},
	interfaceMethods map[string][]string,
) {
	definedPositions := make(map[token.Pos]struct{})
	currentFunction := ""
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncDecl:
			receiver := receiverName(value.Recv)
			symbol := GoSymbol{
				Name: value.Name.Name, Kind: "function", Receiver: receiver,
				Path: relative, Line: fileSet.Position(value.Name.Pos()).Line,
				Exported: ast.IsExported(value.Name.Name),
			}
			if receiver != "" {
				symbol.Kind = "method"
				if typeMethods[receiver] == nil {
					typeMethods[receiver] = make(map[string]struct{})
				}
				typeMethods[receiver][symbol.Name] = struct{}{}
			}
			addDefinition(result, definitions, symbol)
			definedPositions[value.Name.Pos()] = struct{}{}
			previous := currentFunction
			currentFunction = qualifiedSymbol(receiver, value.Name.Name)
			ast.Inspect(value.Body, func(bodyNode ast.Node) bool {
				if call, ok := bodyNode.(*ast.CallExpr); ok {
					if callee := callName(call.Fun); callee != "" {
						result.Calls = append(result.Calls, GoCall{
							Caller: currentFunction,
							Callee: callee,
							Path:   relative,
							Line:   fileSet.Position(call.Pos()).Line,
						})
					}
				}
				return true
			})
			currentFunction = previous
		case *ast.TypeSpec:
			kind := "type"
			iface, isInterface := value.Type.(*ast.InterfaceType)
			if isInterface {
				kind = "interface"
				interfaceMethods[value.Name.Name] = interfaceMethodNames(iface)
			}
			symbol := GoSymbol{
				Name: value.Name.Name, Kind: kind, Path: relative,
				Line:     fileSet.Position(value.Name.Pos()).Line,
				Exported: ast.IsExported(value.Name.Name),
			}
			addDefinition(result, definitions, symbol)
			definedPositions[value.Name.Pos()] = struct{}{}
		case *ast.ValueSpec:
			for _, name := range value.Names {
				symbol := GoSymbol{
					Name: name.Name, Kind: "value", Path: relative,
					Line:     fileSet.Position(name.Pos()).Line,
					Exported: ast.IsExported(name.Name),
				}
				addDefinition(result, definitions, symbol)
				definedPositions[name.Pos()] = struct{}{}
			}
		case *ast.Ident:
			if value.Name == "_" {
				return true
			}
			if _, defined := definedPositions[value.Pos()]; !defined {
				result.References = append(result.References, GoReference{
					Name: value.Name, Path: relative, Line: fileSet.Position(value.Pos()).Line,
				})
			}
		}
		return true
	})
}

func addDefinition(
	result *RepositoryMap,
	definitions map[string][]GoSymbol,
	symbol GoSymbol,
) {
	result.Symbols = append(result.Symbols, symbol)
	definitions[symbol.Name] = append(definitions[symbol.Name], symbol)
}

func resolveReferences(result *RepositoryMap, definitions map[string][]GoSymbol) {
	for index := range result.References {
		candidates := definitions[result.References[index].Name]
		if len(candidates) == 1 {
			result.References[index].ResolvedSymbol = symbolIdentity(candidates[0])
		}
	}
}

func resolveImplementations(
	interfaces map[string][]string,
	types map[string]map[string]struct{},
) []GoImplementation {
	var implementations []GoImplementation
	for interfaceName, required := range interfaces {
		if len(required) == 0 {
			continue
		}
		for concrete, available := range types {
			matches := true
			for _, method := range required {
				if _, exists := available[method]; !exists {
					matches = false
					break
				}
			}
			if matches {
				implementations = append(implementations, GoImplementation{
					Interface: interfaceName,
					Concrete:  concrete,
					Methods:   append([]string(nil), required...),
				})
			}
		}
	}
	slices.SortFunc(implementations, func(left, right GoImplementation) int {
		if compared := strings.Compare(left.Interface, right.Interface); compared != 0 {
			return compared
		}
		return strings.Compare(left.Concrete, right.Concrete)
	})
	return implementations
}

func addNonGoMapInputs(root string, paths []string, result *RepositoryMap) {
	for _, relative := range sortedUnique(paths) {
		if !safeRelativePath(relative) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || len(content) > maxMapFileBytes {
			continue
		}
		digest := sha256.Sum256(content)
		result.Files = append(result.Files, MappedFile{
			Path: relative, SHA256: hex.EncodeToString(digest[:]), Kind: mappedFileKind(relative),
		})
	}
}

func discoverCommands(projectFiles []string) []SuggestedCommand {
	commands := []SuggestedCommand{
		{Kind: "format", Arguments: []string{"gofmt", "-w"}, Source: "go-toolchain"},
		{Kind: "test", Arguments: []string{"go", "test", "./..."}, Source: "go-toolchain"},
		{Kind: "build", Arguments: []string{"go", "build", "./..."}, Source: "go-toolchain"},
	}
	for _, path := range projectFiles {
		name := filepath.Base(path)
		switch name {
		case "Makefile":
			commands = append(commands, SuggestedCommand{
				Kind: "repository-suggested", Arguments: []string{"make"}, Source: path, RequiresApproval: true,
			})
		case "Taskfile.yml", "Taskfile.yaml":
			commands = append(commands, SuggestedCommand{
				Kind: "repository-suggested", Arguments: []string{"task"}, Source: path, RequiresApproval: true,
			})
		case ".golangci.yml", ".golangci.yaml":
			commands = append(commands, SuggestedCommand{
				Kind: "lint", Arguments: []string{"golangci-lint", "run"}, Source: path, RequiresApproval: true,
			})
		}
	}
	return commands
}

func classifyInstructions(paths []string) []RepositoryInstruction {
	result := make([]RepositoryInstruction, 0, len(paths))
	for _, path := range paths {
		result = append(result, RepositoryInstruction{
			Path: path, Kind: filepath.Base(path), Trust: "untrusted-repository-data", RequiresApproval: true,
		})
	}
	return result
}

func sortRepositoryMap(result *RepositoryMap) {
	slices.SortFunc(result.Modules, func(left, right GoModule) int {
		if compared := strings.Compare(left.Path, right.Path); compared != 0 {
			return compared
		}
		return strings.Compare(left.Directory, right.Directory)
	})
	slices.SortFunc(result.Packages, func(left, right GoPackage) int {
		if compared := strings.Compare(left.ImportPath, right.ImportPath); compared != 0 {
			return compared
		}
		return strings.Compare(left.Directory, right.Directory)
	})
	slices.SortFunc(result.Files, func(left, right MappedFile) int {
		return strings.Compare(left.Path, right.Path)
	})
	result.Files = slices.CompactFunc(result.Files, func(left, right MappedFile) bool {
		return left.Path == right.Path
	})
	slices.SortFunc(result.Symbols, func(left, right GoSymbol) int {
		return strings.Compare(symbolIdentity(left), symbolIdentity(right))
	})
	slices.SortFunc(result.References, func(left, right GoReference) int {
		return strings.Compare(referenceIdentity(left), referenceIdentity(right))
	})
	slices.SortFunc(result.Calls, func(left, right GoCall) int {
		return strings.Compare(callIdentity(left), callIdentity(right))
	})
	slices.SortFunc(result.Commands, func(left, right SuggestedCommand) int {
		return strings.Compare(strings.Join(left.Arguments, "\x00"), strings.Join(right.Arguments, "\x00"))
	})
	slices.SortFunc(result.Instructions, func(left, right RepositoryInstruction) int {
		return strings.Compare(left.Path, right.Path)
	})
	slices.SortFunc(result.Warnings, func(left, right MapWarning) int {
		return strings.Compare(left.Path+"\x00"+left.Stage+"\x00"+left.Message, right.Path+"\x00"+right.Stage+"\x00"+right.Message)
	})
}

func calculateMapRevision(result RepositoryMap) string {
	result.MapRevision = ""
	encoded, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func receiverName(receivers *ast.FieldList) string {
	if receivers == nil || len(receivers.List) == 0 {
		return ""
	}
	switch value := receivers.List[0].Type.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		if identifier, ok := value.X.(*ast.Ident); ok {
			return identifier.Name
		}
	}
	return ""
}

func interfaceMethodNames(value *ast.InterfaceType) []string {
	var names []string
	for _, method := range value.Methods.List {
		for _, name := range method.Names {
			names = append(names, name.Name)
		}
	}
	slices.Sort(names)
	return names
}

func callName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		prefix := callName(value.X)
		if prefix == "" {
			return value.Sel.Name
		}
		return prefix + "." + value.Sel.Name
	case *ast.IndexExpr:
		return callName(value.X)
	case *ast.IndexListExpr:
		return callName(value.X)
	default:
		return ""
	}
}

func qualifiedSymbol(receiver, name string) string {
	if receiver == "" {
		return name
	}
	return receiver + "." + name
}

func symbolIdentity(symbol GoSymbol) string {
	return symbol.Path + ":" + strconv.Itoa(symbol.Line) + ":" + qualifiedSymbol(symbol.Receiver, symbol.Name)
}

func referenceIdentity(reference GoReference) string {
	return reference.Path + ":" + strconv.Itoa(reference.Line) + ":" + reference.Name
}

func callIdentity(call GoCall) string {
	return call.Path + ":" + strconv.Itoa(call.Line) + ":" + call.Caller + ":" + call.Callee
}

func isGeneratedGo(content []byte) bool {
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for line := 0; scanner.Scan() && line < 20; line++ {
		if generatedFilePattern.MatchString(strings.TrimSpace(scanner.Text())) {
			return true
		}
	}
	return false
}

func readBuildConstraints(content []byte) []string {
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	var constraints []string
	for line := 0; scanner.Scan() && line < 50; line++ {
		text := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(text, "//go:build ") {
			constraints = append(constraints, strings.TrimSpace(strings.TrimPrefix(text, "//go:build ")))
		}
		if text != "" && !strings.HasPrefix(text, "//") {
			break
		}
	}
	return constraints
}

func mappedFileKind(path string) string {
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return "test"
	case strings.HasSuffix(base, ".go"):
		return "source"
	case base == "go.mod" || base == "go.work":
		return "module"
	case base == "AGENTS.md" || base == "CLAUDE.md" || base == "CODEX.md":
		return "instruction"
	default:
		return "configuration"
	}
}

func repositoryRelative(root, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(relative)), true
}

func joinRepositoryPath(directory, name string) string {
	if directory == "." || directory == "" {
		return filepath.ToSlash(name)
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(directory), name))
}

func sortedUnique(values []string) []string {
	result := append([]string(nil), values...)
	slices.Sort(result)
	return slices.Compact(result)
}

func boundedMessage(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if len(value) > 512 {
		return value[:512]
	}
	return value
}
