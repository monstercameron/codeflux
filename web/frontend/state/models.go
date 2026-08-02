// Package state owns immutable frontend view models and pure state transitions.
//
// Durable coordinator state enters through RemoteAction. UIAction is deliberately
// limited to browser-local presentation state and cannot create task transitions.
package state

import (
	"errors"
	"slices"
	"strings"
)

// DataState makes every async presentation state explicit.
type DataState string

const (
	DataNotRequested     DataState = "not-requested"
	DataLoading          DataState = "loading"
	DataReadyEmpty       DataState = "ready-empty"
	DataReady            DataState = "ready"
	DataPartialStale     DataState = "partial-stale"
	DataRecoverableError DataState = "recoverable-error"
	DataDenied           DataState = "denied"
	DataIncompatible     DataState = "incompatible"
	DataDisconnected     DataState = "disconnected"
)

// BootstrapState is the root application's finite state machine.
type BootstrapState string

const (
	BootstrapBooting                BootstrapState = "booting"
	BootstrapReady                  BootstrapState = "ready"
	BootstrapIncompatible           BootstrapState = "incompatible"
	BootstrapUnauthorized           BootstrapState = "unauthorized"
	BootstrapCoordinatorUnavailable BootstrapState = "coordinator-unavailable"
	BootstrapDatabaseUnavailable    BootstrapState = "database-unavailable"
)

// ConnectionState is the session-stream presentation state.
type ConnectionState string

const (
	ConnectionConnecting ConnectionState = "connecting"
	ConnectionLive       ConnectionState = "live"
	ConnectionReplaying  ConnectionState = "replaying"
	ConnectionDegraded   ConnectionState = "degraded"
	// ConnectionIdle is a reachable coordinator with nothing selected to
	// follow. It exists because reporting that state as Disconnected sent
	// people hunting a transport fault that was not there: the bridge was up,
	// the database was ready, and no thread was open.
	ConnectionIdle         ConnectionState = "idle"
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionIncompatible ConnectionState = "incompatible"
	ConnectionUnauthorized ConnectionState = "unauthorized"
)

// ViewportClass controls responsive shell composition without changing routes.
type ViewportClass string

const (
	ViewportWide    ViewportClass = "wide"
	ViewportMedium  ViewportClass = "standard"
	ViewportNarrow  ViewportClass = "compact"
	ViewportMinimum ViewportClass = "minimum"
)

// Pane identifies the primary narrow-screen workspace surface.
type Pane string

const (
	PaneConversation Pane = "conversation"
	PaneGraph        Pane = "graph"
	PaneThreads      Pane = "threads"
)

// MessageView is presentation-only message data.
type MessageView struct {
	ID       string
	Role     string
	Body     string
	Pending  bool
	Failed   bool
	Sequence uint64
}

// ThreadView is one immutable thread-rail row.
type ThreadView struct {
	ID       string
	Title    string
	Status   string
	Archived bool
	Unread   int
}

// GraphNodeView is one task-graph node.
type GraphNodeView struct {
	ID       string
	Title    string
	Status   string
	Selected bool
}

// WorkspaceView holds only repository presentation data.
type WorkspaceView struct {
	RepositoryID   string
	RepositoryName string
	Branch         string
	Dirty          bool
}

// SessionView holds connection and compatibility presentation data.
type SessionView struct {
	Bootstrap  BootstrapState
	Connection ConnectionState
	Message    string
}

// LayoutPreferences are the only layout values suitable for browser persistence.
// They contain no credentials, message bodies, repository paths, or durable state.
type LayoutPreferences struct {
	RailCollapsed  bool
	RailWidth      int
	GraphCollapsed bool
	GraphWidth     int
	SplitPercent   int
	Viewport       ViewportClass
	ActivePane     Pane
}

// DefaultLayoutPreferences returns safe bounded defaults.
func DefaultLayoutPreferences() LayoutPreferences {
	return LayoutPreferences{
		RailWidth:    240,
		GraphWidth:   420,
		SplitPercent: 62,
		Viewport:     ViewportWide,
		ActivePane:   PaneConversation,
	}
}

// Normalize clamps restored preferences and repairs unknown enum values.
func (p LayoutPreferences) Normalize() LayoutPreferences {
	if p.RailWidth < 224 {
		p.RailWidth = 224
	}
	if p.RailWidth > 480 {
		p.RailWidth = 480
	}
	if p.GraphWidth < 320 {
		p.GraphWidth = 320
	}
	if p.GraphWidth > 720 {
		p.GraphWidth = 720
	}
	if p.SplitPercent < 35 {
		p.SplitPercent = 35
	}
	if p.SplitPercent > 75 {
		p.SplitPercent = 75
	}
	switch p.Viewport {
	case ViewportWide, ViewportMedium, ViewportNarrow, ViewportMinimum:
	default:
		p.Viewport = ViewportWide
	}
	switch p.ActivePane {
	case PaneConversation, PaneGraph, PaneThreads:
	default:
		p.ActivePane = PaneConversation
	}
	return p
}

// Snapshot is an immutable aggregate passed to top-level render boundaries.
// Slice fields are private so callers cannot mutate state through a returned view.
type Snapshot struct {
	Session           SessionView
	Workspace         WorkspaceView
	TopBar            TopBarView
	Review            ReviewView
	Settings          SettingsView
	Memory            MemoryView
	Diagnostics       DiagnosticsView
	FirstRun          FirstRunView
	Layout            LayoutPreferences
	ConversationState DataState
	GraphState        DataState
	ThreadsState      DataState
	SelectedThreadID  string
	SelectedGraphID   string
	CostLabel         string
	topBarRevision    uint64
	threadRevision    uint64
	conversationRev   uint64
	graphRevision     uint64
	threads           []ThreadView
	messages          []MessageView
	graphNodes        []GraphNodeView
}

func (s Snapshot) Threads() []ThreadView       { return slices.Clone(s.threads) }
func (s Snapshot) Messages() []MessageView     { return slices.Clone(s.messages) }
func (s Snapshot) GraphNodes() []GraphNodeView { return slices.Clone(s.graphNodes) }
func (s Snapshot) TopBarRevision() uint64      { return s.topBarRevision }
func (s Snapshot) ThreadRevision() uint64      { return s.threadRevision }
func (s Snapshot) ConversationRevision() uint64 {
	return s.conversationRev
}
func (s Snapshot) GraphRevision() uint64 { return s.graphRevision }

// NewSnapshot constructs an immutable snapshot and clones every caller-owned slice.
func NewSnapshot(threads []ThreadView, messages []MessageView, graph []GraphNodeView) Snapshot {
	return Snapshot{
		Session:           SessionView{Bootstrap: BootstrapBooting, Connection: ConnectionConnecting},
		Layout:            DefaultLayoutPreferences(),
		ConversationState: DataNotRequested,
		GraphState:        DataNotRequested,
		ThreadsState:      DataNotRequested,
		threads:           slices.Clone(threads),
		messages:          slices.Clone(messages),
		graphNodes:        slices.Clone(graph),
	}
}

// Store is a pure value store. Its zero value is usable after NewStore.
type Store struct{ snapshot Snapshot }

func NewStore(initial Snapshot) Store { return Store{snapshot: cloneSnapshot(initial)} }
func (s Store) Snapshot() Snapshot    { return cloneSnapshot(s.snapshot) }

func cloneSnapshot(source Snapshot) Snapshot {
	copy := source
	copy.FirstRun = source.FirstRun.clone()
	copy.threads = slices.Clone(source.threads)
	copy.messages = slices.Clone(source.messages)
	copy.graphNodes = slices.Clone(source.graphNodes)
	return copy
}

// RemoteAction is coordinator-authoritative input.
type RemoteAction interface{ remoteAction() }

type SessionChanged struct{ Session SessionView }
type WorkspaceChanged struct{ Workspace WorkspaceView }
type TopBarChanged struct{ TopBar TopBarView }
type ThreadsReplaced struct {
	State   DataState
	Threads []ThreadView
}
type MessagesAppended struct {
	State    DataState
	Messages []MessageView
}
type GraphReplaced struct {
	State DataState
	Nodes []GraphNodeView
}
type ReviewChanged struct{ Review ReviewView }
type SettingsChanged struct{ Settings SettingsView }
type MemoryChanged struct{ Memory MemoryView }
type DiagnosticsChanged struct{ Diagnostics DiagnosticsView }
type FirstRunChanged struct{ FirstRun FirstRunView }
type CostChanged struct{ Label string }

func (SessionChanged) remoteAction()     {}
func (WorkspaceChanged) remoteAction()   {}
func (TopBarChanged) remoteAction()      {}
func (ThreadsReplaced) remoteAction()    {}
func (MessagesAppended) remoteAction()   {}
func (GraphReplaced) remoteAction()      {}
func (ReviewChanged) remoteAction()      {}
func (SettingsChanged) remoteAction()    {}
func (MemoryChanged) remoteAction()      {}
func (DiagnosticsChanged) remoteAction() {}
func (FirstRunChanged) remoteAction()    {}
func (CostChanged) remoteAction()        {}

// ReduceRemote returns a new store and never mutates the receiver.
func (s Store) ReduceRemote(action RemoteAction) Store {
	next := cloneSnapshot(s.snapshot)
	switch action := action.(type) {
	case SessionChanged:
		next.Session = action.Session
		next.TopBar.Connection = action.Session.Connection
		next.topBarRevision++
	case WorkspaceChanged:
		next.Workspace = action.Workspace
		next.TopBar.Repository = action.Workspace.RepositoryName
		next.TopBar.Branch = action.Workspace.Branch
		if action.Workspace.Dirty {
			next.TopBar.WorktreeStatus = "uncommitted changes"
		} else if action.Workspace.Branch != "" {
			next.TopBar.WorktreeStatus = "clean"
		}
		next.topBarRevision++
	case TopBarChanged:
		next.TopBar = action.TopBar
		next.topBarRevision++
	case ThreadsReplaced:
		next.ThreadsState = action.State
		next.threads = slices.Clone(action.Threads)
		next.threadRevision++
	case MessagesAppended:
		next.ConversationState = action.State
		next.messages = append(slices.Clone(next.messages), action.Messages...)
		next.conversationRev++
	case GraphReplaced:
		next.GraphState = action.State
		next.graphNodes = slices.Clone(action.Nodes)
		next.graphRevision++
	case ReviewChanged:
		next.Review = action.Review
	case SettingsChanged:
		next.Settings = action.Settings
	case MemoryChanged:
		next.Memory = action.Memory
	case DiagnosticsChanged:
		next.Diagnostics = action.Diagnostics
	case FirstRunChanged:
		next.FirstRun = action.FirstRun.clone()
	case CostChanged:
		next.CostLabel = strings.TrimSpace(action.Label)
		next.TopBar.ActualCost = next.CostLabel
		next.topBarRevision++
	}
	return Store{snapshot: next}
}

// UIAction is browser-ephemeral input. Durable task transitions are intentionally
// not members of this closed action family.
type UIAction interface{ uiAction() }

type LayoutChanged struct{ Preferences LayoutPreferences }
type RailCollapsed struct{ Collapsed bool }
type RailWidthChanged struct{ Width int }
type GraphCollapsed struct{ Collapsed bool }
type SplitChanged struct{ Percent int }
type ThreadSelected struct{ ThreadID string }
type GraphNodeSelected struct{ NodeID string }

func (LayoutChanged) uiAction()     {}
func (RailCollapsed) uiAction()     {}
func (RailWidthChanged) uiAction()  {}
func (GraphCollapsed) uiAction()    {}
func (SplitChanged) uiAction()      {}
func (ThreadSelected) uiAction()    {}
func (GraphNodeSelected) uiAction() {}

var ErrInvalidUIAction = errors.New("frontend state: invalid UI action")

// ReduceUI applies presentation-only state. Unknown/nil actions are rejected.
func (s Store) ReduceUI(action UIAction) (Store, error) {
	if action == nil {
		return s, ErrInvalidUIAction
	}
	next := cloneSnapshot(s.snapshot)
	switch action := action.(type) {
	case LayoutChanged:
		next.Layout = action.Preferences.Normalize()
	case RailCollapsed:
		next.Layout.RailCollapsed = action.Collapsed
	case RailWidthChanged:
		next.Layout.RailWidth = action.Width
		next.Layout = next.Layout.Normalize()
	case GraphCollapsed:
		next.Layout.GraphCollapsed = action.Collapsed
	case SplitChanged:
		next.Layout.SplitPercent = action.Percent
		next.Layout = next.Layout.Normalize()
	case ThreadSelected:
		next.SelectedThreadID = strings.TrimSpace(action.ThreadID)
		next.threadRevision++
		next.conversationRev++
	case GraphNodeSelected:
		next.SelectedGraphID = strings.TrimSpace(action.NodeID)
		next.graphRevision++
	default:
		return s, ErrInvalidUIAction
	}
	return Store{snapshot: next}, nil
}
