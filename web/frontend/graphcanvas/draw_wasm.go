//go:build js && wasm

package graphcanvas

import (
	"math"
	"strings"
	"syscall/js"

	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func drawCanvas(ref ui.DOMRef, frame drawFrame) {
	canvas := ref.Value()
	if !canvas.Truthy() {
		document := js.Global().Get("document")
		if document.Truthy() {
			canvas = document.Call("getElementById", "graph-canvas-surface")
		}
	}
	if !canvas.Truthy() {
		return
	}
	canvas.Call("setAttribute", "data-draw-status", "drawing")
	if canvas.Get("width").Int() != frame.Backing.Width {
		canvas.Set("width", frame.Backing.Width)
	}
	if canvas.Get("height").Int() != frame.Backing.Height {
		canvas.Set("height", frame.Backing.Height)
	}
	context := canvas.Call("getContext", "2d")
	if !context.Truthy() {
		return
	}
	context.Call("setTransform", frame.Backing.DPR, 0, 0, frame.Backing.DPR, 0, 0)
	context.Set("fillStyle", string(frame.Tokens.Colors.SurfaceInset))
	context.Call("fillRect", 0, 0, frame.Width, frame.Height)
	if !frame.HighContrast {
		drawInstrumentField(context, frame)
		drawOrientationGrid(context, frame)
	}
	context.Call("save")
	context.Call("translate", frame.Viewport.PanX, frame.Viewport.PanY)
	context.Call("scale", frame.Viewport.Zoom, frame.Viewport.Zoom)
	for _, edge := range frame.Layout.Edges {
		drawEdge(context, edge, frame)
	}
	for _, nodePlacement := range frame.Layout.Nodes {
		drawNode(context, nodePlacement, frame)
	}
	context.Call("restore")
	canvas.Call("setAttribute", "data-draw-status", "ready")
}

func drawInstrumentField(context js.Value, frame drawFrame) {
	gradient := context.Call(
		"createRadialGradient",
		frame.Width*0.72, frame.Height*0.28, 0,
		frame.Width*0.72, frame.Height*0.28, math.Max(frame.Width, frame.Height)*0.78,
	)
	gradient.Call("addColorStop", 0, string(frame.Tokens.Colors.Active)+"20")
	gradient.Call("addColorStop", 0.46, string(frame.Tokens.Colors.Plan)+"0c")
	gradient.Call("addColorStop", 1, string(frame.Tokens.Colors.SurfaceInset)+"00")
	context.Set("fillStyle", gradient)
	context.Call("fillRect", 0, 0, frame.Width, frame.Height)
}

func drawOrientationGrid(context js.Value, frame drawFrame) {
	context.Call("save")
	context.Set("fillStyle", string(frame.Tokens.Colors.BorderSubtle))
	context.Set("globalAlpha", 0.44)
	const spacing = 28.0
	offsetX := math.Mod(frame.Viewport.PanX, spacing)
	offsetY := math.Mod(frame.Viewport.PanY, spacing)
	for x := offsetX; x < frame.Width; x += spacing {
		for y := offsetY; y < frame.Height; y += spacing {
			size := 1.0
			if math.Abs(math.Mod(x-offsetX, spacing*4)) < 0.1 &&
				math.Abs(math.Mod(y-offsetY, spacing*4)) < 0.1 {
				size = 1.8
			}
			context.Call("fillRect", x, y, size, size)
		}
	}
	context.Call("restore")
}

func drawEdge(context js.Value, placement EdgePlacement, frame drawFrame) {
	context.Call("save")
	context.Set("strokeStyle", edgeColor(placement.Edge.Kind, frame.Tokens))
	context.Set("fillStyle", edgeColor(placement.Edge.Kind, frame.Tokens))
	context.Set("lineWidth", 1.5/frame.Viewport.Zoom)
	context.Set("globalAlpha", 0.84)
	context.Call("setLineDash", edgeDash(placement.Edge.Kind, frame.Viewport.Zoom))
	context.Call("beginPath")
	context.Call("moveTo", placement.Start.X, placement.Start.Y)
	context.Call("bezierCurveTo",
		placement.Control1.X, placement.Control1.Y,
		placement.Control2.X, placement.Control2.Y,
		placement.Destination.X, placement.Destination.Y,
	)
	context.Call("stroke")
	angle := math.Atan2(
		placement.Destination.Y-placement.Control2.Y,
		placement.Destination.X-placement.Control2.X,
	)
	arrow := 7.0 / frame.Viewport.Zoom
	context.Call("beginPath")
	context.Call("moveTo", placement.Destination.X, placement.Destination.Y)
	context.Call("lineTo",
		placement.Destination.X-arrow*math.Cos(angle-math.Pi/6),
		placement.Destination.Y-arrow*math.Sin(angle-math.Pi/6),
	)
	context.Call("lineTo",
		placement.Destination.X-arrow*math.Cos(angle+math.Pi/6),
		placement.Destination.Y-arrow*math.Sin(angle+math.Pi/6),
	)
	context.Call("closePath")
	context.Call("fill")
	context.Call("restore")
}

func drawNode(context js.Value, placement NodePlacement, frame drawFrame) {
	selected := placement.Node.ID == frame.SelectedID
	hovered := placement.Node.ID == frame.HoveredID
	style := StyleFor(placement, frame.SelectedID, "")
	fill := frame.Tokens.Colors.Surface2
	if hovered || selected {
		fill = frame.Tokens.Colors.Surface3
	}
	stroke := nodeColor(placement, frame.Tokens)
	status := strings.ToLower(strings.TrimSpace(placement.Node.Status))
	switch status {
	case "active", "running", "in progress":
		stroke = frame.Tokens.Colors.Active
	case "failed", "blocked":
		stroke = frame.Tokens.Colors.Failure
	case "warning", "waiting":
		stroke = frame.Tokens.Colors.Warning
	}
	if selected {
		stroke = frame.Tokens.Colors.FocusRing
	}
	context.Call("save")
	if selected && !frame.ReducedMotion {
		context.Set("shadowColor", string(frame.Tokens.Colors.FocusRing))
		context.Set("shadowBlur", 18/frame.Viewport.Zoom)
	}
	context.Set("fillStyle", string(fill))
	context.Set("strokeStyle", string(stroke))
	context.Set("lineWidth", map[bool]float64{true: 2.2, false: 1.2}[selected]/frame.Viewport.Zoom)
	context.Call("setLineDash", nodeDash(style.Border, frame.Viewport.Zoom))
	nodeShapePath(context, placement.Bounds, style.Shape)
	context.Call("fill")
	context.Call("stroke")
	drawNodeShapeDetail(context, placement.Bounds, style.Shape, frame.Viewport.Zoom)
	context.Set("shadowBlur", 0)
	context.Set("fillStyle", string(frame.Tokens.Colors.TextPrimary))
	context.Set("font", "600 12px "+frame.Tokens.Fonts.UI)
	context.Set("textBaseline", "middle")
	display, _, clipped := TruncateLabel(placement.Node.Title, 34)
	lines := labelLines(display, 21)
	lineY := placement.Bounds.Y + placement.Bounds.Height/2
	if len(lines) == 2 {
		lineY -= 8
	}
	for index, line := range lines {
		context.Call("fillText", line, placement.Bounds.X+30, lineY+float64(index)*16, placement.Bounds.Width-54)
	}
	semanticIcon := semanticGlyph(placement.Semantic)
	statusIcon := style.StatusIcon
	if statusIcon == "" {
		statusIcon = statusGlyph(status)
	}
	context.Set("fillStyle", string(stroke))
	context.Set("font", "700 13px "+frame.Tokens.Fonts.UI)
	context.Call("fillText", semanticIcon, placement.Bounds.X+11, placement.Bounds.Y+placement.Bounds.Height/2, 16)
	context.Set("font", "700 12px "+frame.Tokens.Fonts.UI)
	context.Call("fillText", statusIcon, placement.Bounds.X+placement.Bounds.Width-20, placement.Bounds.Y+placement.Bounds.Height/2, 14)
	if clipped {
		context.Set("fillStyle", string(frame.Tokens.Colors.TextMuted))
		context.Call("fillText", "…", placement.Bounds.X+placement.Bounds.Width-14, placement.Bounds.Y+placement.Bounds.Height/2, 10)
	}
	context.Call("restore")
}

func nodeShapePath(context js.Value, bounds Rect, shape Shape) {
	switch shape {
	case ShapeDocument:
		documentPath(context, bounds)
	case ShapeHexagon:
		hexagonPath(context, bounds)
	case ShapePill:
		roundedRectPath(context, bounds, bounds.Height/2)
	case ShapeCylinder:
		cylinderPath(context, bounds)
	default:
		roundedRectPath(context, bounds, 8)
	}
}

func documentPath(context js.Value, bounds Rect) {
	fold := math.Min(18, bounds.Height*0.34)
	radius := math.Min(7, bounds.Height/4)
	context.Call("beginPath")
	context.Call("moveTo", bounds.X+radius, bounds.Y)
	context.Call("lineTo", bounds.X+bounds.Width-fold, bounds.Y)
	context.Call("lineTo", bounds.X+bounds.Width, bounds.Y+fold)
	context.Call("lineTo", bounds.X+bounds.Width, bounds.Y+bounds.Height-radius)
	context.Call("quadraticCurveTo", bounds.X+bounds.Width, bounds.Y+bounds.Height, bounds.X+bounds.Width-radius, bounds.Y+bounds.Height)
	context.Call("lineTo", bounds.X+radius, bounds.Y+bounds.Height)
	context.Call("quadraticCurveTo", bounds.X, bounds.Y+bounds.Height, bounds.X, bounds.Y+bounds.Height-radius)
	context.Call("lineTo", bounds.X, bounds.Y+radius)
	context.Call("quadraticCurveTo", bounds.X, bounds.Y, bounds.X+radius, bounds.Y)
	context.Call("closePath")
}

func hexagonPath(context js.Value, bounds Rect) {
	cut := math.Min(16, bounds.Width*0.12)
	context.Call("beginPath")
	context.Call("moveTo", bounds.X+cut, bounds.Y)
	context.Call("lineTo", bounds.X+bounds.Width-cut, bounds.Y)
	context.Call("lineTo", bounds.X+bounds.Width, bounds.Y+bounds.Height/2)
	context.Call("lineTo", bounds.X+bounds.Width-cut, bounds.Y+bounds.Height)
	context.Call("lineTo", bounds.X+cut, bounds.Y+bounds.Height)
	context.Call("lineTo", bounds.X, bounds.Y+bounds.Height/2)
	context.Call("closePath")
}

func cylinderPath(context js.Value, bounds Rect) {
	capHeight := math.Min(9, bounds.Height*0.22)
	context.Call("beginPath")
	context.Call("moveTo", bounds.X, bounds.Y+capHeight)
	context.Call("quadraticCurveTo", bounds.X+bounds.Width/2, bounds.Y-capHeight*0.45, bounds.X+bounds.Width, bounds.Y+capHeight)
	context.Call("lineTo", bounds.X+bounds.Width, bounds.Y+bounds.Height-capHeight)
	context.Call("quadraticCurveTo", bounds.X+bounds.Width/2, bounds.Y+bounds.Height+capHeight*0.45, bounds.X, bounds.Y+bounds.Height-capHeight)
	context.Call("closePath")
}

func drawNodeShapeDetail(context js.Value, bounds Rect, shape Shape, zoom float64) {
	context.Call("save")
	context.Call("setLineDash", []any{})
	context.Set("lineWidth", 0.9/zoom)
	context.Set("globalAlpha", 0.7)
	switch shape {
	case ShapeDocument:
		fold := math.Min(18, bounds.Height*0.34)
		context.Call("beginPath")
		context.Call("moveTo", bounds.X+bounds.Width-fold, bounds.Y)
		context.Call("lineTo", bounds.X+bounds.Width-fold, bounds.Y+fold)
		context.Call("lineTo", bounds.X+bounds.Width, bounds.Y+fold)
		context.Call("stroke")
	case ShapeCylinder:
		capHeight := math.Min(9, bounds.Height*0.22)
		context.Call("beginPath")
		context.Call("moveTo", bounds.X, bounds.Y+capHeight)
		context.Call("quadraticCurveTo", bounds.X+bounds.Width/2, bounds.Y+capHeight*2.45, bounds.X+bounds.Width, bounds.Y+capHeight)
		context.Call("stroke")
	}
	context.Call("restore")
}

func roundedRectPath(context js.Value, bounds Rect, radius float64) {
	radius = math.Min(radius, math.Min(bounds.Width, bounds.Height)/2)
	context.Call("beginPath")
	context.Call("moveTo", bounds.X+radius, bounds.Y)
	context.Call("lineTo", bounds.X+bounds.Width-radius, bounds.Y)
	context.Call("quadraticCurveTo", bounds.X+bounds.Width, bounds.Y, bounds.X+bounds.Width, bounds.Y+radius)
	context.Call("lineTo", bounds.X+bounds.Width, bounds.Y+bounds.Height-radius)
	context.Call("quadraticCurveTo", bounds.X+bounds.Width, bounds.Y+bounds.Height, bounds.X+bounds.Width-radius, bounds.Y+bounds.Height)
	context.Call("lineTo", bounds.X+radius, bounds.Y+bounds.Height)
	context.Call("quadraticCurveTo", bounds.X, bounds.Y+bounds.Height, bounds.X, bounds.Y+bounds.Height-radius)
	context.Call("lineTo", bounds.X, bounds.Y+radius)
	context.Call("quadraticCurveTo", bounds.X, bounds.Y, bounds.X+radius, bounds.Y)
	context.Call("closePath")
}

func edgeColor(kind EdgeKind, tokens design.Tokens) string {
	switch kind {
	case EdgeControl:
		return string(tokens.Colors.Active)
	case EdgeEvidence:
		return string(tokens.Colors.Evidence)
	case EdgeRetry:
		return string(tokens.Colors.Warning)
	case EdgeReconciliation:
		return string(tokens.Colors.Plan)
	case EdgeCompensation:
		return string(tokens.Colors.Failure)
	default:
		return string(tokens.Colors.BorderStrong)
	}
}

func edgeDash(kind EdgeKind, zoom float64) any {
	switch kind {
	case EdgeRetry:
		return []any{7 / zoom, 5 / zoom}
	case EdgeReconciliation:
		return []any{2 / zoom, 4 / zoom}
	case EdgeCompensation:
		return []any{9 / zoom, 4 / zoom, 2 / zoom, 4 / zoom}
	default:
		return []any{}
	}
}

func nodeDash(border BorderStyle, zoom float64) any {
	if border == BorderDashed {
		return []any{6 / zoom, 4 / zoom}
	}
	return []any{}
}

func nodeColor(placement NodePlacement, tokens design.Tokens) design.Color {
	switch placement.Semantic {
	case NodeExternal:
		return tokens.Colors.Active
	case NodePlan:
		return tokens.Colors.Plan
	case NodeEvidence:
		return tokens.Colors.Evidence
	case NodeMemory:
		return tokens.Colors.Accent
	default:
		return tokens.Colors.BorderStrong
	}
}

func labelLines(label string, maximum int) []string {
	words := strings.Fields(label)
	if len(words) == 0 {
		return []string{"Unknown"}
	}
	first := words[0]
	for index := 1; index < len(words); index++ {
		candidate := first + " " + words[index]
		if len([]rune(candidate)) > maximum {
			return []string{first, strings.Join(words[index:], " ")}
		}
		first = candidate
	}
	return []string{first}
}

func statusGlyph(status string) string {
	switch status {
	case "complete", "completed", "passed":
		return "✓"
	case "active", "running", "in progress":
		return "◉"
	case "failed", "blocked":
		return "!"
	default:
		return "○"
	}
}

func semanticGlyph(semantic NodeSemantic) string {
	switch semantic {
	case NodeExternal:
		return "↗"
	case NodePlan:
		return "◇"
	case NodeEvidence:
		return "◆"
	case NodeMemory:
		return "◫"
	case NodeOperation:
		return "ƒ"
	default:
		return "•"
	}
}
