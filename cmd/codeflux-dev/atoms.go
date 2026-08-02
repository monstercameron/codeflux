package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const (
	atomMarker              = "codeflux:atom"
	atomDocumentationHeader = "Codeflux atom documentation (schema v1):"
)

var (
	atomDocumentationFields = []string{
		"Purpose",
		"Use when",
		"Do not use when",
		"Semantics",
		"Inputs",
		"Outputs",
		"Preconditions",
		"Postconditions",
		"Effects",
		"Failure semantics",
		"Determinism",
		"Idempotency and retry",
		"Reconciliation and compensation",
		"Security and privacy",
		"Dependencies and bindings",
		"Complexity and limits",
		"Examples",
		"Verification",
		"Retrieval concepts",
	}
	atomWordPattern     = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]*`)
	atomHashPattern     = regexp.MustCompile(`[A-Fa-f0-9]{8,}`)
	atomVersionPattern  = regexp.MustCompile(`(?i)(?:version|v)[0-9]+`)
	genericAtomNames    = stringSet("Process", "Handle", "Execute", "Run", "Check", "Update", "Helper", "Manager")
	fillerAtomSuffixes  = stringSet("Helper", "Utility", "Manager", "Processor", "Handler", "Thing", "Impl")
	concreteActionVerbs = stringSet(
		"Add", "Apply", "Approve", "Attribute", "Build", "Calculate", "Cancel", "Capture",
		"Check", "Classify", "Compare", "Create", "Decode", "Delete", "Derive",
		"Detect", "Dispatch", "Encode", "Execute", "Extract", "Fetch", "Generate",
		"Inspect", "Invalidate", "Load", "Map", "Normalize", "Parse", "Pause",
		"Persist", "Publish", "Read", "Reconcile", "Record", "Reduce", "Reject",
		"Render", "Reserve", "Resolve", "Restore", "Resume", "Retry", "Run", "Save",
		"Scan", "Select", "Send", "Start", "Stop", "Store", "Stream", "Update",
		"Validate", "Verify", "Write",
	)
	establishedAbbreviations = stringSet(
		"AI", "API", "AST", "CLI", "CPU", "CSS", "HTML", "HTTP", "HTTPS", "ID",
		"JSON", "OS", "RPC", "SQL", "UI", "URI", "URL", "UUID", "UX", "WASM", "XML",
	)
	misleadingGuaranteeWords = stringSet("Always", "Correct", "Complete", "Guarantee", "Guaranteed", "Never", "Perfect", "Safe", "Secure")
	providerNameFragments    = []string{"anthropic", "azureopenai", "claude", "gemini", "openai"}
)

type atomNameExceptions struct {
	actionVerb       string
	abbreviations    map[string]string
	providerSpecific string
	guaranteeClaim   string
}

type atomNameRecord struct {
	AtomIdentity     string
	CanonicalGoName  string
	DisplayName      string
	NormalizedPhrase string
}

type atomRenameClassification string

const (
	atomRenameUnchanged          atomRenameClassification = "unchanged"
	atomRenameSemanticPreserving atomRenameClassification = "semantic-preserving"
	atomRenameSemanticBreaking   atomRenameClassification = "semantic-breaking"
)

func checkAtomDeclarations(root string, tracked []string) error {
	for _, relative := range tracked {
		slashed := filepath.ToSlash(relative)
		if filepath.Ext(relative) != ".go" || strings.Contains(slashed, "/testdata/") ||
			strings.HasSuffix(relative, ".pb.go") || strings.HasSuffix(relative, "_gen.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return err
		}
		if err := validateAtomSource(slashed, source); err != nil {
			return err
		}
	}
	return nil
}

func validateAtomSource(path string, source []byte) error {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, source, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("%s: parse atom declarations: %w", path, err)
	}
	consumed := make(map[*ast.CommentGroup]bool)
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if hasAtomMarker(typed.Doc) {
				consumed[typed.Doc] = true
				if err := validateAtomDeclaration(typed.Name.Name, typed.Doc); err != nil {
					return fmt.Errorf("%s:%d: %w", path, fileSet.Position(typed.Pos()).Line, err)
				}
			}
		case *ast.GenDecl:
			for _, specification := range typed.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				doc := typeSpec.Doc
				if doc == nil {
					doc = typed.Doc
				}
				if hasAtomMarker(doc) {
					consumed[doc] = true
					if err := validateAtomDeclaration(typeSpec.Name.Name, doc); err != nil {
						return fmt.Errorf("%s:%d: %w", path, fileSet.Position(typeSpec.Pos()).Line, err)
					}
				}
			}
		}
	}
	for _, group := range file.Comments {
		if hasAtomMarker(group) && !consumed[group] {
			return fmt.Errorf("%s:%d: %s directive is not attached to a supported declaration", path, fileSet.Position(group.Pos()).Line, atomMarker)
		}
	}
	return nil
}

func validateAtomDeclaration(identifier string, doc *ast.CommentGroup) error {
	lines := commentGroupLines(doc)
	opening := openingAtomSentence(lines)
	if !strings.HasPrefix(opening, identifier+" ") || !strings.ContainsAny(opening[len(identifier):], ".!?") {
		return fmt.Errorf("atom comment must begin with %s and a complete descriptive sentence, found %q", identifier, opening)
	}

	headerIndex := indexOfLine(lines, atomDocumentationHeader)
	if headerIndex < 0 {
		for _, line := range lines {
			if strings.Contains(line, "Codeflux atom documentation") {
				return fmt.Errorf("atom %s has malformed documentation schema header", identifier)
			}
		}
		return fmt.Errorf("atom %s lacks schema-versioned Codeflux atom documentation", identifier)
	}
	fields, err := parseAtomDocumentationFields(lines[headerIndex+1:])
	if err != nil {
		return fmt.Errorf("atom %s: %w", identifier, err)
	}
	for _, field := range atomDocumentationFields {
		content, ok := fields[field]
		if !ok {
			return fmt.Errorf("atom %s is missing required field %q", identifier, field)
		}
		if err := validateAtomFieldContent(field, content); err != nil {
			return fmt.Errorf("atom %s: %w", identifier, err)
		}
	}
	exceptions, err := parseAtomNameExceptions(lines)
	if err != nil {
		return fmt.Errorf("atom %s: %w", identifier, err)
	}
	if _, err := deriveAtomNameRecord("lint:"+identifier, identifier, exceptions); err != nil {
		return fmt.Errorf("atom %s name: %w", identifier, err)
	}
	return nil
}

func commentGroupLines(group *ast.CommentGroup) []string {
	if group == nil {
		return nil
	}
	var lines []string
	for _, comment := range group.List {
		text := comment.Text
		switch {
		case strings.HasPrefix(text, "//"):
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(text, "//")))
		case strings.HasPrefix(text, "/*"):
			text = strings.TrimSuffix(strings.TrimPrefix(text, "/*"), "*/")
			for _, line := range strings.Split(text, "\n") {
				lines = append(lines, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*")))
			}
		}
	}
	return lines
}

// openingAtomSentence returns the atom comment's opening sentence, joining the
// wrapped lines that carry it. A doc comment is wrapped to a width, so an
// opening sentence naming a long atom identifier and then describing it does not
// fit on one physical line; reading only the first line would reject correct Go
// style. Collection stops at the first sentence terminator, and also at a blank
// line, the schema header, the atom marker, or a name exception, so a comment
// whose opening sentence is genuinely missing yields the truncated opening text
// rather than the whole comment.
func openingAtomSentence(lines []string) string {
	var collected []string
	for _, line := range lines {
		boundary := line == "" || line == atomMarker ||
			strings.HasPrefix(line, "codeflux:atom-name-exception ") ||
			strings.Contains(line, "Codeflux atom documentation")
		if boundary {
			if len(collected) > 0 {
				break
			}
			continue
		}
		collected = append(collected, line)
		if strings.ContainsAny(line, ".!?") {
			break
		}
	}
	return strings.Join(collected, " ")
}

func hasAtomMarker(group *ast.CommentGroup) bool {
	return indexOfLine(commentGroupLines(group), atomMarker) >= 0
}

func indexOfLine(lines []string, target string) int {
	for index, line := range lines {
		if line == target {
			return index
		}
	}
	return -1
}

func parseAtomDocumentationFields(lines []string) (map[string]string, error) {
	required := make(map[string]bool, len(atomDocumentationFields))
	for _, field := range atomDocumentationFields {
		required[field] = true
	}
	fields := make(map[string]string, len(atomDocumentationFields))
	current := ""
	var content []string
	flush := func() error {
		if current == "" {
			return nil
		}
		if _, exists := fields[current]; exists {
			return fmt.Errorf("required field %q is duplicated", current)
		}
		fields[current] = strings.TrimSpace(strings.Join(content, " "))
		return nil
	}
	for _, line := range lines {
		candidate := strings.TrimSuffix(line, ":")
		if strings.HasSuffix(line, ":") && required[candidate] {
			if err := flush(); err != nil {
				return nil, err
			}
			current = candidate
			content = nil
			continue
		}
		if current != "" && line != "" && !strings.HasPrefix(line, "codeflux:atom-name-exception ") {
			content = append(content, strings.TrimSpace(strings.TrimPrefix(line, "-")))
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return fields, nil
}

func validateAtomFieldContent(field, content string) error {
	if content == "" {
		return fmt.Errorf("required field %q is empty", field)
	}
	if strings.HasPrefix(content, "None") {
		if !strings.HasPrefix(content, "None: ") || len(atomWordPattern.FindAllString(strings.TrimPrefix(content, "None: "), -1)) < 2 {
			return fmt.Errorf("required field %q uses None without a substantive reason", field)
		}
		return nil
	}
	words := atomWordPattern.FindAllString(strings.ToLower(content), -1)
	if len(words) < 3 {
		return fmt.Errorf("required field %q is not substantive", field)
	}
	counts := make(map[string]int)
	maxCount := 0
	for _, word := range words {
		counts[word]++
		if counts[word] > maxCount {
			maxCount = counts[word]
		}
	}
	if len(words) >= 6 && maxCount*2 > len(words) {
		return fmt.Errorf("required field %q appears keyword-stuffed", field)
	}
	return nil
}

func parseAtomNameExceptions(lines []string) (atomNameExceptions, error) {
	exceptions := atomNameExceptions{abbreviations: make(map[string]string)}
	const prefix = "codeflux:atom-name-exception "
	for _, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		kind, reason, ok := strings.Cut(strings.TrimPrefix(line, prefix), ":")
		reason = strings.TrimSpace(reason)
		if !ok || reason == "" {
			return atomNameExceptions{}, fmt.Errorf("atom name exception requires a non-empty reason")
		}
		switch {
		case kind == "action-verb":
			exceptions.actionVerb = reason
		case kind == "provider-specific":
			exceptions.providerSpecific = reason
		case kind == "guarantee-claim":
			exceptions.guaranteeClaim = reason
		case strings.HasPrefix(kind, "abbreviation "):
			token := strings.TrimSpace(strings.TrimPrefix(kind, "abbreviation "))
			if token == "" {
				return atomNameExceptions{}, fmt.Errorf("abbreviation exception requires a token")
			}
			exceptions.abbreviations[token] = reason
		default:
			return atomNameExceptions{}, fmt.Errorf("unknown atom name exception kind %q", kind)
		}
	}
	return exceptions, nil
}

func deriveAtomNameRecord(identity, canonical string, exceptions atomNameExceptions) (atomNameRecord, error) {
	if identity == "" {
		return atomNameRecord{}, fmt.Errorf("atom identity is empty")
	}
	words, err := validateAtomName(canonical, exceptions)
	if err != nil {
		return atomNameRecord{}, err
	}
	display := strings.Join(words, " ")
	return atomNameRecord{
		AtomIdentity:     identity,
		CanonicalGoName:  canonical,
		DisplayName:      display,
		NormalizedPhrase: strings.ToLower(display),
	}, nil
}

func validateAtomName(name string, exceptions atomNameExceptions) ([]string, error) {
	if name == "" {
		return nil, fmt.Errorf("canonical name is empty")
	}
	words := splitGoIdentifier(name)
	if len(words) == 0 {
		return nil, fmt.Errorf("canonical name has no semantic words")
	}
	if len(words) == 1 && genericAtomNames[words[0]] {
		return nil, fmt.Errorf("single generic word %q is not descriptive", words[0])
	}
	if fillerAtomSuffixes[words[len(words)-1]] {
		return nil, fmt.Errorf("filler suffix %q is forbidden", words[len(words)-1])
	}
	if atomVersionPattern.MatchString(name) {
		return nil, fmt.Errorf("version encoding is forbidden")
	}
	if atomHashPattern.MatchString(name) {
		return nil, fmt.Errorf("hash encoding is forbidden")
	}
	if !concreteActionVerbs[words[0]] && exceptions.actionVerb == "" {
		return nil, fmt.Errorf("first word %q is not a recognized concrete action verb", words[0])
	}
	for _, word := range words {
		if len(word) > 1 && word == strings.ToUpper(word) && !establishedAbbreviations[word] {
			if exceptions.abbreviations[word] == "" {
				return nil, fmt.Errorf("unexplained abbreviation %q", word)
			}
		}
		if misleadingGuaranteeWords[word] && exceptions.guaranteeClaim == "" {
			return nil, fmt.Errorf("unreviewed guarantee claim %q", word)
		}
	}
	lower := strings.ToLower(name)
	for _, provider := range providerNameFragments {
		if strings.Contains(lower, provider) && exceptions.providerSpecific == "" {
			return nil, fmt.Errorf("provider-specific name %q requires a reviewed exception", name)
		}
	}
	return words, nil
}

func splitGoIdentifier(identifier string) []string {
	runes := []rune(identifier)
	var words []string
	start := 0
	for index := 1; index < len(runes); index++ {
		previous := runes[index-1]
		current := runes[index]
		var next rune
		if index+1 < len(runes) {
			next = runes[index+1]
		}
		boundary := unicode.IsLower(previous) && unicode.IsUpper(current) ||
			unicode.IsDigit(previous) != unicode.IsDigit(current) ||
			unicode.IsUpper(previous) && unicode.IsUpper(current) && next != 0 && unicode.IsLower(next)
		if boundary {
			words = append(words, string(runes[start:index]))
			start = index
		}
	}
	if start < len(runes) {
		words = append(words, string(runes[start:]))
	}
	return words
}

func classifyAtomRename(previous, next atomNameRecord, semanticsReviewedPreserved bool) atomRenameClassification {
	if previous.AtomIdentity != next.AtomIdentity {
		return atomRenameSemanticBreaking
	}
	if previous.CanonicalGoName == next.CanonicalGoName {
		return atomRenameUnchanged
	}
	if semanticsReviewedPreserved {
		return atomRenameSemanticPreserving
	}
	return atomRenameSemanticBreaking
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
