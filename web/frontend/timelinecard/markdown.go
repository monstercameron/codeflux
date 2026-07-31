package timelinecard

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

type BlockKind string

const (
	BlockParagraph BlockKind = "paragraph"
	BlockCode      BlockKind = "code"
)

type InlineKind string

const (
	InlineText InlineKind = "text"
	InlineCode InlineKind = "code"
	InlineLink InlineKind = "link"
)

type Inline struct {
	Kind InlineKind
	Text string
	Link *SafeLink
}

type CodeOverflow string

const (
	CodeHorizontalScroll CodeOverflow = "horizontal-scroll"
	CodeWrap             CodeOverflow = "wrap"
)

type Block struct {
	Kind     BlockKind
	Inlines  []Inline
	Code     string
	Language string
	CopyText string
	Overflow CodeOverflow
}

type Markdown struct {
	Blocks       []Block
	BlockedLinks int
}

type SafeLink struct {
	URL      string
	External bool
	Scheme   string
}

// SanitizeLink permits only explicit web and mail schemes, rejects credentials,
// control characters, protocol-relative targets, and obfuscated schemeless URLs.
func SanitizeLink(raw string) (SafeLink, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "//") ||
		strings.IndexFunc(trimmed, unicode.IsControl) >= 0 {
		return SafeLink{}, fmt.Errorf("unsafe link target")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.User != nil {
		return SafeLink{}, fmt.Errorf("unsafe link target")
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "https", "http":
		if parsed.Host == "" {
			return SafeLink{}, fmt.Errorf("web link host is required")
		}
	case "mailto":
		if parsed.Opaque == "" && parsed.Path == "" {
			return SafeLink{}, fmt.Errorf("mail link address is required")
		}
	default:
		return SafeLink{}, fmt.Errorf("unsafe link scheme %q", scheme)
	}
	return SafeLink{URL: parsed.String(), External: true, Scheme: scheme}, nil
}

// ParseMarkdown builds a small safe presentation tree. Raw HTML has no node
// type and therefore remains inert text for the GWC renderer.
func ParseMarkdown(source string) (Markdown, error) {
	if !utf8.ValidString(source) {
		return Markdown{}, fmt.Errorf("markdown must be valid UTF-8")
	}
	const maximumMarkdownBytes = 256 * 1024
	if len(source) > maximumMarkdownBytes {
		return Markdown{}, fmt.Errorf("markdown exceeds %d bytes", maximumMarkdownBytes)
	}
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	document := Markdown{}
	paragraph := make([]string, 0)
	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		inlines, blocked := parseInlines(strings.Join(paragraph, "\n"))
		document.Blocks = append(document.Blocks, Block{Kind: BlockParagraph, Inlines: inlines})
		document.BlockedLinks += blocked
		paragraph = paragraph[:0]
	}
	for index := 0; index < len(lines); {
		line := lines[index]
		if strings.HasPrefix(line, "```") {
			flushParagraph()
			language := strings.TrimSpace(strings.TrimPrefix(line, "```"))
			index++
			code := make([]string, 0)
			for index < len(lines) && !strings.HasPrefix(lines[index], "```") {
				code = append(code, lines[index])
				index++
			}
			if index < len(lines) {
				index++
			}
			text := strings.Join(code, "\n")
			document.Blocks = append(document.Blocks, Block{
				Kind: BlockCode, Code: text, Language: language,
				CopyText: text, Overflow: CodeHorizontalScroll,
			})
			continue
		}
		if strings.TrimSpace(line) == "" {
			flushParagraph()
			index++
			continue
		}
		paragraph = append(paragraph, line)
		index++
	}
	flushParagraph()
	return document, nil
}

func parseInlines(source string) ([]Inline, int) {
	result := make([]Inline, 0)
	blocked := 0
	for len(source) > 0 {
		linkStart := strings.IndexByte(source, '[')
		codeStart := strings.IndexByte(source, '`')
		start := firstNonNegative(linkStart, codeStart)
		if start < 0 {
			result = appendText(result, source)
			break
		}
		result = appendText(result, source[:start])
		source = source[start:]
		if source[0] == '`' {
			end := strings.IndexByte(source[1:], '`')
			if end < 0 {
				result = appendText(result, source)
				break
			}
			result = append(result, Inline{Kind: InlineCode, Text: source[1 : end+1]})
			source = source[end+2:]
			continue
		}
		labelEnd := strings.Index(source, "](")
		if labelEnd <= 0 {
			result = appendText(result, source[:1])
			source = source[1:]
			continue
		}
		targetEnd := strings.IndexByte(source[labelEnd+2:], ')')
		if targetEnd < 0 {
			result = appendText(result, source[:1])
			source = source[1:]
			continue
		}
		label := source[1:labelEnd]
		rawTarget := source[labelEnd+2 : labelEnd+2+targetEnd]
		link, err := SanitizeLink(rawTarget)
		if err != nil {
			blocked++
			result = appendText(result, label)
		} else {
			result = append(result, Inline{Kind: InlineLink, Text: label, Link: &link})
		}
		source = source[labelEnd+3+targetEnd:]
	}
	return result, blocked
}

func appendText(inlines []Inline, text string) []Inline {
	if text == "" {
		return inlines
	}
	if len(inlines) > 0 && inlines[len(inlines)-1].Kind == InlineText {
		inlines[len(inlines)-1].Text += text
		return inlines
	}
	return append(inlines, Inline{Kind: InlineText, Text: text})
}

func firstNonNegative(values ...int) int {
	result := -1
	for _, value := range values {
		if value >= 0 && (result < 0 || value < result) {
			result = value
		}
	}
	return result
}
