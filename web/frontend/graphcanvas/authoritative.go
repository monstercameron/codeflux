package graphcanvas

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/graphlayout"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type AuthoritativeProps struct {
	Revision            graph.Revision
	Layout              graphlayout.Layout
	TaskState           domain.TaskState
	Mode                graph.Mode
	ModeUserSelected    bool
	SelectedNodeID      domain.NodeID
	CurrentNodeID       domain.NodeID
	LinkedChatNodeID    domain.NodeID
	ComparisonAvailable bool
	ResponsiveMode      string
	VisualMode          primitives.Mode
	Height              int
	OnSelectNode        func(domain.NodeID)
	OnHighlightMessages func(domain.NodeID)
	OnModeChange        func(graph.Mode)
	OnViewportChange    func(Viewport)
	OnCompareRevision   func()
}

func validateAuthoritativeProps(props AuthoritativeProps) error {
	if err := props.Revision.Validate(); err != nil {
		return err
	}
	if props.Layout.AlgorithmVersion != graphlayout.AlgorithmVersion {
		return fmt.Errorf("unsupported graph layout algorithm %q", props.Layout.AlgorithmVersion)
	}
	nodes := props.Revision.Nodes()
	if len(nodes) != len(props.Layout.Nodes) {
		return errors.New("graph revision and layout node counts differ")
	}
	known := make(map[domain.NodeID]struct{}, len(nodes))
	for _, node := range nodes {
		known[node.ID()] = struct{}{}
	}
	for _, placement := range props.Layout.Nodes {
		if _, ok := known[placement.NodeID]; !ok || placement.Bounds.Width <= 0 || placement.Bounds.Height <= 0 {
			return errors.New("graph layout contains an invalid node placement")
		}
		delete(known, placement.NodeID)
	}
	if len(known) != 0 || props.Layout.Bounds.Width <= 0 || props.Layout.Bounds.Height <= 0 {
		return errors.New("graph layout is incomplete")
	}
	return nil
}

func authoritativeRenderer(props AuthoritativeProps) ui.Node {
	if err := validateAuthoritativeProps(props); err != nil {
		return graphError(props.VisualMode, err)
	}
	return ui.CreateElement(authoritativeSVG, props)
}

func authoritativeSVG(props AuthoritativeProps) ui.Node {
	height := props.Height
	if height <= 0 {
		height = 520
	}
	viewportRef := ui.UseDOMRef()
	geometry := ui.UseElementGeometry(viewportRef)
	hasMeasuredViewport := geometry.Width > 0 && geometry.Height > 0
	width := geometry.Width
	if width <= 0 {
		width = 1024
	}
	measuredHeight := geometry.Height
	if measuredHeight <= 0 {
		measuredHeight = float64(height)
	}
	initialMode := InitialGraphMode(props.Mode, props.ModeUserSelected, props.TaskState)
	mode := ui.UseState(initialMode)
	modeChosen := ui.UseState(props.ModeUserSelected)
	selected := ui.UseState(initialAuthoritativeSelection(props))
	viewport := ui.UseState(fitAuthoritativeViewport(props.Layout.Bounds, width, measuredHeight))
	initialMeasuredFitApplied := ui.UseRef(false)
	drag := ui.UseRef(dragState{})
	focused := ui.UseState(false)

	setViewport := func(next Viewport) {
		next = next.Normalize()
		viewport.Set(next)
		if props.OnViewportChange != nil {
			props.OnViewportChange(next)
		}
	}
	ui.UseEffect(func() func() {
		if hasMeasuredViewport && !initialMeasuredFitApplied.Get() {
			initialMeasuredFitApplied.Set(true)
			setViewport(fitAuthoritativeViewport(props.Layout.Bounds, width, measuredHeight))
		}
		return nil
	}, strconv.FormatFloat(width, 'f', 3, 64)+"|"+strconv.FormatFloat(measuredHeight, 'f', 3, 64))
	selectNode := func(nodeID domain.NodeID, moveFocus bool) {
		if !hasAuthoritativePlacement(props.Layout, nodeID) {
			return
		}
		selected.Set(nodeID)
		if props.OnSelectNode != nil {
			props.OnSelectNode(nodeID)
		}
		if props.OnHighlightMessages != nil {
			props.OnHighlightMessages(nodeID)
		}
		if moveFocus {
			focusGraphNode(nodeID.String())
		}
	}
	ui.UseEffect(func() func() {
		if !props.SelectedNodeID.IsZero() && props.SelectedNodeID != selected.Get() &&
			hasAuthoritativePlacement(props.Layout, props.SelectedNodeID) {
			selected.Set(props.SelectedNodeID)
		}
		return nil
	}, props.SelectedNodeID.String())
	ui.UseEffect(func() func() {
		if !selected.Get().IsZero() && !hasAuthoritativePlacement(props.Layout, selected.Get()) {
			selected.Set(domain.NodeID{})
		}
		return nil
	}, props.Revision.Metadata().ID().String()+"|"+string(props.TaskState))
	ui.UseEffect(func() func() {
		next := SynchronizeAuthoritativeMode(AuthoritativeInteraction{
			Mode: mode.Get(), UserSelectedMode: modeChosen.Get(),
		}, props.Mode, props.ModeUserSelected, props.TaskState)
		modeChosen.Set(next.UserSelectedMode)
		if next.Mode != mode.Get() {
			mode.Set(next.Mode)
		}
		return nil
	}, strconv.FormatBool(props.ModeUserSelected)+"|"+string(props.Mode)+"|"+string(props.TaskState))
	ui.UseEffect(func() func() {
		linked := props.LinkedChatNodeID
		if linked.IsZero() || !hasAuthoritativePlacement(props.Layout, linked) {
			return nil
		}
		selected.Set(linked)
		placement, _ := authoritativePlacement(props.Layout, linked)
		setViewport(centerAuthoritativePlacement(viewport.Get(), placement, width, measuredHeight))
		if props.OnSelectNode != nil {
			props.OnSelectNode(linked)
		}
		if props.OnHighlightMessages != nil {
			props.OnHighlightMessages(linked)
		}
		ui.PostAsync(func() { focusGraphNode(linked.String()) })
		return nil
	}, props.LinkedChatNodeID.String()+"|"+props.Revision.Metadata().ID().String()+"|"+authoritativeLayoutIdentity(props.Layout))

	startPan := func(event ui.Event) {
		if pointerButton(event) != 0 {
			return
		}
		capturePointer(event)
		focused.Set(true)
		x, y := pointerClientPosition(event)
		current := viewport.Get().Normalize()
		drag.Set(dragState{Active: true, StartX: x, StartY: y, StartPanX: current.PanX, StartPanY: current.PanY})
	}
	movePan := func(event ui.Event) {
		currentDrag := drag.Get()
		if !currentDrag.Active {
			return
		}
		x, y := pointerClientPosition(event)
		currentDrag.Moved = currentDrag.Moved || math.Abs(x-currentDrag.StartX) > 3 || math.Abs(y-currentDrag.StartY) > 3
		drag.Set(currentDrag)
		setViewport(Viewport{
			Zoom: viewport.Get().Normalize().Zoom,
			PanX: currentDrag.StartPanX + x - currentDrag.StartX,
			PanY: currentDrag.StartPanY + y - currentDrag.StartY,
		})
	}
	endPan := func(event ui.Event) {
		releasePointer(event)
		currentDrag := drag.Get()
		currentDrag.Active = false
		drag.Set(currentDrag)
	}
	wheelHandler := func(event ui.Event) {
		if !focused.Get() {
			return
		}
		delta := wheelDeltaY(event)
		if delta == 0 {
			return
		}
		event.PreventDefault()
		x, y := pointerOffsetPosition(event)
		setViewport(ZoomViewportAt(viewport.Get(), math.Exp(-delta*0.0012), Point{X: x, Y: y}))
	}

	svgRef := ui.UseDOMRef()
	ui.UseWheel(svgRef, wheelHandler)
	tokens := props.VisualMode.Tokens()
	currentViewport := viewport.Get().Normalize()
	modeValue := mode.Get()
	if !modeValue.IsValid() {
		modeValue = initialMode
	}
	modeTabs := primitives.Tabs(primitives.TabsProps{
		Label: "Graph mode",
		Items: []primitives.TabItem{
			{ID: string(graph.ModeProgram), Label: "Program"},
			{ID: string(graph.ModeExecution), Label: "Execution"},
			{ID: string(graph.ModeEvidence), Label: "Evidence"},
		},
		SelectedID: string(modeValue), Mode: props.VisualMode,
		OnSelect: func(value string) {
			nextInteraction, ok := SelectAuthoritativeMode(AuthoritativeInteraction{
				Viewport: viewport.Get(), SelectedNodeID: selected.Get(), Mode: mode.Get(),
				UserSelectedMode: modeChosen.Get(), RevisionID: props.Revision.Metadata().ID(),
			}, graph.Mode(value))
			if !ok {
				return
			}
			modeChosen.Set(true)
			mode.Set(nextInteraction.Mode)
			if props.OnModeChange != nil {
				props.OnModeChange(nextInteraction.Mode)
			}
		},
	})

	sectionProps := html.Props{}
	sectionProps.Aria = map[string]string{"label": "Project graph"}
	sectionProps.Data = map[string]string{
		"component": "authoritative-task-graph", "renderer": "accessible-svg",
		"layout-algorithm":     props.Layout.AlgorithmVersion,
		"graph-revision":       props.Revision.Metadata().ID().String(),
		"responsive-mode":      props.ResponsiveMode,
		"high-contrast":        strconv.FormatBool(props.VisualMode.HighContrast),
		"viewport-measured":    strconv.FormatBool(hasMeasuredViewport),
		"measured-fit-applied": strconv.FormatBool(initialMeasuredFitApplied.Get()),
		"graph-mode":           string(modeValue), "selected-node-id": selected.Get().String(),
		"zoom":  strconv.FormatFloat(currentViewport.Zoom, 'f', 6, 64),
		"pan-x": strconv.FormatFloat(currentViewport.PanX, 'f', 3, 64),
		"pan-y": strconv.FormatFloat(currentViewport.PanY, 'f', 3, 64),
	}
	sectionProps.Class = authoritativeContainerClass(tokens, height)

	viewportProps := html.PropsOf(html.Ref(viewportRef))
	viewportProps.Class = authoritativeViewportWrapClass()
	viewportProps.Data = map[string]string{"component": "graph-viewport"}
	graphViewport := html.Div(viewportProps,
		authoritativeSVGRoot(svgRef, props, currentViewport, selected.Get(), width, measuredHeight, drag, startPan, movePan, endPan, func(event ui.Event) {
			focused.Set(true)
		}, func(event ui.Event) {
			focused.Set(false)
			drag.Set(dragState{})
		}, selectNode),
		authoritativeToolbar(props, currentViewport, width, measuredHeight, setViewport, selected.Get(), selectNode),
	)

	return html.Section(sectionProps,
		html.Header(html.Props{Class: authoritativeHeaderClass(tokens, props.ResponsiveMode)},
			html.Div(html.Props{},
				html.H2(html.Props{
					Class: graphPanelTitleClass(tokens),
					Text:  "Task graph",
				}),
				html.P(html.Props{
					Class: graphPanelDescriptionClass(tokens),
					Text:  graphModeDescription(modeValue),
				}),
			),
			modeTabs,
		),
		authoritativeModePanels(modeValue, graphViewport),
		authoritativeLegend(props.VisualMode, props.ResponsiveMode),
		selectedNodeAnnouncement(props.Revision, selected.Get()),
	)
}

func authoritativeSVGRoot(
	ref ui.DOMRef,
	props AuthoritativeProps,
	viewport Viewport,
	selectedID domain.NodeID,
	width, height float64,
	drag ui.Ref[dragState],
	onPointerDown, onPointerMove, onPointerUp, onFocus, onBlur func(ui.Event),
	onSelect func(domain.NodeID, bool),
) ui.Node {
	nodes := props.Revision.Nodes()
	nodeByID := make(map[domain.NodeID]graph.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID()] = node
	}
	edgeLayer := authoritativeEdges(props.Revision.Edges(), props.Layout, props.VisualMode)
	nodeLayer := make([]ui.Node, 0, len(props.Layout.Nodes))
	ordered := append([]graphlayout.Placement(nil), props.Layout.Nodes...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Rank != ordered[j].Rank {
			return ordered[i].Rank < ordered[j].Rank
		}
		if ordered[i].Order != ordered[j].Order {
			return ordered[i].Order < ordered[j].Order
		}
		return ordered[i].NodeID.String() < ordered[j].NodeID.String()
	})
	for index, placement := range ordered {
		node := nodeByID[placement.NodeID]
		nodeID := node.ID()
		tabIndex := -1
		if nodeID == selectedID || selectedID.IsZero() && index == 0 {
			tabIndex = html.TabIndexZero
		}
		onClick := func(ui.Event) {
			if !shouldActivateAuthoritativeNodeAfterGesture(drag.Get()) {
				drag.Set(dragState{})
				return
			}
			drag.Set(dragState{})
			onSelect(nodeID, false)
		}
		onKeyDown := func(event ui.KeyboardEvent) {
			switch event.GetKey() {
			case "Enter", " ":
				event.PreventDefault()
				onSelect(nodeID, false)
			case "ArrowRight", "ArrowDown", "ArrowLeft", "ArrowUp", "Home", "End":
				event.PreventDefault()
				next := TraverseAuthoritativeNodes(props.Layout, nodeID, event.GetKey())
				onSelect(next, true)
			}
		}
		groupProps := html.PropsOf(html.OnClick(onClick), html.OnKeyDown(onKeyDown))
		groupProps.ID = graphNodeElementID(nodeID)
		groupProps.Key = nodeID.String()
		groupProps.Role = "button"
		groupProps.TabIndex = tabIndex
		// The <text> glyph authoritativeNodeContents draws for this node may
		// be truncated to fit its shape; the group's aria-label, the
		// full-label data hook below, and the sibling <title> tooltip always
		// carry the full DisplayName, independent of that visual clipping
		// (M21-165, M21-166). The stable node identity lives only in nodeID /
		// data-node-id, never in the label text.
		_, fullLabel, labelTruncated := TruncateLabel(node.DisplayName(), MaximumSVGNodeLabelRunes)
		groupProps.Aria = map[string]string{
			"label":   fullLabel + ", " + graphClassLabel(node.Class()) + ", " + graphStatusLabel(node.Status()),
			"pressed": strconv.FormatBool(nodeID == selectedID),
		}
		groupProps.Data = map[string]string{
			"component": "graph-node", "node-id": nodeID.String(), "class": string(node.Class()),
			"status": string(node.Status()), "shape": graphShape(node.Class()),
			"status-icon": graphStatusIcon(node.Status()), "status-label": graphStatusLabel(node.Status()),
			"selected":        strconv.FormatBool(nodeID == selectedID),
			"full-label":      fullLabel,
			"label-truncated": strconv.FormatBool(labelTruncated),
		}
		groupProps.Raw = map[string]any{
			"transform": fmt.Sprintf("translate(%d %d)", placement.Bounds.X, placement.Bounds.Y),
		}
		groupProps.Class = authoritativeNodeClass(props.VisualMode.Tokens())
		nodeLayer = append(nodeLayer, html.Tag("g", groupProps, authoritativeNodeContents(node, placement.Bounds, props.VisualMode, nodeID == selectedID)...))
	}
	world := html.Tag("g", html.Props{
		Data: map[string]string{"component": "graph-world"},
		Raw:  map[string]any{"transform": fmt.Sprintf("translate(%.3f %.3f) scale(%.6f)", viewport.PanX, viewport.PanY, viewport.Zoom)},
	}, append(edgeLayer, nodeLayer...)...)
	svgProps := html.PropsOf(html.Ref(ref), html.OnPointerDown(onPointerDown), html.OnPointerMove(onPointerMove), html.OnPointerUp(onPointerUp), html.OnFocus(onFocus), html.OnBlur(onBlur))
	svgProps.ID = "authoritative-graph-svg"
	svgProps.Role = "application"
	svgProps.TabIndex = html.TabIndexZero
	svgProps.Aria = map[string]string{
		"label": "Interactive task graph. Use arrow keys on graph nodes to traverse the visible graph.",
	}
	svgProps.Data = map[string]string{
		"component": "graph-svg-root", "keyboard": "arrows-home-end", "pointer-pan": "true", "wheel-zoom": "focused-only",
	}
	svgProps.Raw = map[string]any{
		"viewBox": fmt.Sprintf("0 0 %.3f %.3f", width, height), "preserveAspectRatio": "xMinYMin meet",
	}
	svgProps.Class = authoritativeSVGClass(props.VisualMode.Tokens())
	return html.Svg(svgProps, world)
}

func authoritativeEdges(edges []graph.Edge, layout graphlayout.Layout, mode primitives.Mode) []ui.Node {
	placements := make(map[domain.NodeID]graphlayout.Placement, len(layout.Nodes))
	for _, placement := range layout.Nodes {
		placements[placement.NodeID] = placement
	}
	ordered := append([]graph.Edge(nil), edges...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID().String() < ordered[j].ID().String() })
	tokens := mode.Tokens()
	result := make([]ui.Node, 0, len(ordered)*2)
	for _, edge := range ordered {
		from, fromOK := placements[edge.FromNode()]
		to, toOK := placements[edge.ToNode()]
		if !fromOK || !toOK {
			continue
		}
		startX := from.Bounds.X + from.Bounds.Width
		startY := from.Bounds.Y + from.Bounds.Height/2
		endX := to.Bounds.X
		endY := to.Bounds.Y + to.Bounds.Height/2
		bend := max(int64(48), (endX-startX)/2)
		stroke, dash, width := edgeVisual(edge.Class(), tokens)
		pathProps := html.Props{
			Key:  edge.ID().String(),
			Data: map[string]string{"component": "graph-edge", "edge-id": edge.ID().String(), "relationship": string(edge.Class())},
			Raw: map[string]any{
				"d":    fmt.Sprintf("M %d %d C %d %d, %d %d, %d %d", startX, startY, startX+bend, startY, endX-bend, endY, endX, endY),
				"fill": "none", "stroke": stroke, "stroke-width": strconv.Itoa(width),
				"stroke-dasharray": dash, "vector-effect": "non-scaling-stroke",
			},
		}
		arrow := fmt.Sprintf("%d,%d %d,%d %d,%d", endX, endY, endX-10, endY-6, endX-10, endY+6)
		result = append(result,
			html.Path(pathProps),
			html.Polygon(html.Props{Key: edge.ID().String() + "-arrow", Raw: map[string]any{"points": arrow, "fill": stroke}}),
		)
	}
	return result
}

func authoritativeModePanels(selected graph.Mode, content ui.Node) ui.Node {
	panels := make([]ui.Node, 0, len(graph.AllModes()))
	for _, mode := range graph.AllModes() {
		props := html.Props{
			ID: string(mode) + "-panel", Role: "tabpanel", Hidden: mode != selected,
			Aria:  map[string]string{"labelledby": string(mode)},
			Data:  map[string]string{"component": "graph-mode-panel", "mode": string(mode), "selected": strconv.FormatBool(mode == selected)},
			Class: authoritativeModePanelClass(),
		}
		if mode == selected {
			panels = append(panels, html.Div(props, content))
		} else {
			panels = append(panels, html.Div(props))
		}
	}
	return html.Div(html.Props{Data: map[string]string{"component": "graph-mode-panels"}, Class: authoritativeModePanelsClass()}, panels...)
}

func authoritativeNodeContents(node graph.Node, bounds graphlayout.Rect, mode primitives.Mode, selected bool) []ui.Node {
	tokens := mode.Tokens()
	stroke := graphStatusColor(node.Status(), tokens)
	fill := graphClassColor(node.Class(), tokens)
	dash := graphStatusDash(node.Status())
	shape := authoritativeShape(node.Class(), bounds.Width, bounds.Height, fill, stroke, dash, 2)
	// display is the visual glyph, truncated only when it would overflow the
	// node shape; full is the untruncated DisplayName the <title> tooltip
	// below always uses, so a sighted or assistive-tech user hovering a
	// clipped label still reads the complete name (M21-166).
	display, full, _ := TruncateLabel(node.DisplayName(), MaximumSVGNodeLabelRunes)
	result := []ui.Node{
		html.Tag("title", html.Props{}, html.Text(full+", "+graphClassLabel(node.Class())+", "+graphStatusLabel(node.Status()))),
	}
	if selected {
		result = append(result, html.Tag("g", html.Props{Data: map[string]string{"component": "graph-selection-ring"}},
			authoritativeShape(node.Class(), bounds.Width, bounds.Height, "none", string(tokens.Colors.Selection), "none", 6),
		))
	}
	return append(result,
		shape,
		html.Tag("text", html.Props{Raw: map[string]any{
			"x": "18", "y": "34", "fill": string(tokens.Colors.TextPrimary),
			"font-size": "15", "font-weight": "600", "font-family": tokens.Fonts.UI,
		}}, html.Text(display)),
		html.Tag("text", html.Props{Data: map[string]string{"component": "graph-node-status"}, Raw: map[string]any{
			"x": "18", "y": strconv.FormatInt(bounds.Height-18, 10), "fill": string(tokens.Colors.TextSecondary),
			"font-size": "12", "font-weight": "600", "font-family": tokens.Fonts.UI,
		}}, html.Text(graphStatusIcon(node.Status())+" "+graphStatusLabel(node.Status()))),
	)
}

func authoritativeShape(
	class graph.NodeClass,
	width, height int64,
	fill, stroke, dash string,
	strokeWidth int,
) ui.Node {
	common := map[string]any{
		"fill": fill, "stroke": stroke, "stroke-width": strconv.Itoa(strokeWidth),
		"stroke-dasharray": dash, "vector-effect": "non-scaling-stroke",
	}
	switch class {
	case graph.NodeClassEffect:
		common["points"] = fmt.Sprintf("18,0 %d,0 %d,%d %d,%d 18,%d 0,%d", width-18, width, height/2, width-18, height, height, height/2)
		return html.Polygon(html.Props{Raw: common})
	case graph.NodeClassBranchMatchMerge:
		common["points"] = fmt.Sprintf("%d,0 %d,%d %d,%d 0,%d", width/2, width, height/2, width/2, height, height/2)
		return html.Polygon(html.Props{Raw: common})
	case graph.NodeClassPlanRegion:
		common["points"] = fmt.Sprintf("0,0 %d,0 %d,%d %d,%d 0,%d", width-24, width, 24, width, height, height)
		return html.Polygon(html.Props{Raw: common})
	case graph.NodeClassArtifactResult:
		common["points"] = fmt.Sprintf("0,0 %d,0 %d,18 %d,%d 0,%d", width-18, width, width, height, height)
		return html.Polygon(html.Props{Raw: common})
	default:
		if class == graph.NodeClassRequirement {
			common["rx"] = "14"
		} else if class == graph.NodeClassObligation {
			common["rx"] = strconv.FormatInt(height/2, 10)
		} else {
			common["rx"] = "6"
		}
		common["x"], common["y"] = "0", "0"
		common["width"], common["height"] = strconv.FormatInt(width, 10), strconv.FormatInt(height, 10)
		return html.Rect(html.Props{Raw: common})
	}
}

func authoritativeToolbar(
	props AuthoritativeProps,
	current Viewport,
	width, height float64,
	setViewport func(Viewport),
	selected domain.NodeID,
	selectNode func(domain.NodeID, bool),
) ui.Node {
	center := Point{X: width / 2, Y: height / 2}
	children := []ui.Node{
		primitives.Button(primitives.ButtonProps{Label: "Fit", AccessibleLabel: "Fit graph", Mode: props.VisualMode,
			OnClick: func() { setViewport(fitAuthoritativeViewport(props.Layout.Bounds, width, height)) }}),
		primitives.Button(primitives.ButtonProps{Label: "−", AccessibleLabel: "Zoom out", Mode: props.VisualMode,
			OnClick: func() { setViewport(ZoomViewportAt(current, 0.8, center)) }}),
		primitives.Button(primitives.ButtonProps{Label: "+", AccessibleLabel: "Zoom in", Mode: props.VisualMode,
			OnClick: func() { setViewport(ZoomViewportAt(current, 1.25, center)) }}),
		primitives.Button(primitives.ButtonProps{Label: "Reset", AccessibleLabel: "Reset graph view", Mode: props.VisualMode,
			OnClick: func() { setViewport(ResetViewport()) }}),
	}
	if props.ComparisonAvailable {
		children = append(children,
			primitives.Badge(primitives.BadgeProps{Label: "New revision", Status: design.StatusActive, Mode: props.VisualMode}),
			primitives.Button(primitives.ButtonProps{
				Label: "Compare revision", AccessibleLabel: "Compare new graph revision", Mode: props.VisualMode,
				OnClick: props.OnCompareRevision,
			}))
	}
	if !props.CurrentNodeID.IsZero() && selected != props.CurrentNodeID {
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: "Return to current", AccessibleLabel: "Return to current graph node", Mode: props.VisualMode,
			OnClick: func() {
				placement, ok := authoritativePlacement(props.Layout, props.CurrentNodeID)
				if ok {
					setViewport(centerAuthoritativePlacement(current, placement, width, height))
					selectNode(props.CurrentNodeID, true)
				}
			},
		}))
	}
	return html.Div(html.Props{
		Role: "toolbar", Aria: map[string]string{"label": "Graph viewport controls"},
		Data:  map[string]string{"component": "graph-toolbar", "revision-indicator": strconv.FormatBool(props.ComparisonAvailable)},
		Class: toolbarClass(props.VisualMode.Tokens()),
	}, children...)
}

func authoritativeLegend(mode primitives.Mode, responsiveMode string) ui.Node {
	classItems := make([]ui.Node, 0, len(graph.AllNodeClasses()))
	for _, class := range graph.AllNodeClasses() {
		classItems = append(classItems, html.Li(html.Props{
			Data: map[string]string{"node-class": string(class), "shape": graphShape(class)},
			Text: graphClassLabel(class) + " — " + graphShape(class),
		}))
	}
	statusItems := make([]ui.Node, 0, len(graph.AllNodeStatuses()))
	for _, status := range graph.AllNodeStatuses() {
		statusItems = append(statusItems, html.Li(html.Props{
			Data: map[string]string{"node-status": string(status), "icon": graphStatusIcon(status)},
			Text: graphStatusIcon(status) + " " + graphStatusLabel(status),
		}))
	}
	edgeItems := make([]ui.Node, 0, len(graph.AllEdgeClasses()))
	for _, class := range graph.AllEdgeClasses() {
		edgeItems = append(edgeItems, html.Li(html.Props{Data: map[string]string{"edge-class": string(class)}, Text: edgeClassLabel(class)}))
	}
	return html.Aside(html.Props{
		Aria: map[string]string{"label": "Graph legend"}, Data: map[string]string{"component": "graph-legend", "responsive-mode": responsiveMode},
		Class: authoritativeLegendClass(mode.Tokens(), responsiveMode),
	},
		html.Div(html.Props{}, html.Strong(html.Props{Text: "Node classes"}), html.Ul(html.Props{}, classItems...)),
		html.Div(html.Props{}, html.Strong(html.Props{Text: "Statuses"}), html.Ul(html.Props{}, statusItems...)),
		html.Div(html.Props{}, html.Strong(html.Props{Text: "Relationships"}), html.Ul(html.Props{}, edgeItems...)),
	)
}

func selectedNodeAnnouncement(revision graph.Revision, selectedID domain.NodeID) ui.Node {
	message := "No graph node selected."
	for _, node := range revision.Nodes() {
		if node.ID() == selectedID {
			message = "Selected " + node.DisplayName() + ", " + graphClassLabel(node.Class()) + ", " + graphStatusLabel(node.Status()) + "."
			break
		}
	}
	return html.P(html.Props{
		Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"},
		Data: map[string]string{"component": "graph-selection-announcement"}, Class: screenReaderOnlyClass(), Text: message,
	})
}

func initialAuthoritativeSelection(props AuthoritativeProps) domain.NodeID {
	for _, candidate := range []domain.NodeID{props.SelectedNodeID, props.LinkedChatNodeID, props.CurrentNodeID} {
		if hasAuthoritativePlacement(props.Layout, candidate) {
			return candidate
		}
	}
	return domain.NodeID{}
}

func fitAuthoritativeViewport(bounds graphlayout.Rect, width, height float64) Viewport {
	return FitViewport(Rect{
		X: float64(bounds.X), Y: float64(bounds.Y), Width: float64(bounds.Width), Height: float64(bounds.Height),
	}, width, height, 36)
}

func graphNodeElementID(nodeID domain.NodeID) string { return "graph-svg-node-" + nodeID.String() }

func graphModeDescription(mode graph.Mode) string {
	switch mode {
	case graph.ModeExecution:
		return "Live work, retries, checkpoints, and attributable progress."
	case graph.ModeEvidence:
		return "Validation obligations, provenance, and result evidence."
	default:
		return "Requirements, plan regions, operations, effects, and artifacts."
	}
}

func graphShape(class graph.NodeClass) string {
	switch class {
	case graph.NodeClassRequirement:
		return "rounded rectangle"
	case graph.NodeClassPlanRegion:
		return "document"
	case graph.NodeClassEffect:
		return "hexagon"
	case graph.NodeClassBranchMatchMerge:
		return "diamond"
	case graph.NodeClassObligation:
		return "pill"
	case graph.NodeClassArtifactResult:
		return "folded document"
	default:
		return "rectangle"
	}
}

func graphStatusIcon(status graph.NodeStatus) string {
	return map[graph.NodeStatus]string{
		graph.NodeStatusPending: "○", graph.NodeStatusActive: "▶", graph.NodeStatusPassed: "✓",
		graph.NodeStatusWarning: "!", graph.NodeStatusFailed: "×", graph.NodeStatusBlocked: "■",
		graph.NodeStatusInvalidated: "↺",
	}[status]
}

func graphStatusLabel(status graph.NodeStatus) string { return humanizeGraphLabel(string(status)) }
func graphClassLabel(class graph.NodeClass) string    { return humanizeGraphLabel(string(class)) }
func edgeClassLabel(class graph.EdgeClass) string     { return humanizeGraphLabel(string(class)) }

func humanizeGraphLabel(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "-", " ")
	if value == "" {
		return "Unknown"
	}
	runes := []rune(value)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

func graphStatusDash(status graph.NodeStatus) string {
	switch status {
	case graph.NodeStatusPending:
		return "5 4"
	case graph.NodeStatusBlocked:
		return "2 3"
	case graph.NodeStatusInvalidated:
		return "8 4 2 4"
	default:
		return "none"
	}
}

func graphStatusColor(status graph.NodeStatus, tokens design.Tokens) string {
	switch status {
	case graph.NodeStatusActive:
		return string(tokens.Colors.Active)
	case graph.NodeStatusPassed:
		return string(tokens.Colors.Success)
	case graph.NodeStatusWarning:
		return string(tokens.Colors.Warning)
	case graph.NodeStatusFailed:
		return string(tokens.Colors.Failure)
	case graph.NodeStatusBlocked:
		return string(tokens.Colors.Blocked)
	case graph.NodeStatusInvalidated:
		return string(tokens.Colors.Invalidated)
	case graph.NodeStatusPending:
		return string(tokens.Colors.Pending)
	default:
		return string(tokens.Colors.BorderStrong)
	}
}

func graphClassColor(class graph.NodeClass, tokens design.Tokens) string {
	switch class {
	case graph.NodeClassPlanRegion:
		return string(tokens.Colors.Plan)
	case graph.NodeClassObligation, graph.NodeClassArtifactResult:
		return string(tokens.Colors.Evidence)
	case graph.NodeClassEffect:
		return string(tokens.Colors.Surface3)
	case graph.NodeClassRequirement:
		return string(tokens.Colors.SurfaceRaised)
	default:
		return string(tokens.Colors.Surface2)
	}
}

func edgeVisual(class graph.EdgeClass, tokens design.Tokens) (stroke, dash string, width int) {
	stroke = string(tokens.Colors.BorderStrong)
	width = 2
	switch class {
	case graph.EdgeClassDataProvenance:
		dash = "8 4"
	case graph.EdgeClassEvidenceDependency:
		stroke, dash = string(tokens.Colors.Evidence), "3 4"
	case graph.EdgeClassRetry:
		stroke, dash = string(tokens.Colors.Warning), "2 4"
	case graph.EdgeClassReconciliation:
		stroke, dash = string(tokens.Colors.Active), "10 4"
	case graph.EdgeClassCompensation:
		stroke, dash = string(tokens.Colors.Failure), "10 3 2 3"
	default:
		dash = "none"
	}
	return stroke, dash, width
}

func authoritativeContainerClass(tokens design.Tokens, height int) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)), css.W(css.Full),
		css.H(css.Px(height)), css.MinHeight(css.Px(320)), css.MinWidth(css.Zero),
		css.Padding(css.Px(tokens.Spacing.SM)), css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
	).String()
}

func authoritativeHeaderClass(tokens design.Tokens, responsiveMode string) string {
	rules := []css.Rule{u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.MD))}
	if responsiveMode == "compact" || responsiveMode == "minimum" {
		rules = []css.Rule{u.Flex, u.FlexCol, u.ItemsStretch, css.Gap(css.Px(tokens.Spacing.SM))}
	}
	return css.New(rules...).String()
}

func authoritativeViewportWrapClass() string {
	return css.New(u.Relative, css.FlexGrow(css.Num(1)), css.MinHeight(css.Px(240)), css.Overflow.Hidden).String()
}

func authoritativeModePanelsClass() string {
	return css.New(u.Flex, u.FlexCol, css.FlexGrow(css.Num(1)), css.MinHeight(css.Zero)).String()
}

func authoritativeModePanelClass() string {
	return css.New(u.Flex, u.FlexCol, css.FlexGrow(css.Num(1)), css.MinHeight(css.Zero)).String()
}

func authoritativeSVGClass(tokens design.Tokens) string {
	rules := []css.Rule{
		css.W(css.Full), css.H(css.Full), css.Cursor.Grab,
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
	}
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(-tokens.Geometry.FocusRingWidth)),
	)...)
	return css.New(rules...).String()
}

func authoritativeNodeClass(tokens design.Tokens) string {
	rules := []css.Rule{css.Cursor.Pointer}
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

func authoritativeLegendClass(tokens design.Tokens, responsiveMode string) string {
	columns := 3
	if responsiveMode == "standard" {
		columns = 2
	} else if responsiveMode == "compact" || responsiveMode == "minimum" {
		columns = 1
	}
	return css.New(
		u.Grid, css.GridCols(css.Repeat(columns, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))), css.Gap(css.Px(tokens.Spacing.MD)),
		css.Padding(css.Px(tokens.Spacing.SM)), css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
	).String()
}

// graphPanelTitleClass names the task graph panel.
//
// It is set on the product's own type scale rather than left to the browser's
// default heading size, which would render it larger than the page title it
// sits beneath.
func graphPanelTitleClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.Display)),
		css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
		css.FontWeight.Normal,
	).String()
}

// graphPanelDescriptionClass sets the sentence explaining the current mode.
func graphPanelDescriptionClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.MaxWidth(css.Ch(74)),
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)),
	).String()
}
