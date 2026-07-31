package design

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

const (
	// MinimumNormalTextContrast is the WCAG AA threshold for normal text.
	MinimumNormalTextContrast = 4.5
	// MinimumLargeTextContrast is the WCAG AA threshold for large text and
	// non-text interface boundaries.
	MinimumLargeTextContrast = 3.0
)

// RGB is one normalized sRGB color.
type RGB struct {
	Red   uint8
	Green uint8
	Blue  uint8
}

// ContrastPair identifies one fixed semantic foreground/background use.
type ContrastPair struct {
	Name       string
	Foreground Color
	Background Color
	Minimum    float64
}

// ContrastFailure reports one fixed pair below its required threshold.
type ContrastFailure struct {
	Pair  ContrastPair
	Ratio float64
}

// ParseColor accepts only a full six-digit sRGB hexadecimal color.
func ParseColor(value string) (RGB, error) {
	if len(value) != 7 || value[0] != '#' {
		return RGB{}, errors.New("color must use #rrggbb notation")
	}
	components := [3]uint8{}
	for index := range components {
		parsed, err := strconv.ParseUint(
			value[1+index*2:3+index*2],
			16,
			8,
		)
		if err != nil {
			return RGB{}, errors.New("color must use hexadecimal digits")
		}
		components[index] = uint8(parsed)
	}
	return RGB{
		Red: components[0], Green: components[1], Blue: components[2],
	}, nil
}

// ContrastRatio calculates WCAG relative-luminance contrast.
func ContrastRatio(foreground, background Color) (float64, error) {
	left, err := ParseColor(string(foreground))
	if err != nil {
		return 0, err
	}
	right, err := ParseColor(string(background))
	if err != nil {
		return 0, err
	}
	leftLuminance := relativeLuminance(left)
	rightLuminance := relativeLuminance(right)
	lighter := math.Max(leftLuminance, rightLuminance)
	darker := math.Min(leftLuminance, rightLuminance)
	return (lighter + 0.05) / (darker + 0.05), nil
}

// FixedContrastPairs returns the text/background pairs components are allowed
// to rely on without runtime color composition.
func FixedContrastPairs(tokens Tokens) []ContrastPair {
	colors := tokens.Colors
	return []ContrastPair{
		{Name: "primary-on-canvas", Foreground: colors.TextPrimary, Background: colors.Canvas, Minimum: MinimumNormalTextContrast},
		{Name: "primary-on-surface-1", Foreground: colors.TextPrimary, Background: colors.Surface1, Minimum: MinimumNormalTextContrast},
		{Name: "primary-on-surface-2", Foreground: colors.TextPrimary, Background: colors.Surface2, Minimum: MinimumNormalTextContrast},
		{Name: "primary-on-surface-3", Foreground: colors.TextPrimary, Background: colors.Surface3, Minimum: MinimumNormalTextContrast},
		{Name: "primary-on-raised", Foreground: colors.TextPrimary, Background: colors.SurfaceRaised, Minimum: MinimumNormalTextContrast},
		{Name: "primary-on-inset", Foreground: colors.TextPrimary, Background: colors.SurfaceInset, Minimum: MinimumNormalTextContrast},
		{Name: "secondary-on-surface-1", Foreground: colors.TextSecondary, Background: colors.Surface1, Minimum: MinimumNormalTextContrast},
		{Name: "secondary-on-surface-2", Foreground: colors.TextSecondary, Background: colors.Surface2, Minimum: MinimumNormalTextContrast},
		{Name: "muted-on-surface-1", Foreground: colors.TextMuted, Background: colors.Surface1, Minimum: MinimumNormalTextContrast},
		{Name: "muted-on-surface-2", Foreground: colors.TextMuted, Background: colors.Surface2, Minimum: MinimumNormalTextContrast},
		{Name: "link-on-surface-1", Foreground: colors.Link, Background: colors.Surface1, Minimum: MinimumNormalTextContrast},
		{Name: "link-on-surface-2", Foreground: colors.Link, Background: colors.Surface2, Minimum: MinimumNormalTextContrast},
		{Name: "accent-action", Foreground: colors.OnAccent, Background: colors.Accent, Minimum: MinimumNormalTextContrast},
		{Name: "accent-hover-action", Foreground: colors.OnAccent, Background: colors.AccentHover, Minimum: MinimumNormalTextContrast},
		{Name: "accent-pressed-action", Foreground: colors.OnAccent, Background: colors.AccentPressed, Minimum: MinimumNormalTextContrast},
		{Name: "selection", Foreground: colors.OnSelection, Background: colors.Selection, Minimum: MinimumNormalTextContrast},
		{Name: "success-status", Foreground: colors.OnSuccess, Background: colors.Success, Minimum: MinimumNormalTextContrast},
		{Name: "warning-status", Foreground: colors.OnWarning, Background: colors.Warning, Minimum: MinimumNormalTextContrast},
		{Name: "failure-status", Foreground: colors.OnFailure, Background: colors.Failure, Minimum: MinimumNormalTextContrast},
		{Name: "active-status", Foreground: colors.OnActive, Background: colors.Active, Minimum: MinimumNormalTextContrast},
		{Name: "blocked-status", Foreground: colors.OnBlocked, Background: colors.Blocked, Minimum: MinimumNormalTextContrast},
		{Name: "invalidated-status", Foreground: colors.OnInvalidated, Background: colors.Invalidated, Minimum: MinimumNormalTextContrast},
		{Name: "pending-status", Foreground: colors.OnPending, Background: colors.Pending, Minimum: MinimumNormalTextContrast},
		{Name: "plan-status", Foreground: colors.OnPlan, Background: colors.Plan, Minimum: MinimumNormalTextContrast},
		{Name: "evidence-status", Foreground: colors.OnEvidence, Background: colors.Evidence, Minimum: MinimumNormalTextContrast},
		{Name: "accent-signal-on-surface-2", Foreground: colors.Accent, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "success-signal-on-surface-2", Foreground: colors.Success, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "warning-signal-on-surface-2", Foreground: colors.Warning, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "failure-signal-on-surface-2", Foreground: colors.Failure, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "active-signal-on-surface-2", Foreground: colors.Active, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "blocked-signal-on-surface-2", Foreground: colors.Blocked, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "invalidated-signal-on-surface-2", Foreground: colors.Invalidated, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "pending-signal-on-surface-2", Foreground: colors.Pending, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "plan-signal-on-surface-2", Foreground: colors.Plan, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "evidence-signal-on-surface-2", Foreground: colors.Evidence, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "accent-signal-on-surface-3", Foreground: colors.Accent, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "success-signal-on-surface-3", Foreground: colors.Success, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "warning-signal-on-surface-3", Foreground: colors.Warning, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "failure-signal-on-surface-3", Foreground: colors.Failure, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "active-signal-on-surface-3", Foreground: colors.Active, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "blocked-signal-on-surface-3", Foreground: colors.Blocked, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "invalidated-signal-on-surface-3", Foreground: colors.Invalidated, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "pending-signal-on-surface-3", Foreground: colors.Pending, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "plan-signal-on-surface-3", Foreground: colors.Plan, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "evidence-signal-on-surface-3", Foreground: colors.Evidence, Background: colors.Surface3, Minimum: MinimumLargeTextContrast},
		{Name: "focus-on-canvas", Foreground: colors.FocusRing, Background: colors.Canvas, Minimum: MinimumLargeTextContrast},
		{Name: "focus-on-surface-1", Foreground: colors.FocusRing, Background: colors.Surface1, Minimum: MinimumLargeTextContrast},
		{Name: "focus-on-surface-2", Foreground: colors.FocusRing, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
		{Name: "focus-on-raised", Foreground: colors.FocusRing, Background: colors.SurfaceRaised, Minimum: MinimumLargeTextContrast},
		{Name: "strong-border-on-surface-1", Foreground: colors.BorderStrong, Background: colors.Surface1, Minimum: MinimumLargeTextContrast},
		{Name: "strong-border-on-surface-2", Foreground: colors.BorderStrong, Background: colors.Surface2, Minimum: MinimumLargeTextContrast},
	}
}

// VerifyFixedContrastPairs returns every pair that fails WCAG AA.
func VerifyFixedContrastPairs(tokens Tokens) ([]ContrastFailure, error) {
	pairs := FixedContrastPairs(tokens)
	failures := make([]ContrastFailure, 0)
	for _, pair := range pairs {
		ratio, err := ContrastRatio(pair.Foreground, pair.Background)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pair.Name, err)
		}
		if ratio+0.000001 < pair.Minimum {
			failures = append(failures, ContrastFailure{
				Pair: pair, Ratio: ratio,
			})
		}
	}
	return failures, nil
}

func relativeLuminance(color RGB) float64 {
	channel := func(value uint8) float64 {
		normalized := float64(value) / 255
		if normalized <= 0.04045 {
			return normalized / 12.92
		}
		return math.Pow((normalized+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(color.Red) +
		0.7152*channel(color.Green) +
		0.0722*channel(color.Blue)
}
