package design

// SyntaxColors is the palette a source viewer draws code with.
//
// The console's rule is that saturated color means a machine state, which is
// why the shell itself is graphite and paper. That rule is about the console:
// a colored thing among neutral things is a signal. Inside a source listing
// there is nothing but code, so the signal a reader needs is not "this is
// state" but "this is a comment and that is a string" — a different question,
// answered on a surface where every pixel is already the same kind of thing.
//
// The palette stays disciplined anyway. It is four hues, all desaturated
// against the console's state colors, and it deliberately avoids the live
// cyan that Active owns, so a running stream is still the only cyan on screen.
type SyntaxColors struct {
	// Comment is prose the compiler ignores, dimmed so code reads first.
	Comment Color
	// Keyword is the language's own vocabulary.
	Keyword Color
	// Literal is a string, rune, or number written into the source.
	Literal Color
	// Builtin is a predeclared name: the universe block's types and functions.
	Builtin Color
	// Declared is a name this file gives a function or method at its
	// declaration, which is what a reader scanning for structure looks for.
	Declared Color
	// Function is a name being called. It shares the declaration's hue on
	// purpose: a reader following a call to its definition is following one
	// thing, and one colour says so.
	Function Color
	// Punctuation is every brace, bracket, comma, and operator. It is the
	// dimmest thing on the surface because structure should be legible without
	// competing with the names it holds -- and because in Go it is a third of
	// the tokens on screen.
	Punctuation Color
	// Plain is every other identifier and all punctuation.
	Plain Color
}

// SyntaxColorsFor returns the source palette for a theme.
func SyntaxColorsFor(tokens Tokens) SyntaxColors {
	switch tokens.Theme {
	case ThemeLight:
		return SyntaxColors{
			Comment:     "#556372",
			Keyword:     "#8a4a86",
			Literal:     "#1f6b45",
			Builtin:     "#1a5c94",
			Declared:    "#8a5a1c",
			Function:    "#8a5a1c",
			Punctuation: "#5c6675",
			Plain:       tokens.Colors.TextPrimary,
		}
	case ThemeHighContrast:
		// High contrast keeps hue differences that survive at AAA rather than
		// pretty ones, because the reader who chose this theme is reading the
		// shapes, not admiring the palette.
		return SyntaxColors{
			Comment:     "#c7c7c7",
			Keyword:     "#ffc6f0",
			Literal:     "#b6ffcc",
			Builtin:     "#bfe4ff",
			Declared:    "#ffd9a0",
			Function:    "#ffd9a0",
			Punctuation: "#d8d8d8",
			Plain:       tokens.Colors.TextPrimary,
		}
	default:
		return SyntaxColors{
			Comment:     "#8b95a6",
			Keyword:     "#d6a8d0",
			Literal:     "#9ad5b0",
			Builtin:     "#9dc0e8",
			Declared:    "#e8bd7a",
			Function:    "#e8bd7a",
			Punctuation: "#8996a8",
			Plain:       tokens.Colors.TextPrimary,
		}
	}
}

// SyntaxContrastPairs is every source color against the surface it is drawn
// on, so the contrast check covers the code viewer the way it covers the rest
// of the console.
func SyntaxContrastPairs(tokens Tokens) []ContrastPair {
	colors := SyntaxColorsFor(tokens)
	var pairs []ContrastPair
	// Both surfaces, because the line a person jumped to is drawn on the
	// raised one and a comment on that line must stay as readable as the rest.
	for _, surface := range []struct {
		Name  string
		Color Color
	}{
		{Name: "surface-1", Color: tokens.Colors.Surface1},
		{Name: "surface-2", Color: tokens.Colors.Surface2},
	} {
		for _, entry := range []struct {
			Name  string
			Color Color
		}{
			{Name: "comment", Color: colors.Comment},
			{Name: "keyword", Color: colors.Keyword},
			{Name: "literal", Color: colors.Literal},
			{Name: "builtin", Color: colors.Builtin},
			{Name: "declared", Color: colors.Declared},
			{Name: "function", Color: colors.Function},
			{Name: "punctuation", Color: colors.Punctuation},
			{Name: "plain", Color: colors.Plain},
		} {
			pairs = append(pairs, ContrastPair{
				Name:       "syntax " + entry.Name + " on " + surface.Name,
				Foreground: entry.Color, Background: surface.Color,
				Minimum: MinimumNormalTextContrast,
			})
		}
	}
	return pairs
}
