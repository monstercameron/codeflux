package state

import "slices"

// TopBarView is the complete shell chrome projection. Empty values deliberately
// render as placeholders while their authoritative owners are loading.
type TopBarView struct {
	Repository      string
	Branch          string
	WorktreeStatus  string
	TaskTitle       string
	TaskSummary     string
	TaskState       string
	Connection      ConnectionState
	Provider        string
	Model           string
	Effort          string
	ForecastCost    string
	ActualTokens    string
	ActualCost      string
	PricingSnapshot string
	HardBudget      string
	RemainingBudget string
	BudgetWarning   string
	// SpentFraction is how much of the hard cap has been spent, between 0 and
	// 1, or a negative value when either figure is unmeasured. The header draws
	// it as a meter, and a meter must not invent a zero for a measurement
	// nobody took.
	SpentFraction float64
	BudgetWarned  bool
	CanPause      bool
	CanStop       bool
}

type ReviewView struct {
	State      DataState
	Summary    string
	ApprovalID string
}

type SettingsView struct {
	State          DataState
	ProviderCount  int
	ModelCount     int
	PolicyRevision string
}

type MemoryView struct {
	State      DataState
	SelectedID string
	Entries    int
}

type DiagnosticsView struct {
	State                    DataState
	Health                   string
	AppVersion               string
	APIVersion               string
	Schema                   string
	Frontend                 string
	ActiveTasks              int
	LastAppliedSequence      uint64
	LastAppliedSequenceKnown bool
	SessionReplayActive      bool
	SessionLive              bool
	SessionGapRepairRequired bool
}

type FirstRunView struct {
	State       DataState
	CurrentStep string
	Completed   []string
}

func (v FirstRunView) clone() FirstRunView {
	v.Completed = slices.Clone(v.Completed)
	return v
}

// The named stores below make ownership reviewable at compile time. Remote
// stores expose replacement from transport projections; UIStore alone exposes
// ephemeral layout and draft mutation.
type SessionStore struct{ view SessionView }
type WorkspaceStore struct{ view WorkspaceView }
type ThreadStore struct{ threads []ThreadView }
type TaskStore struct{ topBar TopBarView }
type GraphStore struct{ nodes []GraphNodeView }
type ReviewStore struct{ view ReviewView }
type SettingsStore struct{ view SettingsView }
type MemoryStore struct{ view MemoryView }
type DiagnosticsStore struct{ view DiagnosticsView }
type FirstRunStore struct{ view FirstRunView }

func NewSessionStore(view SessionView) SessionStore       { return SessionStore{view: view} }
func NewWorkspaceStore(view WorkspaceView) WorkspaceStore { return WorkspaceStore{view: view} }
func NewThreadStore(threads []ThreadView) ThreadStore {
	return ThreadStore{threads: slices.Clone(threads)}
}
func NewTaskStore(view TopBarView) TaskStore           { return TaskStore{topBar: view} }
func NewGraphStore(nodes []GraphNodeView) GraphStore   { return GraphStore{nodes: slices.Clone(nodes)} }
func NewReviewStore(view ReviewView) ReviewStore       { return ReviewStore{view: view} }
func NewSettingsStore(view SettingsView) SettingsStore { return SettingsStore{view: view} }
func NewMemoryStore(view MemoryView) MemoryStore       { return MemoryStore{view: view} }
func NewDiagnosticsStore(view DiagnosticsView) DiagnosticsStore {
	return DiagnosticsStore{view: view}
}
func NewFirstRunStore(view FirstRunView) FirstRunStore {
	return FirstRunStore{view: view.clone()}
}

func (s SessionStore) View() SessionView         { return s.view }
func (s WorkspaceStore) View() WorkspaceView     { return s.view }
func (s ThreadStore) Threads() []ThreadView      { return slices.Clone(s.threads) }
func (s TaskStore) TopBar() TopBarView           { return s.topBar }
func (s GraphStore) Nodes() []GraphNodeView      { return slices.Clone(s.nodes) }
func (s ReviewStore) View() ReviewView           { return s.view }
func (s SettingsStore) View() SettingsView       { return s.view }
func (s MemoryStore) View() MemoryView           { return s.view }
func (s DiagnosticsStore) View() DiagnosticsView { return s.view }
func (s FirstRunStore) View() FirstRunView       { return s.view.clone() }

type UIStore struct {
	layout LayoutPreferences
	drafts map[string]string
}

func NewUIStore(layout LayoutPreferences, drafts map[string]string) UIStore {
	return UIStore{layout: layout.Normalize(), drafts: cloneMap(drafts)}
}

func (s UIStore) Layout() LayoutPreferences { return s.layout }
func (s UIStore) Draft(threadID string) string {
	return s.drafts[threadID]
}
func (s UIStore) WithLayout(layout LayoutPreferences) UIStore {
	s.layout = layout.Normalize()
	s.drafts = cloneMap(s.drafts)
	return s
}
func (s UIStore) WithDraft(threadID, draft string) UIStore {
	s.drafts = cloneMap(s.drafts)
	if draft == "" {
		delete(s.drafts, threadID)
	} else {
		s.drafts[threadID] = draft
	}
	return s
}

func cloneMap(source map[string]string) map[string]string {
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
