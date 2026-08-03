package textutil

import "errors"

// errInvalidDigit is returned by parseConfigInt when a character is not a
// decimal digit.
var errInvalidDigit = errors.New("invalid digit")

// Sum adds every value in values.
func Sum(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

// Max returns the largest value in values, and false when values is empty.
func Max(values []int) (int, bool) {
	if len(values) == 0 {
		return 0, false
	}
	largest := values[0]
	for _, value := range values[1:] {
		if value > largest {
			largest = value
		}
	}
	return largest, true
}

// Mean returns the arithmetic mean of values, and false when values is empty.
func Mean(values []int) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	return float64(Sum(values)) / float64(len(values)), true
}

// Clamp restricts value to the closed range [low, high].
func Clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// LoadConfigValue is pre-existing, untouched, mildly careless code: it
// discards a parse failure with the blank identifier rather than reporting
// it. It stays in the fixture deliberately so the PIPE-116 test can prove a
// pre-existing anti-pattern in a declaration nobody touches is recorded as
// context, not a gate failure (PIPE-113).
func LoadConfigValue(text string) int {
	value, _ := parseConfigInt(text)
	return value
}

// parseConfigInt parses a non-negative decimal integer. It is deliberately
// left without its own test or doc comment in the fixture, so the PIPE-116
// test can prove a pre-existing, untouched gap is not sent back to a future
// attempt (PIPE-112) even though it is a real gap.
func parseConfigInt(text string) (int, error) {
	total := 0
	for _, character := range text {
		if character < '0' || character > '9' {
			return 0, errInvalidDigit
		}
		total = total*10 + int(character-'0')
	}
	return total, nil
}
