// Package textutil is the PIPE-116 fixture's pre-existing library. Every
// function in this file, and in numeric.go beside it, exists before the
// simulated run starts; the test that loads this fixture commits it as the
// base revision and then makes exactly one further, one-line change,
// specifically so a check re-scoped by PIPE-111/PIPE-111a has substantial
// pre-existing code to be correctly indifferent to.
package textutil

import "strings"

// Reverse returns its argument with the characters in reverse order.
func Reverse(value string) string {
	runes := []rune(value)
	for left, right := 0, len(runes)-1; left < right; left, right = left+1, right-1 {
		runes[left], runes[right] = runes[right], runes[left]
	}
	return string(runes)
}

// CountWords reports how many whitespace-separated words value holds.
func CountWords(value string) int {
	return len(strings.Fields(value))
}

// Title uppercases the first letter of every word in value.
func Title(value string) string {
	words := strings.Fields(value)
	for index, word := range words {
		if word == "" {
			continue
		}
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// TrimAll removes every leading and trailing occurrence of cut from value.
func TrimAll(value, cut string) string {
	return strings.Trim(value, cut)
}

// IsPalindrome reports whether value reads the same forwards and backwards,
// ignoring case.
func IsPalindrome(value string) bool {
	lowered := strings.ToLower(value)
	return lowered == Reverse(lowered)
}

// Truncate cuts value to at most limit runes, appending an ellipsis when it
// does.
func Truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
