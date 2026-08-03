package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// REPO-034: a test may not call this repository's provider-key resolver
// without an explicit, visible opt-in.
//
// engine_produces_program_test.go was the only test that called
// ReadProviderKey, and for a while it did so on the strength of a key being
// present alone: any `go test ./...` that happened to run on a machine with
// OPENAI_API_KEY set -- including the one the pre-push hook runs -- spent
// real money on 250 live model runs. It is gated now behind CODEFLUX_LADDER,
// a distinct environment variable checked first, whose absence skips the
// test before the key is ever read. Nothing before this rule stopped the
// next provider-backed test from calling ReadProviderKey the same
// ungated way, so lint refuses one that does.
//
// The rule matches the resolver call itself, not every environment read
// whose name happens to look like a credential. Two real tests in this
// repository read OPENAI_API_KEY for reasons that have nothing to do with
// calling a provider: internal/worker/environment_test.go reads it back out
// of a synthetic child-process environment to prove a credential-leak
// prevention actually strips it, and internal/executor/command_test.go
// prints it into a captured subprocess stream as a redaction fixture.
// Neither can spend a cent, and gating either behind an opt-in would
// silently disable a security test rather than prevent spend. ReadProviderKey
// is the one call path that actually resolves a usable credential -- from
// the environment or from .env -- so it is the one signal this rule matches.
var (
	// providerKeyEnvironmentNames mirrors ProviderKeyNames in
	// internal/coordinator/agent_credentials.go. It is duplicated rather
	// than imported so this rule does not pull internal/coordinator -- a
	// package with a real SQLite and provider dependency graph -- into the
	// development tool's own build for the sake of two string constants. It
	// is used only to recognise the provider-key names an opt-in gate must
	// stay distinct from, never to match a raw environment read as key
	// access in its own right.
	providerKeyEnvironmentNames = stringSet("OPENAI_API_KEY", "OPENAI_KEY")

	readProviderKeyFunctionName = "ReadProviderKey"
)

// checkProviderKeyTestsAreGated refuses a tracked _test.go function that
// calls ReadProviderKey unless the same function first checks a distinct
// environment variable and skips when it is unset, the shape
// TestTheEngineProducesProgramsThatBuildAndRun already uses with
// CODEFLUX_LADDER.
func checkProviderKeyTestsAreGated(root string, tracked []string) error {
	files := token.NewFileSet()
	for _, relative := range tracked {
		if !strings.HasSuffix(relative, "_test.go") {
			continue
		}
		sourcePath := filepath.Join(root, relative)
		file, err := parser.ParseFile(files, sourcePath, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s for provider-key gating: %w", filepath.ToSlash(relative), err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			if err := checkFunctionGatesProviderKeyAccess(files, filepath.ToSlash(relative), function); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkFunctionGatesProviderKeyAccess(files *token.FileSet, relative string, function *ast.FuncDecl) error {
	var (
		keyAccessPos   token.Pos
		keyAccessFound bool
		keyAccessWhat  string
	)

	// ReadProviderKey is flagged wherever it is called, qualified or not: it
	// exists for one reason, to resolve a usable credential, so calling it
	// is never incidental. See the package-level comment above for why a
	// raw environment read of a key-shaped name is deliberately not matched
	// here.
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !calls(call, readProviderKeyFunctionName) {
			return true
		}
		if !keyAccessFound || call.Pos() < keyAccessPos {
			keyAccessFound = true
			keyAccessPos = call.Pos()
			keyAccessWhat = readProviderKeyFunctionName
		}
		return true
	})

	if !keyAccessFound {
		return nil
	}

	// The gate does not have to sit inside the if statement that skips: the
	// established shape reads the distinct environment variable into a
	// variable first (CODEFLUX_LADDER via strings.TrimSpace(os.Getenv(...)))
	// and branches on that variable afterward. So a function gates its key
	// access when, before the access, it both (a) reads some environment
	// variable other than a provider key, and (b) has an if statement that
	// calls Skip/Skipf -- not necessarily the same statement, since proving
	// the skip's condition is data-flow-dependent on the earlier read would
	// require more than syntactic analysis. The two-part, position-ordered
	// requirement still refuses the two shapes that matter: no distinct
	// environment check at all, and a distinct check or skip that comes
	// after the key was already read.
	hasDistinctEnvironmentRead := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if hasDistinctEnvironmentRead {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || call.Pos() >= keyAccessPos {
			return true
		}
		if name, ok := getenvArgument(call); ok && !providerKeyEnvironmentNames[name] {
			hasDistinctEnvironmentRead = true
		}
		return true
	})

	hasPrecedingSkipGate := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if hasPrecedingSkipGate {
			return false
		}
		ifStatement, ok := node.(*ast.IfStmt)
		if !ok || ifStatement.Pos() >= keyAccessPos {
			return true
		}
		if ifStatementCallsSkip(ifStatement) {
			hasPrecedingSkipGate = true
		}
		return true
	})

	if hasDistinctEnvironmentRead && hasPrecedingSkipGate {
		return nil
	}

	position := files.Position(keyAccessPos)
	return fmt.Errorf(
		"%s:%d: %s reads a provider key via %s with no preceding environment "+
			"opt-in gate (a distinct os.Getenv/os.LookupEnv check whose failure "+
			"calls t.Skip); see CODEFLUX_LADDER in "+
			"internal/coordinator/engine_produces_program_test.go for the required shape",
		relative, position.Line, function.Name.Name, keyAccessWhat,
	)
}

// calls reports whether call invokes the named function, either as a bare
// identifier or through a selector such as pkg.Name or receiver.Name.
func calls(call *ast.CallExpr, name string) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == name
	case *ast.SelectorExpr:
		return fun.Sel.Name == name
	default:
		return false
	}
}

// getenvArgument reports the literal environment-variable name passed to an
// os.Getenv or os.LookupEnv call, when the argument is a string literal.
func getenvArgument(call *ast.CallExpr) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	packageIdent, ok := selector.X.(*ast.Ident)
	if !ok || packageIdent.Name != "os" {
		return "", false
	}
	if selector.Sel.Name != "Getenv" && selector.Sel.Name != "LookupEnv" {
		return "", false
	}
	if len(call.Args) == 0 {
		return "", false
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// ifStatementCallsSkip reports whether an if statement's body calls a
// *.Skip or *.Skipf method, the shape a test uses to bail out before
// reaching the provider key when the opt-in is absent.
func ifStatementCallsSkip(ifStatement *ast.IfStmt) bool {
	found := false
	ast.Inspect(ifStatement.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "Skip" || selector.Sel.Name == "Skipf" {
			found = true
		}
		return true
	})
	return found
}
