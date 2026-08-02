package appearanceview_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/appearanceview"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func mode(t *testing.T) primitives.Mode {
	t.Helper()
	return primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable}
}

// TestAppearanceMarksTheChoicesInForce checks each control reports which
// option is active: a settings page whose controls do not show their own state
// is a page you have to change something on to find out what it is set to.
func TestAppearanceMarksTheChoicesInForce(t *testing.T) {
	markup := render(t, ui.CreateElement(appearanceview.Component, appearanceview.Props{
		Mode: mode(t), Theme: design.ThemeHighContrast, Density: design.DensityCompact,
		MotionFollowsSystem: false, ReduceMotion: true,
		OnTheme: func(design.Theme) {}, OnDensity: func(design.Density) {},
		OnMotionFollow: func() {}, OnMotionReduce: func(bool) {},
	}))
	for _, want := range []string{
		`data-component="appearance-settings"`,
		`data-setting="theme"`,
		`data-setting="density"`,
		`data-setting="motion"`,
		"High contrast",
		"Compact",
		"Reduce motion",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("appearance markup missing %q", want)
		}
	}
	if pressed := strings.Count(markup, `aria-pressed="true"`); pressed != 3 {
		t.Errorf("pressed controls = %d, want exactly one per setting: %s", pressed, markup)
	}
}

// TestAppearanceSaysWhatFollowingTheSystemMeans keeps "Follow the system" from
// being a control whose effect nobody can see.
func TestAppearanceSaysWhatFollowingTheSystemMeans(t *testing.T) {
	asking := render(t, ui.CreateElement(appearanceview.Component, appearanceview.Props{
		Mode: mode(t), Theme: design.ThemeDark, Density: design.DensityComfortable,
		MotionFollowsSystem: true, SystemReducesMotion: true, ReduceMotion: true,
		OnTheme: func(design.Theme) {}, OnDensity: func(design.Density) {},
		OnMotionFollow: func() {}, OnMotionReduce: func(bool) {},
	}))
	if !strings.Contains(asking, "This system asks for reduced motion.") {
		t.Errorf("motion hint missing: %s", asking)
	}
	quiet := render(t, ui.CreateElement(appearanceview.Component, appearanceview.Props{
		Mode: mode(t), Theme: design.ThemeDark, Density: design.DensityComfortable,
		MotionFollowsSystem: true,
		OnTheme:             func(design.Theme) {}, OnDensity: func(design.Density) {},
		OnMotionFollow: func() {}, OnMotionReduce: func(bool) {},
	}))
	if !strings.Contains(quiet, "This system does not ask for reduced motion.") {
		t.Errorf("motion hint missing: %s", quiet)
	}
}

func TestAnAxisPutsEveryChoiceOnTheSamePageColumn(t *testing.T) {
	markup := render(t, appearanceview.Component(appearanceview.Props{
		Theme: design.ThemeDark, Density: design.DensityComfortable,
		Axis:    appearanceview.Axis{Measure: 620, Value: 360, Rail: 170},
		OnTheme: func(design.Theme) {},
	}))
	// Every row reserves the rail to the right of the value column, whether or
	// not it has anything to put there. An axis is only shared if it stays in
	// the same place on every row, so the reservation is the layout.
	rails := strings.Count(markup, "</div><span></span></div>")
	if rails != 3 {
		t.Fatalf("want three rows holding the axis open, got %d: %s", rails, markup)
	}
}

func TestWithoutAnAxisTheLabelStaysAboveItsOptions(t *testing.T) {
	markup := render(t, appearanceview.Component(appearanceview.Props{
		Theme: design.ThemeDark, Density: design.DensityComfortable,
	}))
	// A narrow column has no axis to reserve: the label sits above its options
	// and the options take the width they need.
	if strings.Contains(markup, "</div><span></span></div>") {
		t.Fatalf("a stacked row must not reserve a value axis: %s", markup)
	}
}
