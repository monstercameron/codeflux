package timelinecard

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type OutputPage struct {
	Number int
	Text   string
}

// RedactedOutput stores only text already passed through the shared redaction
// boundary. Pages are bounded, collapsed by default, and revealed lazily.
type RedactedOutput struct {
	Summary     string
	Collapsed   bool
	Truncated   bool
	TotalBytes  int
	PageCount   int
	LoadedPages int
	pages       []OutputPage
}

func NewRedactedOutput(summary, redactedText string, pageBytes, maximumBytes int) (RedactedOutput, error) {
	if pageBytes <= 0 || maximumBytes <= 0 || pageBytes > maximumBytes {
		return RedactedOutput{}, fmt.Errorf("redacted output limits are invalid")
	}
	if !utf8.ValidString(redactedText) {
		return RedactedOutput{}, fmt.Errorf("redacted output must be valid UTF-8")
	}
	bounded := redactedText
	truncated := false
	if len(bounded) > maximumBytes {
		bounded = validPrefix(bounded, maximumBytes)
		truncated = true
	}
	pages := make([]OutputPage, 0, (len(bounded)+pageBytes-1)/pageBytes)
	for len(bounded) > 0 {
		length := pageBytes
		if length > len(bounded) {
			length = len(bounded)
		}
		chunk := validPrefix(bounded, length)
		if chunk == "" {
			_, size := utf8.DecodeRuneInString(bounded)
			chunk = bounded[:size]
		}
		pages = append(pages, OutputPage{Number: len(pages) + 1, Text: chunk})
		bounded = bounded[len(chunk):]
	}
	return RedactedOutput{
		Summary: strings.TrimSpace(summary), Collapsed: true, Truncated: truncated,
		TotalBytes: len(redactedText), PageCount: len(pages), pages: pages,
	}, nil
}

func (output RedactedOutput) Expand() RedactedOutput {
	output.Collapsed = false
	if output.LoadedPages == 0 && output.PageCount > 0 {
		output.LoadedPages = 1
	}
	return output
}

func (output RedactedOutput) Collapse() RedactedOutput {
	output.Collapsed = true
	return output
}

func (output RedactedOutput) LoadNext() RedactedOutput {
	if output.Collapsed {
		return output
	}
	if output.LoadedPages < output.PageCount {
		output.LoadedPages++
	}
	return output
}

func (output RedactedOutput) VisiblePages() []OutputPage {
	if output.Collapsed || output.LoadedPages <= 0 {
		return nil
	}
	limit := output.LoadedPages
	if limit > len(output.pages) {
		limit = len(output.pages)
	}
	result := make([]OutputPage, limit)
	copy(result, output.pages[:limit])
	return result
}

// CopyText returns only pages the user deliberately disclosed.
func (output RedactedOutput) CopyText() string {
	visible := output.VisiblePages()
	var builder strings.Builder
	for _, page := range visible {
		builder.WriteString(page.Text)
	}
	return builder.String()
}

func validPrefix(value string, maximum int) string {
	if maximum >= len(value) {
		return value
	}
	for maximum > 0 && !utf8.ValidString(value[:maximum]) {
		maximum--
	}
	return value[:maximum]
}
