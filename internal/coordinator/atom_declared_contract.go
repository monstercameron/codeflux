package coordinator

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/atomdoc"
)

// declaredContracts parses every source-authored //codeflux:atom doc comment
// among the given files and returns each declaration's validated schema-v1
// Document, keyed by the Go identifier it documents (PIPE-137).
//
// This is the specification side of a contract: a Document is text the
// atom's own author wrote about what it promises — its Effects, its
// Preconditions, its Postconditions — extracted from the doc comment rather
// than computed by inspecting the body of the function it documents. It can
// therefore disagree with what that body actually does, which is exactly the
// property a contract "derived from the finished code" can never have: two
// facts computed from the same source by the same walk cannot help but
// agree. AGENTS.md's "Atom Documentation Style" already requires this
// schema; this reads it rather than inventing a second one.
//
// A function with no admitted //codeflux:atom directive, or whose comment
// fails schema validation (internal/atomdoc.ValidateAtomDocumentationSchema),
// has no entry in the returned map. That is a distinct, honest fact from
// "declared and agrees" or "declared and disagrees" — a caller must not
// fabricate a contract for it, which is the defect PIPE-137 exists to
// remove, not reintroduce one function away from where it was found.
func declaredContracts(
	worktree string, files []string,
) (map[string]atomdoc.Document, error) {
	documents := map[string]atomdoc.Document{}
	fileSet := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			// AGENTS.md's directive marks "each source-authored Go atom
			// declaration"; a test file is the checking apparatus, not an
			// atom, and it is never asked to carry this schema.
			continue
		}
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil, parser.ParseComments)
		if parseErr != nil {
			// A file that fails to parse contributes no declared contracts.
			// The caller already fails independently on a source parse
			// error through readProducedFunctions; this must not fail a
			// second time for the same reason under a different message.
			continue
		}
		candidates, locateErr := atomdoc.LocateAtomDeclarationCandidates(fileSet, tree)
		if locateErr != nil {
			// A directive present but not attached to a supported
			// declaration is an authoring error, not a fact about any one
			// atom's contract, and it names no identifier to record a
			// Document against. It is left for the doc-authoring checks this
			// ticket does not own; here it is simply not a contract.
			continue
		}
		for _, candidate := range candidates {
			parsed, parseCommentErr := atomdoc.ParseAtomDocumentationComment(candidate)
			if parseCommentErr != nil {
				continue
			}
			document, validateErr := atomdoc.ValidateAtomDocumentationSchema(parsed.Fields)
			if validateErr != nil {
				continue
			}
			documents[candidate.Identifier] = document
		}
	}
	return documents, nil
}

// declaredPurity reads a Document's Effects field the way AGENTS.md's
// schema-v1 defines purity: "Write 'None: pure atom' when pure." Any other
// content — one or more named effects, or an explained absence that is not
// itself "None" — is not a declaration of purity.
func declaredPurity(document atomdoc.Document) bool {
	return document.Effects.None
}

// declaredEffectNames lists what a Document's Effects field names, for
// evidence and for comparison against what was actually observed. A
// declared-pure document names none, by declaredPurity's own rule.
func declaredEffectNames(document atomdoc.Document) []string {
	if document.Effects.None {
		return nil
	}
	return append([]string(nil), document.Effects.Items...)
}
