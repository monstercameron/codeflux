package coordinator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// decodingBoundaryPrefixes name functions that turn input into structure.
//
// The list is declared rather than inferred so the rule can be argued with. A
// boundary is where untrusted bytes become a value the rest of the program
// trusts, and those are the places fuzzing is for.
var decodingBoundaryPrefixes = []string{
	"Parse", "Decode", "Unmarshal", "Scan", "Read", "Load", "FromBytes",
	"FromString", "FromJSON",
}

// decodingBoundaries enumerates the produced functions that decode input
// (PIPE-014).
//
// A function counts when either signal holds:
//
//   - its name begins with a decoding verb; or
//   - it takes a string or []byte and returns an error alongside its value,
//     which is the shape of "turn this input into a value, or say why not".
//
// Both are readings of the signature rather than proof that a function parses
// anything. That is deliberate: the enumeration decides whether the absence of
// fuzzing is a gap or a non-question, and over-reporting a boundary asks for a
// fuzz target that may not be needed, while under-reporting hides a missing
// one. The first is a nuisance and the second is the defect.
func decodingBoundaries(worktree string) ([]string, error) {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	fileSet := token.NewFileSet()
	var boundaries []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			continue
		}
		for _, declaration := range tree.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Name == nil {
				continue
			}
			if isDecodingBoundary(function) {
				boundaries = append(boundaries, function.Name.Name)
			}
		}
	}
	return boundaries, nil
}

// isDecodingBoundary applies the two signals to one declaration.
func isDecodingBoundary(function *ast.FuncDecl) bool {
	name := function.Name.Name
	for _, prefix := range decodingBoundaryPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return takesRawInput(function) && returnsError(function)
}

// takesRawInput reports whether any parameter is a string or []byte.
func takesRawInput(function *ast.FuncDecl) bool {
	if function.Type == nil || function.Type.Params == nil {
		return false
	}
	for _, parameter := range function.Type.Params.List {
		switch typed := parameter.Type.(type) {
		case *ast.Ident:
			if typed.Name == "string" {
				return true
			}
		case *ast.ArrayType:
			if element, ok := typed.Elt.(*ast.Ident); ok && element.Name == "byte" {
				return true
			}
		}
	}
	return false
}

// returnsError reports whether the function reports failure to its caller.
func returnsError(function *ast.FuncDecl) bool {
	if function.Type == nil || function.Type.Results == nil {
		return false
	}
	for _, result := range function.Type.Results.List {
		if identifier, ok := result.Type.(*ast.Ident); ok &&
			identifier.Name == "error" {
			return true
		}
	}
	return false
}
