package testselection

import (
	"reflect"
	"slices"
	"testing"
)

func TestSelectCombinesEveryDeterministicSourceWithReasonsAndCoverage(t *testing.T) {
	packageCommand := command(CostPackage, "go", "test", "./internal/widget")
	mappedCommand := command(CostFocused, "go", "test", "./internal/widget", "-run", "TestParse")
	baselineCommand := command(CostFocused, "go", "test", "./internal/store", "-run", "TestCommit")
	repositoryCommand := command(CostBroad, "go", "test", "./...")
	acceptanceCommand := Command{Executable: "go", Arguments: []string{"test", "./acceptance", "-run", "Test User Spec", ""}, WorkingDirectory: "tools", Cost: CostBroad}

	result, err := Select(Request{
		ChangedFiles: []ChangedFile{
			{Path: "internal/widget/parser.go", Package: "./internal/widget"},
			{Path: "internal/store/commit.go", Package: "./internal/store"},
		},
		PackageChecks:         []PackageCheck{{Package: "./internal/widget", Command: packageCommand}},
		FileMappings:          []FileMapping{{ChangedPath: "internal/widget/parser.go", TestPaths: []string{"internal/widget/parser_test.go"}, Command: mappedCommand}},
		FailingBaselineChecks: []BaselineFailure{{Command: baselineCommand, ChangedPaths: []string{"internal/store/commit.go"}}},
		RepositoryChecks:      []Command{repositoryCommand}, ProtectedChange: true, RepositoryWideFeasible: true,
		AcceptanceChecks: []AcceptanceCheck{{Command: acceptanceCommand}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PolicyVersion != PolicyVersion || len(result.Checks) != 5 {
		t.Fatalf("selection = %#v", result)
	}
	wantOrder := []Command{baselineCommand, mappedCommand, packageCommand, acceptanceCommand, repositoryCommand}
	for index, want := range wantOrder {
		if !reflect.DeepEqual(result.Checks[index].Command, want) {
			t.Fatalf("check %d command = %#v, want %#v", index, result.Checks[index].Command, want)
		}
	}
	assertCheck(t, result, mappedCommand, []Reason{ReasonDeterministicMapping}, []string{"internal/widget/parser.go"}, []string{"internal/widget/parser_test.go"})
	assertCheck(t, result, baselineCommand, []Reason{ReasonFailingBaseline}, []string{"internal/store/commit.go"}, nil)
	assertCheck(t, result, packageCommand, []Reason{ReasonChangedPackage}, []string{"internal/widget/parser.go"}, nil)
	assertCheck(t, result, acceptanceCommand, []Reason{ReasonUserAcceptance}, nil, nil)
	assertCheck(t, result, repositoryCommand, []Reason{ReasonProtectedRepository}, []string{"internal/store/commit.go", "internal/widget/parser.go"}, nil)
}

func TestSelectDeduplicatesEquivalentCommandsAndMergesAttribution(t *testing.T) {
	sharedFocused := command(CostFocused, "go", "test", "./internal/widget")
	sharedConservative := sharedFocused
	sharedConservative.Cost = CostBroad
	result, err := Select(Request{
		ChangedFiles:          []ChangedFile{{Path: "internal/widget/a.go", Package: "./internal/widget"}, {Path: "internal/widget/b.go", Package: "./internal/widget"}},
		PackageChecks:         []PackageCheck{{Package: "./internal/widget", Command: sharedFocused}},
		FileMappings:          []FileMapping{{ChangedPath: "internal/widget/a.go", TestPaths: []string{"internal/widget/a_test.go"}, Command: sharedConservative}},
		FailingBaselineChecks: []BaselineFailure{{Command: sharedFocused, ChangedPaths: []string{"internal/widget/b.go"}}},
		AcceptanceChecks:      []AcceptanceCheck{{Command: sharedFocused, ChangedPaths: []string{"internal/widget/a.go"}}},
		RepositoryChecks:      []Command{sharedFocused}, ProtectedChange: true, RepositoryWideFeasible: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Checks) != 1 {
		t.Fatalf("deduplicated checks = %#v", result.Checks)
	}
	check := result.Checks[0]
	wantReasons := []Reason{ReasonFailingBaseline, ReasonDeterministicMapping, ReasonChangedPackage, ReasonUserAcceptance, ReasonProtectedRepository}
	if !reflect.DeepEqual(check.Reasons, wantReasons) || !reflect.DeepEqual(check.CoveredChangedFiles, []string{"internal/widget/a.go", "internal/widget/b.go"}) {
		t.Fatalf("merged attribution = %#v", check)
	}
	if check.Command.Cost != CostBroad {
		t.Fatalf("conflicting duplicate cost = %q, want conservative broad", check.Command.Cost)
	}
}

func TestSelectRepositoryWideCheckRequiresProtectedFeasibleChange(t *testing.T) {
	request := Request{ChangedFiles: []ChangedFile{{Path: "internal/auth/token.go"}}, RepositoryChecks: []Command{command(CostBroad, "go", "test", "./...")}}
	for _, test := range []struct {
		name      string
		protected bool
		feasible  bool
		want      int
	}{{"routine", false, true, 0}, {"protected infeasible", true, false, 0}, {"protected feasible", true, true, 1}} {
		t.Run(test.name, func(t *testing.T) {
			request.ProtectedChange, request.RepositoryWideFeasible = test.protected, test.feasible
			result, err := Select(request)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Checks) != test.want {
				t.Fatalf("checks = %#v", result.Checks)
			}
		})
	}
}

func TestSelectIgnoresMappingsAndPackagesNotImplicatedByChanges(t *testing.T) {
	result, err := Select(Request{
		ChangedFiles:  []ChangedFile{{Path: "internal/widget/widget.go", Package: "./internal/widget"}},
		PackageChecks: []PackageCheck{{Package: "./internal/other", Command: command(CostPackage, "go", "test", "./internal/other")}},
		FileMappings:  []FileMapping{{ChangedPath: "internal/other/other.go", Command: command(CostFocused, "go", "test", "./internal/other", "-run", "TestOther")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Checks) != 0 {
		t.Fatalf("unimplicated selection = %#v", result.Checks)
	}
}

func TestSelectIsStableAcrossCandidatePermutation(t *testing.T) {
	first := command(CostTiny, "go", "test", "./a")
	second := command(CostTiny, "go", "test", "./b")
	request := Request{ChangedFiles: []ChangedFile{{Path: "a/a.go", Package: "./a"}, {Path: "b/b.go", Package: "./b"}},
		PackageChecks: []PackageCheck{{Package: "./b", Command: second}, {Package: "./a", Command: first}}}
	left, err := Select(request)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(request.ChangedFiles)
	slices.Reverse(request.PackageChecks)
	right, err := Select(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("permuted selection differs:\nleft=%#v\nright=%#v", left, right)
	}
}

func TestSelectRejectsUnsafeOrUnboundedInputs(t *testing.T) {
	valid := command(CostFocused, "go", "test", "./...")
	for _, test := range []struct {
		name    string
		request Request
	}{
		{"traversal", Request{ChangedFiles: []ChangedFile{{Path: "../secret.go"}}}},
		{"absolute", Request{ChangedFiles: []ChangedFile{{Path: "/secret.go"}}}},
		{"duplicate changed file", Request{ChangedFiles: []ChangedFile{{Path: "a.go"}, {Path: "a.go"}}}},
		{"invalid cost", Request{AcceptanceChecks: []AcceptanceCheck{{Command: Command{Executable: "go", Cost: "instant"}}}}},
		{"nul argument", Request{AcceptanceChecks: []AcceptanceCheck{{Command: Command{Executable: "go", Arguments: []string{"a\x00b"}, Cost: CostFocused}}}}},
		{"unsafe coverage", Request{FailingBaselineChecks: []BaselineFailure{{Command: valid, ChangedPaths: []string{"../a.go"}}}}},
		{"unknown coverage", Request{ChangedFiles: []ChangedFile{{Path: "a.go"}}, FailingBaselineChecks: []BaselineFailure{{Command: valid, ChangedPaths: []string{"b.go"}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Select(test.request); err == nil {
				t.Fatal("unsafe selection input was accepted")
			}
		})
	}
}

func command(cost CostClass, executable string, arguments ...string) Command {
	return Command{Executable: executable, Arguments: arguments, Cost: cost}
}

func assertCheck(t *testing.T, result Result, command Command, reasons []Reason, coverage, tests []string) {
	t.Helper()
	for _, check := range result.Checks {
		if commandKey(check.Command) == commandKey(command) {
			if !reflect.DeepEqual(check.Reasons, reasons) || !reflect.DeepEqual(check.CoveredChangedFiles, coverage) || !reflect.DeepEqual(check.MappedTestFiles, tests) {
				t.Fatalf("check attribution = %#v, want reasons=%#v coverage=%#v tests=%#v", check, reasons, coverage, tests)
			}
			return
		}
	}
	t.Fatalf("command was not selected: %#v", command)
}
