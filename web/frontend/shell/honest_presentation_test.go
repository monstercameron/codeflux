package shell

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func honestRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func honestTestMode(t *testing.T) primitives.Mode {
	t.Helper()
	tokens, err := design.TokensFor(design.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return primitives.Mode{Theme: tokens.Theme, Density: tokens.Density}
}

func TestTheChooserListsWhatCanActuallyBeOpened(t *testing.T) {
	// The chooser was drawn from a hardcoded count of zero, so a person with a
	// repository already open read "0 recent workspaces" beside a browse
	// control that could not create one either.
	markup := honestRender(t, RepositoryChooserShell(RepositoryChooserProps{
		State: state.DataReady,
		Choices: []RepositoryChoice{
			{Name: "orders-service", Detail: "Open thread", Revision: "d176089",
				Path: "/workspace/repo_x/thread/thr_y"},
		},
		Mode: honestTestMode(t),
	}))
	if strings.Contains(markup, "0 recent workspaces") {
		t.Error("the chooser reported nothing while holding a repository")
	}
	if !strings.Contains(markup, "orders-service") {
		t.Errorf("the chooser did not name the repository:\n%s", markup)
	}
	if !strings.Contains(markup, "/workspace/repo_x/thread/thr_y") {
		t.Error("the row carries no path, so nothing can be opened from it")
	}
}

func TestAReadyChooserHoldingNothingSaysSoRatherThanCountingToZero(t *testing.T) {
	markup := honestRender(t, RepositoryChooserShell(RepositoryChooserProps{
		State: state.DataReady,
		Mode:  honestTestMode(t),
	}))
	if strings.Contains(markup, "0 recent workspaces") {
		t.Errorf("an empty chooser counted to zero instead of saying it is empty:\n%s", markup)
	}
	if !strings.Contains(markup, "No recent workspaces") {
		t.Errorf("an empty chooser did not say it is empty:\n%s", markup)
	}
}

func TestAnInertRowIsNotDrawnAsALink(t *testing.T) {
	// A link that goes nowhere is worse than a line of text: it invites a click
	// and answers with silence.
	markup := honestRender(t, RepositoryChooserShell(RepositoryChooserProps{
		State:   state.DataReady,
		Choices: []RepositoryChoice{{Name: "unreachable", Detail: "No open thread"}},
		Mode:    honestTestMode(t),
	}))
	if strings.Contains(markup, "<a") {
		t.Errorf("a row with no path was drawn as a link:\n%s", markup)
	}
	if !strings.Contains(markup, "unreachable") {
		t.Error("the row disappeared rather than being drawn as text")
	}
}

func TestWithNoTaskTheMetricStripSaysOneTrueThing(t *testing.T) {
	// This strip printed six labels against six copies of the word Unknown —
	// the widest, boldest, most colourful row on the page saying nothing at all.
	tokens, err := design.TokensFor(design.Options{})
	if err != nil {
		t.Fatal(err)
	}
	markup := honestRender(t, taskMetricStrip(
		state.TopBarView{}, "unknown", tokens.Colors.Active, tokens))
	if count := strings.Count(markup, "Unknown"); count > 0 {
		t.Errorf("a strip with no task printed %d Unknown value(s):\n%s", count, markup)
	}
	// The bug this guards is six labels against six copies of the word Unknown:
	// the widest, boldest row on the page saying nothing at all. Whether the
	// strip then states one true sentence or draws nothing is a design choice;
	// stating nothing false is not.
	if !strings.Contains(markup, `data-state="no-task"`) {
		t.Errorf("the strip does not mark itself as having no task:\n%s", markup)
	}
}

func TestWithATaskTheMetricStripReportsIt(t *testing.T) {
	tokens, err := design.TokensFor(design.Options{})
	if err != nil {
		t.Fatal(err)
	}
	markup := honestRender(t, taskMetricStrip(state.TopBarView{
		TaskState: "in progress", ActualCost: "$1.20", ActualTokens: "12,480",
		HardBudget: "$4.00",
	}, "In progress", tokens.Colors.Active, tokens))
	// The strip carries the three facts the centre of the workspace needs. The
	// cap and what is left of it are the header meter's job and the run rail's,
	// and printing all six here made the widest row the least useful one.
	for _, expected := range []string{"In progress", "$1.20", "12,480"} {
		if !strings.Contains(markup, expected) {
			t.Errorf("the strip did not report %q:\n%s", expected, markup)
		}
	}
	if strings.Contains(markup, "No task is running") {
		t.Error("a running task was reported as no task")
	}
}

func TestScopedDestinationsFollowWhatIsOpenNotOnlyTheURL(t *testing.T) {
	// Standing on the repository chooser — the page every session starts on —
	// disabled Tasks and Memory with "Choose a repository first" while a
	// repository was already open, and the only enabled way to one was the
	// page the person was already looking at.
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	destinations := navigationDestinations(
		routes.Route{Name: routes.RepositoryChooser},
		NavigationScope{RepositoryID: repositoryID, ThreadID: threadID},
	)
	byLabel := map[string]navigationDestination{}
	for _, destination := range destinations {
		byLabel[destination.label] = destination
	}
	for _, label := range []string{"Tasks", "Memory"} {
		if byLabel[label].path == "" {
			t.Errorf("%s is unreachable while a repository is open: %q",
				label, byLabel[label].reason)
		}
	}
	if !strings.Contains(byLabel["Tasks"].path, threadID.String()) {
		t.Errorf("Tasks = %q, want the open conversation", byLabel["Tasks"].path)
	}
	// No two destinations may be the same place. Home and Repositories both
	// pointed at the chooser, so two rail items lit up together on the page
	// every session begins at.
	seen := map[string]string{}
	for _, destination := range destinations {
		if destination.path == "" {
			continue
		}
		if owner, taken := seen[destination.path]; taken {
			t.Errorf("%s and %s are the same destination: %q",
				owner, destination.label, destination.path)
		}
		seen[destination.path] = destination.label
	}
}

func TestWithNothingOpenScopedDestinationsStillRefuseWithAReason(t *testing.T) {
	// A control that cannot work must say why. Enabling it against no
	// repository would produce a link to a path that does not parse.
	destinations := navigationDestinations(
		routes.Route{Name: routes.RepositoryChooser}, NavigationScope{})
	for _, destination := range destinations {
		if destination.label != "Tasks" && destination.label != "Memory" {
			continue
		}
		if destination.path != "" {
			t.Errorf("%s offered a path with no repository open", destination.label)
		}
		if destination.reason == "" {
			t.Errorf("%s is disabled without saying why", destination.label)
		}
	}
}

func TestExactlyOneRailItemIsMarkedCurrent(t *testing.T) {
	// A rail with two current pages tells a person nothing about where they
	// are. Home and Repositories were marked together on the chooser.
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	scope := NavigationScope{RepositoryID: repositoryID, ThreadID: threadID}
	for name, route := range map[string]routes.Route{
		"the chooser": {Name: routes.RepositoryChooser},
		"a thread":    {Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID},
		"settings":    {Name: routes.Settings},
		"graphs":      {Name: routes.Graphs},
	} {
		t.Run(name, func(t *testing.T) {
			marked := []string{}
			for _, destination := range navigationDestinations(route, scope) {
				if routeSelected(route, destination.label, destination.path) {
					marked = append(marked, destination.label)
				}
			}
			if len(marked) != 1 {
				t.Errorf("%d rail item(s) marked current on %s: %v",
					len(marked), name, marked)
			}
		})
	}
}
