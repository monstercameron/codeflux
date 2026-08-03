//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/graphcanvas"
	"codeflux.dev/codeflux/web/frontend/graphinspector"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const mountedGraphTimeout = 5 * time.Second

type mountedGraphSource struct {
	Authoritative *graphcanvas.AuthoritativeProps
	Inspector     ui.Node
	Reload        func()
}

func useMountedGraph(
	thread threadrail.Thread,
	taskState domain.TaskState,
	projectionGraphRevision uint64,
	visualMode primitives.Mode,
	onSeedChat func(string),
	onHighlightMessages func([]domain.MessageID),
) mountedGraphSource {
	scope, scopeErr := selectedGraphResourceScope(thread)
	requestedMode := ui.UseState(graphcanvas.DefaultGraphMode(taskState))
	modeUserSelected := ui.UseState(false)
	selectedNode := ui.UseState(domain.NodeID{})
	selectedDetail := ui.UseState(graphNodeResource{})
	detailReady := ui.UseState(false)
	detailLoading := ui.UseState(false)
	actionBusy := ui.UseState(false)
	actionStatus := ui.UseState("")
	override := ui.UseState(graphResource{})
	overridePresent := ui.UseState(false)
	searchQuery := ui.UseState("")
	searchResults := ui.UseState([]taskgraph.Node{})
	searchLoading := ui.UseState(false)
	searchStatus := ui.UseState("")
	searchFocusNodeID := ui.UseState(domain.NodeID{})

	dependency := "unavailable"
	if scopeErr == nil {
		dependency = scope.ProjectID.String() + "|" + scope.TaskID.String() + "|" +
			string(requestedMode.Get()) + "|" + strconv.FormatUint(projectionGraphRevision, 10)
	}
	resource := fetch.UseResource(func(parent context.Context) (graphResource, error) {
		if scopeErr != nil {
			return graphResource{}, scopeErr
		}
		ctx, cancel := context.WithTimeout(parent, mountedGraphTimeout)
		defer cancel()
		return loadGraphResource(ctx, openBrowserGraphResourceClient, scope, requestedMode.Get(), 300, 600, "")
	}, dependency)
	state := resource.Get()
	if !state.Ready {
		return mountedGraphSource{
			Inspector: mountedGraphResourceState(state, visualMode),
			Reload:    resource.Reload,
		}
	}

	current := state.Value
	if overridePresent.Get() {
		candidate := override.Get()
		if candidate.TaskID == current.TaskID && candidate.Revision.Metadata().ID() == current.Revision.Metadata().ID() {
			current = candidate
		}
	}
	loadDetail := func(nodeID domain.NodeID) {
		if nodeID.IsZero() || detailLoading.Get() {
			return
		}
		selectedNode.Set(nodeID)
		detailReady.Set(false)
		detailLoading.Set(true)
		actionStatus.Set("Loading selected node details.")
		ui.SafeGo("load authoritative graph node", func() {
			ctx, cancel := context.WithTimeout(context.Background(), mountedGraphTimeout)
			detail, err := loadGraphNodeResource(ctx, openBrowserGraphResourceClient, scope, current.Revision.Metadata().ID(), nodeID)
			cancel()
			ui.PostAsync(func() {
				detailLoading.Set(false)
				if err != nil {
					actionStatus.Set("Selected node details could not be loaded.")
					return
				}
				if selectedNode.Get() != nodeID {
					return
				}
				selectedDetail.Set(detail)
				detailReady.Set(true)
				actionStatus.Set("Selected node details are current.")
				if onHighlightMessages != nil {
					onHighlightMessages(detail.MessageIDs)
				}
			})
		})
	}
	// runSearch asks the authoritative SearchGraph RPC for the nodes a query
	// names. It searches the server's graph, not only this view's currently
	// loaded slice, so a result can name a node the canvas has never drawn.
	runSearch := func(query string) {
		searchQuery.Set(query)
		trimmed := strings.TrimSpace(query)
		if trimmed == "" {
			searchResults.Set(nil)
			searchStatus.Set("")
			searchLoading.Set(false)
			return
		}
		searchLoading.Set(true)
		searchStatus.Set("")
		revisionID := current.Revision.Metadata().ID()
		ui.SafeGo("search authoritative graph", func() {
			ctx, cancel := context.WithTimeout(context.Background(), mountedGraphTimeout)
			result, err := searchGraphResource(ctx, openBrowserGraphResourceClient, scope, revisionID, requestedMode.Get(), trimmed, 20, "")
			cancel()
			ui.PostAsync(func() {
				if searchQuery.Get() != query {
					return // a later keystroke superseded this response
				}
				searchLoading.Set(false)
				if err != nil {
					searchResults.Set(nil)
					searchStatus.Set("The graph search could not be completed.")
					return
				}
				nodes := result.Revision.Nodes()
				searchResults.Set(nodes)
				if len(nodes) == 0 {
					searchStatus.Set(fmt.Sprintf("No graph node matches %q.", trimmed))
				} else {
					searchStatus.Set("")
				}
			})
		})
	}
	// selectSearchResult brings a chosen match into focus when it is already
	// part of the currently loaded bounded slice. It cannot place a match the
	// loaded Layout has never drawn, so it says that plainly instead of doing
	// nothing.
	selectSearchResult := func(nodeID domain.NodeID) {
		if !mountedGraphHasPlacement(current.Layout, nodeID) {
			searchStatus.Set("That match is not in the currently loaded graph view. Try Fit, or expand from a nearby node, then search again.")
			return
		}
		searchFocusNodeID.Set(nodeID)
	}
	runExpansion := func(nodeID domain.NodeID, traversal string, hops int) {
		if actionBusy.Get() {
			return
		}
		actionBusy.Set(true)
		actionStatus.Set("Updating the bounded graph view.")
		ui.SafeGo("expand authoritative graph", func() {
			ctx, cancel := context.WithTimeout(context.Background(), mountedGraphTimeout)
			next, err := expandGraphResource(ctx, openBrowserGraphResourceClient, scope, current.Revision.Metadata().ID(), []domain.NodeID{nodeID}, requestedMode.Get(), traversal, hops, 300, 600, "")
			cancel()
			ui.PostAsync(func() {
				actionBusy.Set(false)
				if err != nil {
					actionStatus.Set("The graph exploration request was not confirmed.")
					return
				}
				override.Set(next)
				overridePresent.Set(true)
				actionStatus.Set("Graph view updated from the authoritative revision.")
			})
		})
	}
	parentRevision, comparisonAvailable := current.Revision.Metadata().ParentID()
	compare := func(domain.GraphRevisionID) {
		if !comparisonAvailable || actionBusy.Get() {
			return
		}
		actionBusy.Set(true)
		actionStatus.Set("Comparing graph revisions.")
		ui.SafeGo("compare authoritative graph revisions", func() {
			ctx, cancel := context.WithTimeout(context.Background(), mountedGraphTimeout)
			comparison, err := compareGraphResources(ctx, openBrowserGraphResourceClient, scope, parentRevision, current.Revision.Metadata().ID(), 300, "")
			cancel()
			ui.PostAsync(func() {
				actionBusy.Set(false)
				if err != nil {
					actionStatus.Set("Graph revision comparison could not be loaded.")
					return
				}
				actionStatus.Set(fmt.Sprintf("Graph comparison loaded: %d bounded change(s).", len(comparison.Changes)))
			})
		})
	}
	explain := func(nodeID domain.NodeID) {
		if actionBusy.Get() {
			return
		}
		actionBusy.Set(true)
		actionStatus.Set("Preparing a bounded node explanation.")
		ui.SafeGo("explain authoritative graph node", func() {
			ctx, cancel := context.WithTimeout(context.Background(), mountedGraphTimeout)
			explanation, err := explainGraphNodeResource(ctx, openBrowserGraphResourceClient, scope, current.Revision.Metadata().ID(), nodeID)
			cancel()
			ui.PostAsync(func() {
				actionBusy.Set(false)
				if err != nil {
					actionStatus.Set("The node explanation could not be loaded.")
					return
				}
				selectedDetail.Set(explanation.Node)
				detailReady.Set(true)
				actionStatus.Set("Node explanation added to the local chat draft.")
				if onSeedChat != nil {
					onSeedChat(explanation.Explanation)
				}
			})
		})
	}

	authoritative := graphcanvas.AuthoritativeProps{
		Revision: current.Revision, Layout: current.Layout, TaskState: taskState,
		Mode: requestedMode.Get(), ModeUserSelected: modeUserSelected.Get(),
		SelectedNodeID: selectedNode.Get(), CurrentNodeID: mountedGraphCurrentNode(current),
		FocusNodeID:         searchFocusNodeID.Get(),
		ComparisonAvailable: comparisonAvailable,
		OnSelectNode:        loadDetail,
		OnModeChange: func(mode taskgraph.Mode) {
			if !mode.IsValid() {
				return
			}
			modeUserSelected.Set(true)
			requestedMode.Set(mode)
			overridePresent.Set(false)
		},
		OnCompareRevision:    func() { compare(current.Revision.Metadata().ID()) },
		SearchQuery:          searchQuery.Get(),
		SearchResults:        searchResults.Get(),
		SearchLoading:        searchLoading.Get(),
		SearchStatus:         searchStatus.Get(),
		OnSearchQueryChange:  runSearch,
		OnSearchResultSelect: selectSearchResult,
	}
	inspector := mountedGraphInspector(
		selectedNode.Get(), selectedDetail.Get(), detailReady.Get(), detailLoading.Get(),
		current.Revision.Metadata().ID(), visualMode, actionStatus.Get(), graphInspectorResourceActions{
			Mode: visualMode, ComparisonAvailable: comparisonAvailable,
			OnExplainInChat:   explain,
			OnExpandNeighbors: func(nodeID domain.NodeID) { runExpansion(nodeID, "neighbors", 1) },
			OnDependencyCone:  func(nodeID domain.NodeID) { runExpansion(nodeID, "dependencies", 8) },
			OnEvidenceCone:    func(nodeID domain.NodeID) { runExpansion(nodeID, "evidence", 8) },
			OnCompareRevision: compare,
		},
	)
	return mountedGraphSource{Authoritative: &authoritative, Inspector: inspector, Reload: resource.Reload}
}

func mountedGraphResourceState(state fetch.ResourceState[graphResource], mode primitives.Mode) ui.Node {
	switch {
	case state.Loading:
		return html.P(html.Props{Role: "status", Text: "Loading the authoritative project graph."})
	case state.Error != nil:
		return html.P(html.Props{Role: "status", Text: "The authoritative project graph is not available yet."})
	default:
		return html.P(html.Props{Role: "status", Text: "Select a task with an authoritative graph."})
	}
}

func mountedGraphInspector(
	selected domain.NodeID,
	detail graphNodeResource,
	ready, loading bool,
	revisionID domain.GraphRevisionID,
	mode primitives.Mode,
	status string,
	actions graphInspectorResourceActions,
) ui.Node {
	children := make([]ui.Node, 0, 2)
	switch {
	case loading:
		children = append(children, html.P(html.Props{Role: "status", Text: "Loading selected node details."}))
	case selected.IsZero():
		children = append(children, html.P(html.Props{Role: "status", Text: "Select a graph node to inspect its evidence, attribution, and source links."}))
	case ready:
		props, err := graphInspectorProps(detail, revisionID, actions)
		if err != nil {
			children = append(children, html.P(html.Props{Role: "status", Text: "The selected node detail is malformed and was not rendered."}))
		} else {
			children = append(children, ui.CreateElement(graphinspector.ContextInspector, props))
		}
	default:
		children = append(children, html.P(html.Props{Role: "status", Text: "Selected node details are not available."}))
	}
	if strings.TrimSpace(status) != "" {
		children = append(children, html.P(html.Props{
			Role: "status", Aria: map[string]string{"live": "polite"},
			Data: map[string]string{"component": "graph-action-status"}, Text: status,
		}))
	}
	return html.Div(html.Props{Data: map[string]string{"component": "mounted-graph-inspector"}}, children...)
}

func mountedGraphCurrentNode(resource graphResource) domain.NodeID {
	nodes := resource.Revision.Nodes()
	for _, node := range nodes {
		if node.Status() == taskgraph.NodeStatusActive {
			return node.ID()
		}
	}
	if len(nodes) > 0 {
		return nodes[0].ID()
	}
	return domain.NodeID{}
}
