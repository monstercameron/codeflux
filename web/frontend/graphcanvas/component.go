package graphcanvas

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type Props struct {
	Authoritative    *AuthoritativeProps
	Nodes            []state.GraphNodeView
	Edges            []Edge
	SelectedID       string
	CurrentID        string
	ResponsiveMode   string
	Mode             primitives.Mode
	Height           int
	OnSelect         func(string)
	OnViewportChange func(Viewport)
}

// Renderer validates and lays out a bounded graph before mounting the browser
// drawing component. Invalid graphs remain visible instead of partially drawing.
func Renderer(props Props) ui.Node {
	if props.Authoritative != nil {
		return authoritativeRenderer(*props.Authoritative)
	}
	return ui.CreateElement(stableRenderer, props)
}

func stableRenderer(props Props) ui.Node {
	previous := ui.UseRef(Layout{})
	prior := previous.Get()
	layout, err := BuildStableLayout(props.Nodes, props.Edges, &prior)
	if err != nil {
		return graphError(props.Mode, err)
	}
	if len(layout.Nodes) == 0 {
		return graphEmpty(props.Mode)
	}
	previous.Set(layout)
	return ui.CreateElement(interactiveCanvas, interactiveProps{Props: props, Layout: layout})
}

type interactiveProps struct {
	Props
	Layout Layout
}

type dragState struct {
	Active               bool
	Moved                bool
	StartX, StartY       float64
	StartPanX, StartPanY float64
}

type drawFrame struct {
	Layout        Layout
	Viewport      Viewport
	Width         float64
	Height        float64
	Backing       BackingStoreSize
	Tokens        design.Tokens
	HighContrast  bool
	ReducedMotion bool
	HoveredID     string
	SelectedID    string
}

type canvasSurfaceProps struct {
	Frame           drawFrame
	Height          int
	OnPointerDown   func(ui.Event)
	OnPointerMove   func(ui.Event)
	OnPointerUp     func(ui.Event)
	OnPointerCancel func(ui.Event)
	OnWheel         func(ui.Event)
	OnFocus         func(ui.Event)
	OnBlur          func(ui.Event)
}

func canvasSurface(props canvasSurfaceProps) ui.Node {
	canvasRef := ui.UseDOMRef()
	ui.UseLayoutEffect(func() func() {
		drawCanvas(canvasRef, props.Frame)
		return nil
	}, canvasDrawKey(props.Frame))
	ui.UseWheel(canvasRef, props.OnWheel)
	canvasProps := html.PropsOf(
		html.Ref(canvasRef),
		html.OnPointerDown(props.OnPointerDown),
		html.OnPointerMove(props.OnPointerMove),
		html.OnPointerUp(props.OnPointerUp),
	)
	canvasProps.ID = "graph-canvas-surface"
	canvasProps.Key = "graph-canvas-surface"
	canvasProps.TabIndex = html.TabIndexZero
	canvasProps.Width = strconv.Itoa(props.Frame.Backing.Width)
	canvasProps.Height = strconv.Itoa(props.Frame.Backing.Height)
	canvasProps.Aria = map[string]string{
		"label": "Graph viewport. Focus this surface before using the mouse wheel to zoom.",
	}
	canvasProps.Data = map[string]string{
		"component": "graph-render-surface", "renderer": "canvas-2d",
		"high-dpi": "true", "dpr": strconv.FormatFloat(props.Frame.Backing.DPR, 'f', 2, 64),
		"reduced-motion": strconv.FormatBool(props.Frame.ReducedMotion),
	}
	canvasProps.Class = canvasClass(props.Frame.Tokens, props.Height)
	canvasProps.OnFocus = html.PropsOf(html.OnFocus(props.OnFocus)).OnFocus
	canvasProps.OnBlur = html.PropsOf(html.OnBlur(props.OnBlur)).OnBlur
	canvasProps.OnAnimationEnd = html.PropsOf(html.OnAnimationEnd(func(ui.Event) {
		drawCanvas(canvasRef, props.Frame)
	})).OnAnimationEnd
	canvasProps.OnMouseEnter = html.PropsOf(html.OnMouseEnter(func(ui.Event) {
		drawCanvas(canvasRef, props.Frame)
	})).OnMouseEnter
	return html.Canvas(canvasProps, html.Text("Project graph. Use the graph-node controls following the canvas."))
}

func canvasDrawKey(frame drawFrame) string {
	return fmt.Sprintf(
		"%s|%.1f|%.1f|%d|%d|%.4f|%.2f|%.2f|%s|%s|%s|%t|%t",
		frame.Layout.Key, frame.Width, frame.Height, frame.Backing.Width, frame.Backing.Height,
		frame.Viewport.Zoom, frame.Viewport.PanX, frame.Viewport.PanY,
		frame.HoveredID, frame.SelectedID, frame.Tokens.Theme,
		frame.HighContrast, frame.ReducedMotion,
	)
}

func interactiveCanvas(props interactiveProps) ui.Node {
	height := props.Height
	if height <= 0 {
		height = 390
	}
	containerRef := ui.UseDOMRef()
	geometry := ui.UseElementGeometry(containerRef)
	width := geometry.Width
	if width <= 0 {
		// The first WASM render can precede the ref-backed ResizeObserver.
		// Keep the temporary backing store larger than the supported workspace
		// so the browser never upscales a low-resolution canvas.
		width = 1024
	}
	measuredHeight := geometry.Height
	if measuredHeight <= 0 {
		measuredHeight = math.Max(float64(height), 900)
	}
	viewport := ui.UseState(FitViewport(props.Layout.Bounds, width, measuredHeight, 28))
	hovered := ui.UseState("")
	selected := ui.UseState(initialSelectedID(props.Props))
	focused := ui.UseState(false)
	drag := ui.UseRef(dragState{})
	selectNode := ui.UseEvent(func(event ui.MouseEvent) {
		nodeID := strings.TrimSpace(event.GetValue())
		if _, ok := placementByID(props.Layout, nodeID); !ok {
			return
		}
		selected.Set(nodeID)
		if props.OnSelect != nil {
			onSelect := props.OnSelect
			ui.PostAsync(func() { onSelect(nodeID) })
		}
	})
	ui.UseEffect(func() func() {
		external := strings.TrimSpace(props.SelectedID)
		if external != "" && external != selected.Get() {
			selected.Set(external)
		}
		return nil
	}, props.SelectedID)
	setViewport := func(next Viewport) {
		next = next.Normalize()
		viewport.Set(next)
		if props.OnViewportChange != nil {
			props.OnViewportChange(next)
		}
	}
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
		if math.Abs(x-currentDrag.StartX) > 3 || math.Abs(y-currentDrag.StartY) > 3 {
			currentDrag.Moved = true
			drag.Set(currentDrag)
		}
		current := viewport.Get().Normalize()
		current.PanX = currentDrag.StartPanX + x - currentDrag.StartX
		current.PanY = currentDrag.StartPanY + y - currentDrag.StartY
		setViewport(current)
	}
	endPan := func(event ui.Event) {
		releasePointer(event)
		currentDrag := drag.Get()
		drag.Set(dragState{Moved: currentDrag.Moved})
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
		factor := math.Exp(-delta * 0.0012)
		setViewport(ZoomViewportAt(viewport.Get(), factor, Point{X: x, Y: y}))
	}
	tokens := props.Mode.Tokens()
	backing := BackingStore(width, measuredHeight, browserDevicePixelRatio())
	frame := drawFrame{
		Layout: props.Layout, Viewport: viewport.Get().Normalize(), Width: width, Height: measuredHeight,
		Backing: backing, Tokens: tokens, HighContrast: props.Mode.HighContrast,
		ReducedMotion: props.Mode.ReducedMotion, HoveredID: hovered.Get(), SelectedID: selected.Get(),
	}
	nodesByID := make(map[string]state.GraphNodeView, len(props.Nodes))
	for _, node := range props.Nodes {
		nodesByID[node.ID] = node
	}
	responsiveMode := strings.TrimSpace(props.ResponsiveMode)
	if responsiveMode == "" {
		responsiveMode = "wide"
	}
	currentViewport := viewport.Get().Normalize()
	sectionProps := html.PropsOf(html.Ref(containerRef))
	sectionProps.Aria = map[string]string{"label": "Project graph"}
	sectionProps.Data = map[string]string{
		"component": "graph-canvas", "layout": "deterministic-layered", "renderer": "canvas-2d",
		"high-dpi": "true", "device-pixel-ratio": strconv.FormatFloat(backing.DPR, 'f', 3, 64),
		"backing-width": strconv.Itoa(backing.Width), "backing-height": strconv.Itoa(backing.Height),
		"zoom":             strconv.FormatFloat(currentViewport.Zoom, 'f', 6, 64),
		"pan-x":            strconv.FormatFloat(currentViewport.PanX, 'f', 3, 64),
		"pan-y":            strconv.FormatFloat(currentViewport.PanY, 'f', 3, 64),
		"selected-node-id": selected.Get(), "responsive-mode": responsiveMode,
		"node-count": strconv.Itoa(len(props.Layout.Nodes)), "edge-count": strconv.Itoa(len(props.Layout.Edges)),
	}
	sectionProps.Class = containerClass(tokens, height)
	return html.Section(sectionProps,
		ui.CreateElement(canvasSurface, canvasSurfaceProps{
			Frame: frame, Height: height,
			OnPointerDown: startPan, OnPointerMove: movePan, OnPointerUp: endPan,
			OnPointerCancel: func(ui.Event) { drag.Set(dragState{}) },
			OnWheel:         wheelHandler,
			OnFocus:         func(ui.Event) { focused.Set(true) },
			OnBlur: func(ui.Event) {
				focused.Set(false)
				drag.Set(dragState{})
			},
		}),
		domCompanion(
			props, viewport.Get(), width, measuredHeight,
			selected, hovered, selectNode,
		),
		viewportToolbar(props, viewport.Get(), width, measuredHeight, setViewport, selected),
		edgeTextAlternative(props.Layout.Edges, nodesByID, props.Mode),
	)
}

func initialSelectedID(props Props) string {
	if selected := strings.TrimSpace(props.SelectedID); selected != "" {
		return selected
	}
	for _, node := range props.Nodes {
		if node.Selected {
			return node.ID
		}
	}
	if current := strings.TrimSpace(props.CurrentID); current != "" {
		return current
	}
	return ""
}

func domCompanion(
	props interactiveProps,
	viewport Viewport,
	width, height float64,
	selected, hovered ui.State[string],
	selectNode ui.Handler,
) ui.Node {
	buttons := make([]ui.Node, 0, len(props.Layout.Nodes))
	for _, placement := range props.Layout.Nodes {
		placement := placement
		screen := ScreenRect(placement.Bounds, viewport)
		buttonProps := html.PropsOf(
			html.OnMouseEnter(func(ui.Event) { hovered.Set(placement.Node.ID) }),
			html.OnMouseLeave(func(ui.Event) {
				if hovered.Get() == placement.Node.ID {
					hovered.Set("")
				}
			}),
			html.OnFocus(func(ui.Event) { hovered.Set(placement.Node.ID) }),
			html.OnBlur(func(ui.Event) {
				if hovered.Get() == placement.Node.ID {
					hovered.Set("")
				}
			}),
		)
		buttonProps.OnClick = selectNode
		buttonProps.Type = "button"
		buttonProps.Value = placement.Node.ID
		buttonProps.Aria = map[string]string{
			"label":   placement.Node.Title + " - " + statusLabel(placement.Node.Status),
			"pressed": strconv.FormatBool(selected.Get() == placement.Node.ID),
			"description": semanticLabel(placement.Semantic) + " node, " +
				shapeLabel(shapeForSemantic(placement.Semantic)) + " shape",
		}
		buttonProps.Data = map[string]string{
			"component": "graph-node", "node-id": placement.Node.ID,
			"selected": strconv.FormatBool(selected.Get() == placement.Node.ID),
			"semantic": string(placement.Semantic), "status": placement.Node.Status,
			"shape": string(shapeForSemantic(placement.Semantic)),
		}
		buttonProps.Class = companionButtonClass(props.Mode.Tokens(), screen, width, height)
		buttons = append(buttons, html.Button(buttonProps))
	}
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Graph nodes"},
		Class: companionLayerClass(),
	}, buttons...)
}

func viewportToolbar(
	props interactiveProps,
	current Viewport,
	width, height float64,
	setViewport func(Viewport),
	selected ui.State[string],
) ui.Node {
	center := Point{X: width / 2, Y: height / 2}
	return html.Div(html.Props{
		Role: "toolbar", Aria: map[string]string{"label": "Graph viewport controls"},
		Data: map[string]string{"component": "graph-canvas-toolbar"}, Class: toolbarClass(props.Mode.Tokens()),
	},
		primitives.Button(primitives.ButtonProps{
			Label: "Fit", AccessibleLabel: "Fit graph", Mode: props.Mode,
			OnClick: func() { setViewport(FitViewport(props.Layout.Bounds, width, height, 28)) },
		}),
		primitives.Button(primitives.ButtonProps{
			Label: "−", AccessibleLabel: "Zoom out", Mode: props.Mode,
			OnClick: func() { setViewport(ZoomViewportAt(current, 0.8, center)) },
		}),
		primitives.Button(primitives.ButtonProps{
			Label: "+", AccessibleLabel: "Zoom in", Mode: props.Mode,
			OnClick: func() { setViewport(ZoomViewportAt(current, 1.25, center)) },
		}),
		primitives.Button(primitives.ButtonProps{
			Label: "Reset", AccessibleLabel: "Reset graph view", Mode: props.Mode,
			OnClick: func() { setViewport(ResetViewport()) },
		}),
		returnToCurrentControl(props, setViewport, selected, width, height),
	)
}

func returnToCurrentControl(
	props interactiveProps,
	setViewport func(Viewport),
	selected ui.State[string],
	width, height float64,
) ui.Node {
	currentID := strings.TrimSpace(props.CurrentID)
	if currentID == "" || selected.Get() == currentID {
		return html.Span(html.Props{Hidden: true})
	}
	return primitives.Button(primitives.ButtonProps{
		Label: "Return to current", AccessibleLabel: "Return to current graph node", Mode: props.Mode,
		OnClick: func() {
			if placement, ok := placementByID(props.Layout, currentID); ok {
				if props.OnSelect != nil {
					props.OnSelect(currentID)
				}
				selected.Set(currentID)
				setViewport(Viewport{
					Zoom: 1, PanX: width/2 - placement.Bounds.Center().X,
					PanY: height/2 - placement.Bounds.Center().Y,
				})
			}
		},
	})
}

func placementByID(layout Layout, id string) (NodePlacement, bool) {
	for _, placement := range layout.Nodes {
		if placement.Node.ID == id {
			return placement, true
		}
	}
	return NodePlacement{}, false
}

func edgeTextAlternative(edges []EdgePlacement, nodes map[string]state.GraphNodeView, mode primitives.Mode) ui.Node {
	items := make([]ui.Node, 0, len(edges))
	for _, placement := range edges {
		items = append(items, html.Li(html.Props{Text: edgeLabel(placement.Edge, nodes)}))
	}
	return html.Div(html.Props{Class: screenReaderOnlyClass(), Data: map[string]string{"component": "graph-edge-text"}},
		html.H2(html.Props{Text: "Graph relationships"}), html.Ul(html.Props{}, items...),
	)
}

func graphError(mode primitives.Mode, err error) ui.Node {
	return primitives.InlineAlert(primitives.InlineAlertProps{
		Title: "Graph unavailable", Message: "The graph could not be presented safely: " + err.Error(),
		Tone: design.StatusWarning, Mode: mode,
	})
}

func graphEmpty(mode primitives.Mode) ui.Node {
	return primitives.EmptyState(primitives.EmptyStateProps{
		Title: "No graph nodes", Body: "This bounded task slice does not contain graph nodes.", Mode: mode,
	})
}

func statusLabel(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "Unknown status"
	}
	words := strings.ReplaceAll(strings.ToLower(status), "-", " ")
	runes := []rune(words)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

func containerClass(tokens design.Tokens, _ int) string {
	return css.New(
		u.Relative, css.FlexGrow(css.Num(1)),
		css.W(css.Full), css.H(css.Full), css.MinHeight(css.Px(240)),
		css.MinWidth(css.Zero), css.Overflow.Hidden,
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.Border(css.Zero, css.Transparent),
	).String()
}

func canvasClass(tokens design.Tokens, _ int) string {
	return css.New(
		css.W(css.Full), css.H(css.Full), css.MaxWidth(css.Full),
		css.Cursor.Grab, css.UserSelect.None,
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.Keyframes("graph-canvas-paint",
			css.At("0%", css.OpacityNum(css.Num(0.999))),
			css.At("100%", css.OpacityNum(css.Num(1))),
		),
		css.Animation(css.Ms(1), css.Linear),
	).String()
}

func companionLayerClass() string {
	return css.New(u.Absolute, css.Inset(css.Zero), css.PointerEvents.None, css.ZIndex(2)).String()
}

func companionButtonClass(tokens design.Tokens, bounds Rect, width, height float64) string {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	left := max(0, min(100, bounds.X/width*100))
	top := max(0, min(100, bounds.Y/height*100))
	buttonWidth := max(0.1, min(100-left, bounds.Width/width*100))
	buttonHeight := max(0.1, min(100-top, bounds.Height/height*100))
	rules := []css.Rule{
		u.Absolute, css.Left(css.Percent(left)), css.Top(css.Percent(top)),
		css.W(css.Percent(buttonWidth)), css.H(css.Percent(buttonHeight)),
		css.Padding(css.Zero), css.Bg(css.Transparent), css.Border(css.Px(1), css.Transparent),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)), css.PointerEvents.Auto, css.Cursor.Pointer,
	}
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

func toolbarClass(tokens design.Tokens) string {
	return css.New(
		u.Absolute, u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
		css.Left(css.Px(tokens.Spacing.MD)), css.Bottom(css.Px(tokens.Spacing.MD)), css.ZIndex(3),
		css.Padding(css.Px(tokens.Spacing.XS)), css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Px(12), css.Px(28), css.Px(-16), css.RGBA(0, 0, 0, 0.72),
		)),
	).String()
}

func screenReaderOnlyClass() string {
	return css.New(
		u.Absolute, css.W(css.Px(1)), css.H(css.Px(1)), css.Overflow.Hidden,
		css.Left(css.Zero), css.Top(css.Zero), css.OpacityNum(css.Num(0)), css.PointerEvents.None,
	).String()
}
