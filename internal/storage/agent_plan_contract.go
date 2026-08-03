package storage

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
)

// TaskType is the deterministic broad shape of one user requirement.
type TaskType string

const (
	TaskTypeBugFix        TaskType = "bug-fix"
	TaskTypeFeature       TaskType = "feature"
	TaskTypeRefactor      TaskType = "refactor"
	TaskTypeDocumentation TaskType = "documentation"
	TaskTypeTest          TaskType = "test"
	TaskTypeInvestigation TaskType = "investigation"
	TaskTypeMaintenance   TaskType = "maintenance"
)

func (value TaskType) valid() bool {
	switch value {
	case TaskTypeBugFix, TaskTypeFeature, TaskTypeRefactor,
		TaskTypeDocumentation, TaskTypeTest, TaskTypeInvestigation,
		TaskTypeMaintenance:
		return true
	default:
		return false
	}
}

// ValidationProfileName is the fixed M14 validation floor selected from risk.
type ValidationProfileName string

const (
	ValidationProfileRoutine   ValidationProfileName = "routine-v1"
	ValidationProfileElevated  ValidationProfileName = "elevated-v1"
	ValidationProfileProtected ValidationProfileName = "protected-v1"
)

func validationProfileForRisk(risk domain.RiskLevel) ValidationProfileName {
	switch risk {
	case domain.RiskLevelProtected:
		return ValidationProfileProtected
	case domain.RiskLevelElevated:
		return ValidationProfileElevated
	default:
		return ValidationProfileRoutine
	}
}

// RequirementAmbiguity records either a blocking question or a bounded
// non-material assumption.
type RequirementAmbiguity struct {
	Topic             string `json:"topic"`
	Material          bool   `json:"material"`
	Question          string `json:"question,omitempty"`
	BoundedAssumption string `json:"bounded_assumption,omitempty"`
}

// RequirementAnalysis is deterministic structured intake over the already
// persisted original user message.
type RequirementAnalysis struct {
	SchemaVersion      uint64                 `json:"schema_version"`
	ClassifierVersion  string                 `json:"classifier_version"`
	TaskType           TaskType               `json:"task_type"`
	ExplicitFiles      []string               `json:"explicit_files"`
	ExplicitSymbols    []string               `json:"explicit_symbols"`
	ExplicitCommands   []string               `json:"explicit_commands"`
	AcceptanceCriteria []string               `json:"acceptance_criteria"`
	Ambiguities        []RequirementAmbiguity `json:"ambiguities"`
	Assumptions        []string               `json:"assumptions"`
	RiskLevel          domain.RiskLevel       `json:"risk_level"`
	ValidationProfile  ValidationProfileName  `json:"validation_profile"`
}

const requirementClassifierVersion = "deterministic-requirement-v1"

var (
	// A path is only a file when its last segment carries a known extension.
	// Requiring one everywhere is what keeps a module path out: "example.com/demo"
	// has slashes and a dot but names no file, and taking it for one put a file
	// in the plan's expected set that no step could ever cover, which refused
	// the whole plan.
	requirementPathPattern = regexp.MustCompile(
		`(?i)(?:^|[\s"'()])((?:[a-z0-9_.-]+/)*[a-z0-9_-]+\.(?:go|md|sql|proto|json|yaml|yml|toml|mod|sum))`,
	)
	requirementCodePattern = regexp.MustCompile("`([^`\\r\\n]{1,256})`")
)

// AnalyzeTaskRequirement classifies and extracts bounded facts with a stable
// rule set. It does not call a model and therefore cannot silently vary.
//
// The body is normalized here, once, before anything is derived from it
// (PIPE-019a). Every caller that needs "the analysis of this requirement" —
// the ambiguity check, the plan builder, and the store's own re-derivation
// from the persisted message — reaches this function with whatever leading
// or trailing whitespace its own copy happens to carry, and a mandatory
// multi-line <<<ACCEPTANCE ... >>> block (PIPE-019) leaves a trailing
// newline on every requirement that uses it. Refusing that instead of
// normalizing it made the three call sites disagree about whether "the
// requirement text" included the newline, which is the same fact computed
// three times with three different answers. Trimming inside the one function
// every caller already goes through makes it impossible for them to
// disagree, without touching what was actually persisted: the stored user
// message is untouched by this, because nothing here writes it back. Only
// the derived analysis is normalized, and only in this one place.
func AnalyzeTaskRequirement(body string) (RequirementAnalysis, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 131_072 {
		return RequirementAnalysis{},
			errors.New("requirement body must be non-empty and bounded")
	}
	lower := strings.ToLower(body)
	analysis := RequirementAnalysis{
		SchemaVersion:      1,
		ClassifierVersion:  requirementClassifierVersion,
		TaskType:           classifyTaskType(lower),
		ExplicitFiles:      extractRequirementFiles(body),
		ExplicitSymbols:    extractRequirementSymbols(body),
		ExplicitCommands:   extractRequirementCommands(body),
		AcceptanceCriteria: extractAcceptanceCriteria(body),
	}
	analysis.Ambiguities = classifyRequirementAmbiguities(lower)
	for _, ambiguity := range analysis.Ambiguities {
		if !ambiguity.Material && ambiguity.BoundedAssumption != "" {
			analysis.Assumptions = append(
				analysis.Assumptions,
				ambiguity.BoundedAssumption,
			)
		}
	}
	analysis.RiskLevel = classifyRequirementRisk(lower, analysis)
	analysis.ValidationProfile = validationProfileForRisk(analysis.RiskLevel)
	if err := analysis.Validate(); err != nil {
		return RequirementAnalysis{}, err
	}
	return analysis, nil
}

// Validate checks deterministic classification and clarification consistency.
func (value RequirementAnalysis) Validate() error {
	switch {
	case value.SchemaVersion != 1:
		return errors.New("requirement analysis schema version is unsupported")
	case value.ClassifierVersion != requirementClassifierVersion:
		return errors.New("requirement classifier version is unsupported")
	case !value.TaskType.valid():
		return errors.New("requirement task type is invalid")
	case !value.RiskLevel.IsValid():
		return errors.New("requirement risk is invalid")
	case value.ValidationProfile != validationProfileForRisk(value.RiskLevel):
		return errors.New("requirement validation profile differs from risk floor")
	case !normalizedPlanPaths(value.ExplicitFiles, 512),
		!normalizedPlanStrings(value.ExplicitSymbols, 512, 512),
		!normalizedPlanStrings(value.ExplicitCommands, 128, 4096),
		!normalizedPlanStrings(value.AcceptanceCriteria, 128, 4096),
		!normalizedPlanStrings(value.Assumptions, 128, 4096):
		return errors.New("requirement extracted values are not normalized")
	}
	for _, ambiguity := range value.Ambiguities {
		switch {
		case strings.TrimSpace(ambiguity.Topic) != ambiguity.Topic,
			ambiguity.Topic == "", len(ambiguity.Topic) > 512:
			return errors.New("requirement ambiguity topic is invalid")
		case ambiguity.Material &&
			(strings.TrimSpace(ambiguity.Question) != ambiguity.Question ||
				ambiguity.Question == "" ||
				len(ambiguity.Question) > 2048 ||
				ambiguity.BoundedAssumption != ""):
			return errors.New("material ambiguity requires one targeted question")
		case !ambiguity.Material &&
			(strings.TrimSpace(ambiguity.BoundedAssumption) !=
				ambiguity.BoundedAssumption ||
				ambiguity.BoundedAssumption == "" ||
				len(ambiguity.BoundedAssumption) > 2048 ||
				ambiguity.Question != ""):
			return errors.New("non-material ambiguity requires one bounded assumption")
		}
	}
	return nil
}

// RequiresClarification reports whether planning must stop for user authority.
func (value RequirementAnalysis) RequiresClarification() bool {
	for _, ambiguity := range value.Ambiguities {
		if ambiguity.Material {
			return true
		}
	}
	return false
}

// ClarificationQuestion returns the first deterministic blocking question.
func (value RequirementAnalysis) ClarificationQuestion() string {
	for _, ambiguity := range value.Ambiguities {
		if ambiguity.Material {
			return ambiguity.Question
		}
	}
	return ""
}

// AgentPlan is one complete immutable structured execution plan.
type AgentPlan struct {
	SchemaVersion              uint64                 `json:"schema_version"`
	TaskType                   TaskType               `json:"task_type"`
	Goal                       string                 `json:"goal"`
	Scope                      []string               `json:"scope"`
	ExpectedFiles              []string               `json:"expected_files"`
	Steps                      []AgentPlanStep        `json:"steps"`
	ExplicitValidationCommands []string               `json:"explicit_validation_commands"`
	ValidationCommands         []string               `json:"validation_commands"`
	Risks                      []string               `json:"risks"`
	AuthorityNeeds             []string               `json:"authority_needs"`
	Assumptions                []string               `json:"assumptions"`
	Ambiguities                []RequirementAmbiguity `json:"ambiguities"`
	CompletionCriteria         []string               `json:"completion_criteria"`
}

// AgentPlanStepKind is a closed semantic intent. Each kind deterministically
// selects its only completion tool set; model-authored plans cannot widen it.
type AgentPlanStepKind string

const (
	StepKindEdit           AgentPlanStepKind = "edit"
	StepKindReadFile       AgentPlanStepKind = "read-file"
	StepKindListDirectory  AgentPlanStepKind = "list-directory"
	StepKindSearchText     AgentPlanStepKind = "search-text"
	StepKindSearchSymbol   AgentPlanStepKind = "search-symbol"
	StepKindInspectDiff    AgentPlanStepKind = "inspect-diff"
	StepKindGitStatus      AgentPlanStepKind = "git-status"
	StepKindGitHistory     AgentPlanStepKind = "git-history"
	StepKindTest           AgentPlanStepKind = "test"
	StepKindBuild          AgentPlanStepKind = "build"
	StepKindStaticAnalysis AgentPlanStepKind = "static-analysis"
)

func (kind AgentPlanStepKind) completionTools() ([]string, bool) {
	var tool executor.ToolName
	switch kind {
	case StepKindEdit:
		tool = executor.ToolApplyEdit
	case StepKindReadFile:
		tool = executor.ToolReadFile
	case StepKindListDirectory:
		tool = executor.ToolListDirectory
	case StepKindSearchText:
		tool = executor.ToolSearchText
	case StepKindSearchSymbol:
		tool = executor.ToolSearchSymbol
	case StepKindInspectDiff:
		tool = executor.ToolInspectDiff
	case StepKindGitStatus:
		tool = executor.ToolGitStatus
	case StepKindGitHistory:
		tool = executor.ToolGitHistory
	case StepKindTest:
		tool = executor.ToolTest
	case StepKindBuild:
		tool = executor.ToolBuild
	case StepKindStaticAnalysis:
		tool = executor.ToolStaticAnalysis
	default:
		return nil, false
	}
	return []string{string(tool)}, true
}

// AgentPlanStep is one machine-readable ordered unit of work.
type AgentPlanStep struct {
	ID                 string            `json:"id"`
	Kind               AgentPlanStepKind `json:"kind"`
	Title              string            `json:"title"`
	DetailRedacted     string            `json:"detail_redacted"`
	ExpectedFiles      []string          `json:"expected_files"`
	ValidationCommands []string          `json:"validation_commands"`
	CompletionTools    []string          `json:"completion_tools"`
	AuthorityNeeds     []string          `json:"authority_needs"`
	GraphNodeIDs       []domain.NodeID   `json:"graph_node_ids"`
}

// AgentPlanStepDraft omits the deterministic machine ID.
type AgentPlanStepDraft struct {
	Kind               AgentPlanStepKind
	Title              string
	DetailRedacted     string
	ExpectedFiles      []string
	ValidationCommands []string
	AuthorityNeeds     []string
	GraphNodeIDs       []domain.NodeID
}

// AgentPlanDraft contains the bounded human/model-authored plan content.
type AgentPlanDraft struct {
	Goal               string
	Scope              []string
	ExpectedFiles      []string
	Steps              []AgentPlanStepDraft
	ValidationCommands []string
	Risks              []string
	AuthorityNeeds     []string
	Assumptions        []string
	CompletionCriteria []string
}

// BuildAgentPlan assigns stable step IDs and carries requirement assumptions
// and unresolved ambiguity into the user-visible plan.
func BuildAgentPlan(
	analysis RequirementAnalysis,
	draft AgentPlanDraft,
) (AgentPlan, error) {
	if err := analysis.Validate(); err != nil {
		return AgentPlan{}, err
	}
	explicitValidationCommands, err :=
		canonicalRequirementValidationCommands(
			analysis.ExplicitCommands,
		)
	if err != nil {
		return AgentPlan{}, err
	}
	steps := make([]AgentPlanStep, len(draft.Steps))
	for index, step := range draft.Steps {
		steps[index] = AgentPlanStep{
			ID:                 fmt.Sprintf("step-%03d", index+1),
			Kind:               step.Kind,
			Title:              step.Title,
			DetailRedacted:     step.DetailRedacted,
			ExpectedFiles:      normalizedPaths(step.ExpectedFiles),
			ValidationCommands: normalizedStrings(step.ValidationCommands),
			CompletionTools:    mustCompletionTools(step.Kind),
			AuthorityNeeds:     normalizedStrings(step.AuthorityNeeds),
			GraphNodeIDs:       append([]domain.NodeID(nil), step.GraphNodeIDs...),
		}
	}
	plan := AgentPlan{
		SchemaVersion: 1,
		TaskType:      analysis.TaskType,
		Goal:          draft.Goal,
		Scope:         normalizedStrings(draft.Scope),
		ExpectedFiles: normalizedPaths(append(
			append([]string(nil), analysis.ExplicitFiles...),
			draft.ExpectedFiles...,
		)),
		Steps: steps,
		// The copy starts from an empty slice rather than a nil one so that a
		// requirement naming no command encodes as [] here exactly as it does in
		// the analysis. Both sides state the same fact, and the store compares
		// them as JSON: nil encodes as null, null does not equal [] in SQL, and
		// every plan for a requirement naming no explicit command was refused.
		ExplicitValidationCommands: append(
			make([]string, 0, len(analysis.ExplicitCommands)),
			analysis.ExplicitCommands...,
		),
		ValidationCommands: normalizedStrings(append(
			explicitValidationCommands,
			draft.ValidationCommands...,
		)),
		Risks:          normalizedStrings(draft.Risks),
		AuthorityNeeds: normalizedStrings(draft.AuthorityNeeds),
		Assumptions: normalizedStrings(append(
			append([]string(nil), analysis.Assumptions...),
			draft.Assumptions...,
		)),
		Ambiguities: append([]RequirementAmbiguity(nil), analysis.Ambiguities...),
		CompletionCriteria: normalizedStrings(append(
			append([]string(nil), analysis.AcceptanceCriteria...),
			draft.CompletionCriteria...,
		)),
	}
	if err := plan.Validate(); err != nil {
		return AgentPlan{}, err
	}
	return plan, nil
}

// Validate checks complete plan content and deterministic step identities.
func (value AgentPlan) Validate() error {
	switch {
	case value.SchemaVersion != 1:
		return errors.New("agent plan schema version is unsupported")
	case !value.TaskType.valid():
		return errors.New("agent plan task type is invalid")
	case strings.TrimSpace(value.Goal) != value.Goal,
		value.Goal == "", len(value.Goal) > 4096:
		return errors.New("agent plan goal is invalid")
	case len(value.Scope) == 0:
		return errors.New("agent plan scope is required")
	case len(value.Steps) == 0 || len(value.Steps) > 128:
		return errors.New("agent plan steps are required and bounded")
	case len(value.CompletionCriteria) == 0:
		return errors.New("agent plan completion criteria are required")
	case len(value.ValidationCommands) == 0:
		return errors.New("agent plan validation commands are required")
	case !normalizedPlanStrings(value.Scope, 128, 4096),
		!normalizedPlanPaths(value.ExpectedFiles, 512),
		!normalizedPlanStrings(
			value.ExplicitValidationCommands, 128, 4096,
		),
		!normalizedPlanStrings(value.ValidationCommands, 128, 4096),
		!normalizedPlanStrings(value.Risks, 128, 4096),
		!normalizedPlanStrings(value.AuthorityNeeds, 128, 4096),
		!normalizedPlanStrings(value.Assumptions, 128, 4096),
		!normalizedPlanStrings(value.CompletionCriteria, 128, 4096):
		return errors.New("agent plan lists are not normalized")
	}
	for _, command := range value.ValidationCommands {
		if err := executor.ValidateValidationPlanCommand(command); err != nil {
			return errors.New("agent plan validation command is not canonical")
		}
	}
	for index, step := range value.Steps {
		derivedTools, kindValid := step.Kind.completionTools()
		if step.ID != fmt.Sprintf("step-%03d", index+1) ||
			!kindValid ||
			strings.TrimSpace(step.Title) != step.Title ||
			step.Title == "" ||
			len(step.Title) > 512 ||
			strings.TrimSpace(step.DetailRedacted) != step.DetailRedacted ||
			step.DetailRedacted == "" ||
			len(step.DetailRedacted) > 4096 ||
			!normalizedPlanPaths(step.ExpectedFiles, 512) ||
			!normalizedPlanStrings(step.ValidationCommands, 128, 4096) ||
			!slices.Equal(step.CompletionTools, derivedTools) ||
			!normalizedPlanStrings(step.AuthorityNeeds, 128, 4096) {
			return fmt.Errorf("agent plan step %d is invalid", index+1)
		}
		if stepKindRequiresExpectedFiles(step.Kind) &&
			len(step.ExpectedFiles) == 0 {
			return fmt.Errorf(
				"agent plan path-scoped step %d requires expected files",
				index+1,
			)
		}
		for _, command := range step.ValidationCommands {
			if err := executor.ValidateValidationPlanCommand(command); err != nil ||
				!slices.Contains(value.ValidationCommands, command) {
				return fmt.Errorf(
					"agent plan step %d validation command is unbound",
					index+1,
				)
			}
		}
		seenNodes := make(map[domain.NodeID]struct{}, len(step.GraphNodeIDs))
		for _, nodeID := range step.GraphNodeIDs {
			if nodeID.IsZero() {
				return fmt.Errorf("agent plan step %d has an empty graph node", index+1)
			}
			if _, exists := seenNodes[nodeID]; exists {
				return fmt.Errorf("agent plan step %d repeats a graph node", index+1)
			}
			seenNodes[nodeID] = struct{}{}
		}
	}
	if err := value.validateEditCoverage(); err != nil {
		return err
	}
	return nil
}

func stepKindRequiresExpectedFiles(kind AgentPlanStepKind) bool {
	switch kind {
	case StepKindEdit, StepKindReadFile, StepKindListDirectory,
		StepKindSearchText, StepKindSearchSymbol:
		return true
	default:
		return false
	}
}

func mustCompletionTools(kind AgentPlanStepKind) []string {
	tools, _ := kind.completionTools()
	return tools
}

func (value AgentPlan) validateEditCoverage() error {
	if value.TaskType == TaskTypeInvestigation {
		return validateExpectedPathCoverage(
			value.ExpectedFiles,
			value.Steps,
			func(kind AgentPlanStepKind) bool {
				return stepKindRequiresExpectedFiles(kind)
			},
			"investigation",
		)
	}
	editSteps := 0
	for _, step := range value.Steps {
		if step.Kind == StepKindEdit {
			editSteps++
		}
	}
	if editSteps == 0 {
		return errors.New("non-investigation plan requires an edit step")
	}
	return validateExpectedPathCoverage(
		value.ExpectedFiles,
		value.Steps,
		func(kind AgentPlanStepKind) bool { return kind == StepKindEdit },
		"edit-step",
	)
}

func validateExpectedPathCoverage(
	expected []string,
	steps []AgentPlanStep,
	inScope func(AgentPlanStepKind) bool,
	label string,
) error {
	for _, path := range expected {
		covered := false
		for _, step := range steps {
			if !inScope(step.Kind) {
				continue
			}
			for _, stepPath := range step.ExpectedFiles {
				if planPathCovers(path, stepPath) {
					covered = true
					break
				}
			}
			if covered {
				break
			}
		}
		if !covered {
			return fmt.Errorf(
				"expected file %q lacks %s coverage", path, label,
			)
		}
	}
	return nil
}

func planPathCovers(scope, target string) bool {
	return scope == target || strings.HasPrefix(target, scope+"/")
}

// UserSummary renders a concise deterministic projection for plan approval.
func (value AgentPlan) UserSummary() string {
	var builder strings.Builder
	builder.WriteString("Task type: ")
	builder.WriteString(string(value.TaskType))
	builder.WriteString("\nGoal: ")
	builder.WriteString(value.Goal)
	writeSummaryList(&builder, "Scope", value.Scope)
	writeSummaryList(&builder, "Expected files", value.ExpectedFiles)
	writeSummaryList(
		&builder,
		"Requested validation commands",
		value.ExplicitValidationCommands,
	)
	writeSummaryList(&builder, "Validation commands", value.ValidationCommands)
	writeSummaryList(&builder, "Risks", value.Risks)
	writeSummaryList(&builder, "Authority needs", value.AuthorityNeeds)
	writeSummaryList(&builder, "Assumptions", value.Assumptions)
	builder.WriteString("\nAmbiguities: ")
	if len(value.Ambiguities) == 0 {
		builder.WriteString("(none)")
	}
	for index, ambiguity := range value.Ambiguities {
		if index != 0 {
			builder.WriteString("; ")
		}
		builder.WriteString(ambiguity.Topic)
		if ambiguity.Material {
			builder.WriteString(" [material; question=")
			builder.WriteString(ambiguity.Question)
		} else {
			builder.WriteString(" [bounded; assumption=")
			builder.WriteString(ambiguity.BoundedAssumption)
		}
		builder.WriteString("]")
	}
	writeSummaryList(&builder, "Completion criteria", value.CompletionCriteria)
	builder.WriteString("\nSteps:")
	for _, step := range value.Steps {
		builder.WriteString("\n")
		builder.WriteString(step.ID)
		builder.WriteString(": ")
		builder.WriteString(step.Title)
		builder.WriteString(" — ")
		builder.WriteString(step.DetailRedacted)
		builder.WriteString(" [kind=")
		builder.WriteString(string(step.Kind))
		builder.WriteString("; completion-tools=")
		builder.WriteString(strings.Join(step.CompletionTools, ","))
		builder.WriteString("; expected-files=")
		if len(step.ExpectedFiles) == 0 {
			builder.WriteString("(none)")
		} else {
			builder.WriteString(strings.Join(step.ExpectedFiles, ","))
		}
		builder.WriteString("; validation-commands=")
		if len(step.ValidationCommands) == 0 {
			builder.WriteString("(none)")
		} else {
			builder.WriteString(strings.Join(step.ValidationCommands, ","))
		}
		builder.WriteString("; authority-needs=")
		if len(step.AuthorityNeeds) == 0 {
			builder.WriteString("(none)")
		} else {
			builder.WriteString(strings.Join(step.AuthorityNeeds, ","))
		}
		builder.WriteString("]")
	}
	return builder.String()
}

func writeSummaryList(
	builder *strings.Builder,
	label string,
	values []string,
) {
	builder.WriteString("\n")
	builder.WriteString(label)
	builder.WriteString(": ")
	if len(values) == 0 {
		builder.WriteString("(none)")
		return
	}
	builder.WriteString(strings.Join(values, "; "))
}

func classifyTaskType(lower string) TaskType {
	switch {
	case containsAny(lower, "fix ", "fixes ", "repair ", "resolve "):
		return TaskTypeBugFix
	case containsAny(lower, "refactor", "restructure", "rename"):
		return TaskTypeRefactor
	case containsAny(
		lower,
		"document ", "documentation", "update readme", "write readme",
		"update docs", "write docs",
	):
		return TaskTypeDocumentation
	case containsAny(lower, "add test", "write test", "test coverage"):
		return TaskTypeTest
	case containsAny(lower, "add ", "implement", "create", "support"):
		return TaskTypeFeature
	case containsAny(
		lower,
		"change ", "modify ", "update ", "remove ", "delete ",
		"replace ", "migrate ",
	):
		return TaskTypeMaintenance
	case containsAny(lower, "investigate", "diagnose", "analyze", "why "):
		return TaskTypeInvestigation
	case containsAny(lower, "bug", "broken", "error", "regression"):
		return TaskTypeBugFix
	default:
		return TaskTypeMaintenance
	}
}

func classifyRequirementRisk(
	lower string,
	analysis RequirementAnalysis,
) domain.RiskLevel {
	if containsAny(
		lower,
		"production", "deploy", "publish", "public repo", "delete",
		"credential", "secret", "authentication", "authorization",
		"security", "payment", "billing", "migration",
	) {
		return domain.RiskLevelProtected
	}
	if containsAny(
		lower,
		"dependency", "schema", "database", "api", "network",
		"install", "refactor", "configuration",
	) || len(analysis.ExplicitFiles) > 3 || len(analysis.ExplicitCommands) > 1 {
		return domain.RiskLevelElevated
	}
	return domain.RiskLevelRoutine
}

func classifyRequirementAmbiguities(lower string) []RequirementAmbiguity {
	switch {
	// The phrases name a choice of approach, not any use of the word either.
	// Matching bare "either " caught "holding either a value or an error" — a
	// description of a type, not a fork in the road — and stopped the run to
	// ask which alternative was meant. A question asked about something
	// unambiguous is worse than none: it teaches people to dismiss them.
	case containsAny(lower, "either in ", "either using ", "either by ",
		"either as ", "which one", "which approach", "or whichever"):
		return []RequirementAmbiguity{{
			Topic:    "material scope choice",
			Material: true,
			Question: "Which of the stated alternatives should define the implementation scope?",
		}}
	case containsAny(lower, "as appropriate", "if needed", "where useful"):
		return []RequirementAmbiguity{{
			Topic:             "implementation discretion",
			BoundedAssumption: "Use the smallest in-scope change that satisfies the explicit acceptance criteria.",
		}}
	default:
		return nil
	}
}

func extractRequirementFiles(body string) []string {
	var values []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "`"))
		if isRequirementCommand(trimmed) {
			continue
		}
		matches := requirementPathPattern.FindAllStringSubmatch(line, 512)
		for _, match := range matches {
			values = append(
				values,
				strings.TrimRight(
					strings.ReplaceAll(match[1], "\\", "/"),
					".,;:",
				),
			)
		}
	}
	return collapseRestatedPaths(normalizedPaths(values))
}

// collapseRestatedPaths drops a bare filename that another named path already
// covers.
//
// A request names its deliverables once and then talks about them: "create
// cmd/bank/main.go and cmd/bank/account.go, where account.go holds the type
// and main.go reads input". The second mention of each is the same file, not a
// third and fourth one. Counting them separately produced a plan step per
// mention — steps scoped to files at the repository root that the work would
// never touch — and pushed the request over the file count that classifies it
// as heavier work, so a plain two-file program demanded a plan approval it had
// no reason to need.
//
// Only bare names collapse, and only into a path that ends with them. Two real
// files that genuinely share a basename, in different directories, are both
// named with their directories and both survive.
func collapseRestatedPaths(paths []string) []string {
	covered := make(map[string]bool, len(paths))
	for _, candidate := range paths {
		index := strings.LastIndex(candidate, "/")
		if index < 0 {
			continue
		}
		covered[candidate[index+1:]] = true
	}
	kept := make([]string, 0, len(paths))
	for _, candidate := range paths {
		if !strings.Contains(candidate, "/") && covered[candidate] {
			continue
		}
		kept = append(kept, candidate)
	}
	return kept
}

func extractRequirementSymbols(body string) []string {
	matches := requirementCodePattern.FindAllStringSubmatch(body, 512)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		value := strings.TrimSpace(match[1])
		if !strings.ContainsAny(value, " /\\") &&
			!strings.Contains(value, ".go") &&
			!strings.HasPrefix(value, "go ") {
			values = append(values, value)
		}
	}
	return normalizedStrings(values)
}

func extractRequirementCommands(body string) []string {
	var values []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "`"))
		if isRequirementCommand(trimmed) {
			values = append(values, trimmed)
		}
	}
	return normalizedStrings(values)
}

func canonicalRequirementValidationCommands(
	values []string,
) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		request, err := parseRequirementValidationCommand(value)
		if err != nil {
			return nil, err
		}
		rendered, err := executor.RenderValidationPlanCommand(request)
		if err != nil {
			return nil, fmt.Errorf(
				"canonicalize explicit validation command: %w", err,
			)
		}
		result = append(result, rendered)
	}
	return normalizedStrings(result), nil
}

func parseRequirementValidationCommand(
	value string,
) (executor.ToolRequest, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 || strings.Join(fields, " ") != value {
		return executor.ToolRequest{},
			errors.New("explicit validation command has ambiguous spacing")
	}
	var tool executor.ToolName
	switch {
	case len(fields) >= 2 && fields[0] == "go" && fields[1] == "test":
		tool = executor.ToolTest
	case len(fields) >= 2 && fields[0] == "go" && fields[1] == "vet":
		tool = executor.ToolStaticAnalysis
	case fields[0] == "make":
		tool = executor.ToolBuild
	case fields[0] == "gofmt":
		return executor.ToolRequest{}, errors.New(
			"mutating formatter command cannot be a validation contract",
		)
	default:
		return executor.ToolRequest{},
			errors.New("explicit validation command is unsupported")
	}
	for _, field := range fields {
		if !safeValidationCommandToken(field) {
			return executor.ToolRequest{},
				errors.New("explicit validation command token is unsafe")
		}
		if looksSensitiveValidationToken(field) {
			return executor.ToolRequest{},
				errors.New("explicit validation command contains sensitive data")
		}
		lower := strings.ToLower(field)
		if strings.HasPrefix(lower, "-exec") ||
			strings.HasPrefix(lower, "-toolexec") ||
			strings.HasPrefix(lower, "-overlay") ||
			strings.HasPrefix(lower, "-vet=off") ||
			strings.HasPrefix(lower, "-vettool") {
			return executor.ToolRequest{},
				errors.New("explicit validation command weakens execution")
		}
		if separator := strings.IndexByte(field, '='); separator > 0 &&
			sensitiveValidationArgumentName(field[:separator]) {
			return executor.ToolRequest{},
				errors.New("explicit validation command contains sensitive data")
		}
	}
	arguments := make([]executor.ToolArgument, 0, len(fields))
	for index, field := range fields {
		name := "argument"
		if index == 0 {
			name = "executable"
		}
		arguments = append(arguments, executor.ToolArgument{
			Name: name, Value: field,
		})
	}
	descriptor, found := validationToolDescriptor(tool)
	if !found {
		return executor.ToolRequest{},
			errors.New("validation tool descriptor is unavailable")
	}
	return executor.ToolRequest{
		SchemaVersion:    executor.ToolSchemaVersion,
		Name:             tool,
		Arguments:        arguments,
		Timeout:          time.Minute,
		ClaimedAuthority: descriptor.DefaultAuthority,
		ExpectedSideEffects: append(
			[]executor.SideEffect(nil), descriptor.Effects...,
		),
	}, nil
}

func validationToolDescriptor(
	name executor.ToolName,
) (executor.ToolDescriptor, bool) {
	for _, descriptor := range executor.ToolCatalog() {
		if descriptor.Name == name {
			return descriptor, true
		}
	}
	return executor.ToolDescriptor{}, false
}

func safeValidationCommandToken(value string) bool {
	if value == "" || len(value) > 4096 ||
		strings.ContainsAny(value, "'\"`;$|&<>\\\r\n") {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func looksSensitiveValidationToken(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range []string{
		"sk-", "ghp_", "github_pat_", "xoxb-", "xoxp-",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func sensitiveValidationArgumentName(value string) bool {
	upper := strings.ToUpper(value)
	for _, fragment := range []string{
		"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL",
	} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

func isRequirementCommand(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "go test ") ||
		strings.HasPrefix(lower, "go vet ") ||
		strings.HasPrefix(lower, "gofmt ") ||
		strings.HasPrefix(lower, "make ")
}

func extractAcceptanceCriteria(body string) []string {
	var values []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "-*0123456789. "))
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "acceptance:") ||
			strings.HasPrefix(lower, "must ") ||
			strings.HasPrefix(lower, "ensure ") {
			values = append(values, trimmed)
		}
	}
	return normalizedStrings(values)
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func normalizedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizedPaths(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
		value = path.Clean(value)
		if validPlanPath(value) {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizedPlanPaths(values []string, maximum int) bool {
	return len(values) <= maximum &&
		slices.Equal(values, normalizedPaths(values))
}

func validPlanPath(value string) bool {
	return value != "" && value != "." && value != ".." &&
		!strings.HasPrefix(value, "/") &&
		!strings.HasPrefix(value, "../") &&
		!strings.Contains(value, ":") &&
		len(value) <= 4096
}

func normalizedPlanStrings(values []string, maximum, maxLength int) bool {
	if len(values) > maximum {
		return false
	}
	for index, value := range values {
		if strings.TrimSpace(value) != value ||
			value == "" ||
			len(value) > maxLength ||
			(index > 0 && values[index-1] >= value) {
			return false
		}
	}
	return true
}

// ValidationProfileForRisk reports the validation profile a risk level
// requires.
//
// The pairing is a rule of the contract rather than a choice a caller makes:
// an analysis whose profile does not match its risk is refused. It is exported
// so a caller that must adopt a risk decided elsewhere can keep the two in step
// instead of guessing the mapping.
func ValidationProfileForRisk(risk domain.RiskLevel) ValidationProfileName {
	return validationProfileForRisk(risk)
}
