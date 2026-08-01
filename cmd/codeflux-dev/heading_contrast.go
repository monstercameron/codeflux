package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// checkHeadingContrast refuses a heading that never sets its own text colour.
//
// A heading with no colour of its own takes whatever an ancestor happens to
// supply. That is legible by accident: it works wherever some container sets a
// colour and fails wherever none does, and it changes with the theme. The
// product shipped exactly that defect — the largest text on the first-run
// page rendered near-white on near-white in the light theme, and every
// automated check passed, because a colour nobody set is not a colour anybody
// can assert.
//
// The rule is deliberately narrow: it fires only on heading elements whose
// class list is built here, and it accepts a colour set either inline or by a
// helper in the same package.
func checkHeadingContrast(root string, tracked []string) error {
	fileSet := token.NewFileSet()
	var reported []string
	for _, relative := range tracked {
		slashed := filepath.ToSlash(relative)
		if !strings.HasPrefix(slashed, "web/frontend/") ||
			filepath.Ext(relative) != ".go" ||
			strings.HasSuffix(relative, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(
			fileSet, filepath.Join(root, relative), nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", relative, err)
		}
		helpers := classHelpersSettingColor(file)
		for _, finding := range headingsWithoutColor(file, fileSet, helpers) {
			reported = append(reported,
				fmt.Sprintf("%s:%d: %s", relative, finding.Line, finding.Reason))
		}
	}
	if len(reported) > 0 {
		// Every finding is reported at once. Stopping at the first turns a
		// one-pass fix into one build per heading, which is how a check ends up
		// deleted rather than satisfied.
		return fmt.Errorf("%d heading(s) will not render on the product's type scale:\n  %s",
			len(reported), strings.Join(reported, "\n  "))
	}
	return nil
}

// canonicalDesignRoleHelpers are the design package's typography role
// helpers. Each sets a text colour for every role it can return, which the
// design package asserts in its own tests.
var canonicalDesignRoleHelpers = map[string]bool{
	"HeadingClass": true,
	"ProseClass":   true,
	"ReadoutClass": true,
}

// headingFinding names one heading that will not render on the product's own
// type scale, and says which of the two ways it went wrong.
type headingFinding struct {
	Reason string
	Line   int
}

// classHelpersSettingColor returns the names of same-package functions whose
// body mentions a text colour, so a heading delegating to one is accepted.
func classHelpersSettingColor(file *ast.File) map[string]bool {
	helpers := map[string]bool{}
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Body == nil {
			continue
		}
		found := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, isSelector := node.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			// TextColor sets it outright; assistive classes hide the element
			// from sight entirely, where a colour would mean nothing.
			if selector.Sel.Name == "TextColor" {
				found = true
			}
			return true
		})
		if found || strings.Contains(strings.ToLower(function.Name.Name), "assistive") {
			helpers[function.Name.Name] = true
		}
	}
	return helpers
}

// headingsWithoutColor finds heading calls whose class expression never
// resolves to something that sets a colour.
func headingsWithoutColor(
	file *ast.File,
	fileSet *token.FileSet,
	helpers map[string]bool,
) []headingFinding {
	var findings []headingFinding
	ast.Inspect(file, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		pkg, isIdent := selector.X.(*ast.Ident)
		if !isIdent || pkg.Name != "html" || !isHeadingName(selector.Sel.Name) {
			return true
		}
		if len(call.Args) == 0 {
			return true
		}
		props, isComposite := call.Args[0].(*ast.CompositeLit)
		if !isComposite {
			return true
		}
		class := classExpression(props)
		if class == nil {
			// A heading with no class at all falls back to the browser's own
			// heading styling, which is not on this product's type scale. The
			// thread rail shipped one: an unstyled H2 rendered larger and
			// heavier than the page title beside it.
			findings = append(findings, headingFinding{
				Reason: selector.Sel.Name + " has no class, so it renders at the browser's " +
					"own heading size rather than on this product's type scale; the thread " +
					"rail shipped one that read larger than the page title beside it",
				Line: fileSet.Position(call.Pos()).Line,
			})
			return true
		}
		if expressionSetsColor(class, helpers) {
			return true
		}
		findings = append(findings, headingFinding{
			Reason: selector.Sel.Name + " builds a class list that never sets a text colour, " +
				"so it inherits one and is legible by accident; the first-run title shipped " +
				"this way and rendered near-white on near-white in the light theme",
			Line: fileSet.Position(call.Pos()).Line,
		})
		return true
	})
	return findings
}

func isHeadingName(name string) bool {
	return len(name) == 2 && name[0] == 'H' && name[1] >= '1' && name[1] <= '6'
}

// classExpression returns the Class field of an html.Props literal.
func classExpression(props *ast.CompositeLit) ast.Expr {
	for _, element := range props.Elts {
		pair, isPair := element.(*ast.KeyValueExpr)
		if !isPair {
			continue
		}
		key, isIdent := pair.Key.(*ast.Ident)
		if isIdent && key.Name == "Class" {
			return pair.Value
		}
	}
	return nil
}

// expressionSetsColor reports whether a class expression sets a text colour,
// directly or through a helper known to set one.
func expressionSetsColor(expression ast.Expr, helpers map[string]bool) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			if typed.Sel.Name == "TextColor" {
				found = true
			}
			// The design package's role helpers are the canonical way to style
			// a heading, and every role they produce sets a colour. That is
			// asserted in the design package's own tests rather than assumed
			// here, so this rule can accept them across the package boundary.
			if pkg, isIdent := typed.X.(*ast.Ident); isIdent && pkg.Name == "design" {
				if canonicalDesignRoleHelpers[typed.Sel.Name] {
					found = true
				}
			}
		case *ast.Ident:
			if helpers[typed.Name] {
				found = true
			}
		}
		return true
	})
	return found
}
