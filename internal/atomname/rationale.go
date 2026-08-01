package atomname

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalidNamingRationale classifies a rejected naming rationale.
var ErrInvalidNamingRationale = errors.New("invalid atom naming rationale")

var rationaleWordPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]*`)

// NamingRationale is the short, required explanation naming the nearest
// confusing alternative atom and the qualifier that distinguishes this
// name from it (M21-152). Both fields must be substantive; a rationale
// cannot be constructed with an empty or trivial field.
type NamingRationale struct {
	nearestConfusingAlternative string
	distinguishingQualifier     string
}

// NewNamingRationale validates and constructs a NamingRationale.
func NewNamingRationale(nearestConfusingAlternative, distinguishingQualifier string) (NamingRationale, error) {
	nearest := strings.TrimSpace(nearestConfusingAlternative)
	qualifier := strings.TrimSpace(distinguishingQualifier)
	if err := validateRationaleField("nearest confusing alternative", nearest); err != nil {
		return NamingRationale{}, err
	}
	if err := validateRationaleField("distinguishing qualifier", qualifier); err != nil {
		return NamingRationale{}, err
	}
	return NamingRationale{nearestConfusingAlternative: nearest, distinguishingQualifier: qualifier}, nil
}

func validateRationaleField(label, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s must not be empty", ErrInvalidNamingRationale, label)
	}
	if len(value) > MaximumRationaleFieldBytes {
		return fmt.Errorf("%w: %s exceeds the maximum bound of %d bytes", ErrInvalidNamingRationale, label, MaximumRationaleFieldBytes)
	}
	if len(rationaleWordPattern.FindAllString(value, -1)) < 2 {
		return fmt.Errorf("%w: %s is not substantive", ErrInvalidNamingRationale, label)
	}
	return nil
}

// NearestConfusingAlternative names the atom or name this canonical name
// could realistically be confused with.
func (rationale NamingRationale) NearestConfusingAlternative() string {
	return rationale.nearestConfusingAlternative
}

// DistinguishingQualifier states what distinguishes this atom from the
// nearest confusing alternative.
func (rationale NamingRationale) DistinguishingQualifier() string {
	return rationale.distinguishingQualifier
}

// IsZero reports whether no rationale has been assigned.
func (rationale NamingRationale) IsZero() bool {
	return rationale.nearestConfusingAlternative == "" && rationale.distinguishingQualifier == ""
}
