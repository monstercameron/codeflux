package graphcanvas

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

// MaximumCanvasNodeLabelRunes bounds the node label glyph rendered onto the
// Canvas 2D pixel surface (draw_wasm.go). Truncating this visual label never
// removes the full DisplayName from the page: the DOM companion button laid
// over the same node bounds always carries the full text as its Title
// (native hover tooltip), aria-label, and full-label data hook (M21-166).
const MaximumCanvasNodeLabelRunes = 34

// MaximumSVGNodeLabelRunes bounds the node label glyph rendered as SVG <text>
// in the authoritative renderer (authoritative.go). Truncating this visual
// label never removes the full DisplayName: the sibling SVG <title> tooltip,
// the node group's aria-label, and its full-label data hook always carry the
// full, untruncated text (M21-166).
const MaximumSVGNodeLabelRunes = 26

type Interaction struct {
	Viewport   Viewport
	SelectedID string
	ActiveID   string
}

func SelectNode(current Interaction, layout Layout, id string) (Interaction, bool) {
	id = strings.TrimSpace(id)
	if id != "" && !layoutHasNode(layout, id) {
		return current, false
	}
	current.SelectedID = id
	return current, true
}

func ActivateNode(current Interaction, layout Layout, id string) (Interaction, bool) {
	id = strings.TrimSpace(id)
	if id != "" && !layoutHasNode(layout, id) {
		return current, false
	}
	current.ActiveID = id
	return current, true
}

// ReconcileInteraction preserves viewport and compatible stable identities
// across an immutable graph revision.
func ReconcileInteraction(current Interaction, next Layout) Interaction {
	if !layoutHasNode(next, current.SelectedID) {
		current.SelectedID = ""
	}
	if !layoutHasNode(next, current.ActiveID) {
		current.ActiveID = ""
	}
	return current
}

// ResizeViewport preserves the world point at the old viewport center.
func ResizeViewport(viewport Viewport, oldWidth, oldHeight, newWidth, newHeight float64) Viewport {
	viewport = viewport.Normalize()
	if oldWidth <= 0 || oldHeight <= 0 || newWidth <= 0 || newHeight <= 0 ||
		!finiteInteraction(oldWidth) || !finiteInteraction(oldHeight) ||
		!finiteInteraction(newWidth) || !finiteInteraction(newHeight) {
		return viewport
	}
	worldCenter := Point{
		X: (oldWidth/2 - viewport.PanX) / viewport.Zoom,
		Y: (oldHeight/2 - viewport.PanY) / viewport.Zoom,
	}
	viewport.PanX = newWidth/2 - worldCenter.X*viewport.Zoom
	viewport.PanY = newHeight/2 - worldCenter.Y*viewport.Zoom
	return viewport
}

func ResetViewport() Viewport { return Viewport{Zoom: 1} }

type HitKind string

const (
	HitNone HitKind = "none"
	HitNode HitKind = "node"
	HitEdge HitKind = "edge"
)

type HitResult struct {
	Kind   HitKind
	NodeID string
	EdgeID string
	World  Point
}

// HitTestGeometry tests nodes before edges and keeps screen-space edge
// tolerance constant across zoom levels.
func HitTestGeometry(layout Layout, viewport Viewport, screen Point, edgeTolerance float64) HitResult {
	viewport = viewport.Normalize()
	world := Point{X: (screen.X - viewport.PanX) / viewport.Zoom, Y: (screen.Y - viewport.PanY) / viewport.Zoom}
	for index := len(layout.Nodes) - 1; index >= 0; index-- {
		if layout.Nodes[index].Bounds.Contains(world) {
			return HitResult{Kind: HitNode, NodeID: layout.Nodes[index].Node.ID, World: world}
		}
	}
	if edgeTolerance < 0 || !finiteInteraction(edgeTolerance) {
		return HitResult{Kind: HitNone, World: world}
	}
	tolerance := edgeTolerance / viewport.Zoom
	edges := append([]EdgePlacement(nil), layout.Edges...)
	sort.Slice(edges, func(i, j int) bool { return edges[i].Edge.ID < edges[j].Edge.ID })
	for _, edge := range edges {
		// Four deterministic samples approximate the cubic curve for hit testing
		// without introducing browser geometry dependencies.
		previous := edge.Start
		for step := 1; step <= 16; step++ {
			t := float64(step) / 16
			point := cubicPoint(edge, t)
			if segmentDistance(world, previous, point) <= tolerance {
				return HitResult{Kind: HitEdge, EdgeID: edge.Edge.ID, World: world}
			}
			previous = point
		}
	}
	return HitResult{Kind: HitNone, World: world}
}

type Shape string
type BorderStyle string

const (
	ShapeRounded  Shape = "rounded-rectangle"
	ShapeDocument Shape = "document"
	ShapeHexagon  Shape = "hexagon"
	ShapePill     Shape = "pill"
	ShapeCylinder Shape = "cylinder"

	BorderSolid  BorderStyle = "solid"
	BorderDashed BorderStyle = "dashed"
	BorderDouble BorderStyle = "double"
)

type SemanticStyle struct {
	Shape       Shape
	StatusIcon  string
	StatusLabel string
	Border      BorderStyle
	Selected    bool
	Active      bool
}

func StyleFor(placement NodePlacement, selectedID, activeID string) SemanticStyle {
	status := strings.ToLower(strings.TrimSpace(placement.Node.Status))
	style := SemanticStyle{
		Shape: shapeForSemantic(placement.Semantic), StatusIcon: statusIcon(status),
		StatusLabel: humanStatus(status), Border: BorderSolid,
		Selected: placement.Node.ID == selectedID, Active: placement.Node.ID == activeID,
	}
	if status == "invalidated" {
		style.Border = BorderDashed
	}
	if style.Selected {
		style.Border = BorderDouble
	}
	return style
}

// truncationEllipsis marks a visually truncated graph-node label. It mirrors
// the unexported constant of the same name and value in
// internal/atomname/truncate.go so the two truncation surfaces stay visually
// identical.
const truncationEllipsis = "…"

// TruncateLabel derives the visual graph-node label for label, clipping only
// the returned display form and always preserving the full, untrimmed value
// as full for the tooltip, accessibility label, and full-label data hook
// (M21-165, M21-166). It never mutates or replaces the caller's copy of the
// node's real DisplayName — a stored node title or DisplayName read after
// calling this function is unaffected (M21-175's guarantee, reproduced here
// for the frontend rendering surfaces).
//
// The algorithm deliberately mirrors
// internal/atomname.TruncateGraphNodeLabel's tested contract rune-for-rune
// (see TestTruncateLabelMatchesAtomNameTruncationContract in
// reducers_test.go): a non-positive budget means "no truncation," a budget at
// or under the ellipsis width returns that many literal runes with no
// ellipsis, and a wider budget trims trailing spaces before appending the
// ellipsis. graphcanvas cannot call atomname.TruncateGraphNodeLabel directly:
// that function takes an atomname.DisplayName, and atomname exports no way to
// construct one from an arbitrary string — only DeriveDisplayName(CanonicalName)
// — because production node titles here are not yet backed by an
// atomname.AtomNameRecord (that wiring belongs to internal/graph's projector,
// out of scope for this frontend-only change).
func TruncateLabel(label string, maximumRunes int) (display, full string, clipped bool) {
	full = strings.TrimSpace(label)
	runes := []rune(full)
	if maximumRunes <= 0 || len(runes) <= maximumRunes {
		return full, full, false
	}
	ellipsisLength := utf8.RuneCountInString(truncationEllipsis)
	if maximumRunes <= ellipsisLength {
		return string(runes[:maximumRunes]), full, true
	}
	kept := strings.TrimRight(string(runes[:maximumRunes-ellipsisLength]), " ")
	return kept + truncationEllipsis, full, true
}

func layoutHasNode(layout Layout, id string) bool {
	if id == "" {
		return false
	}
	for _, node := range layout.Nodes {
		if node.Node.ID == id {
			return true
		}
	}
	return false
}

func cubicPoint(edge EdgePlacement, t float64) Point {
	oneMinus := 1 - t
	return Point{
		X: oneMinus*oneMinus*oneMinus*edge.Start.X + 3*oneMinus*oneMinus*t*edge.Control1.X + 3*oneMinus*t*t*edge.Control2.X + t*t*t*edge.Destination.X,
		Y: oneMinus*oneMinus*oneMinus*edge.Start.Y + 3*oneMinus*oneMinus*t*edge.Control1.Y + 3*oneMinus*t*t*edge.Control2.Y + t*t*t*edge.Destination.Y,
	}
}

func segmentDistance(point, start, end Point) float64 {
	dx, dy := end.X-start.X, end.Y-start.Y
	if dx == 0 && dy == 0 {
		return math.Hypot(point.X-start.X, point.Y-start.Y)
	}
	t := ((point.X-start.X)*dx + (point.Y-start.Y)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(point.X-(start.X+t*dx), point.Y-(start.Y+t*dy))
}

func finiteInteraction(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func shapeForSemantic(semantic NodeSemantic) Shape {
	switch semantic {
	case NodePlan:
		return ShapeDocument
	case NodeEvidence:
		return ShapePill
	case NodeMemory:
		return ShapeCylinder
	case NodeExternal:
		return ShapeHexagon
	default:
		return ShapeRounded
	}
}

func shapeLabel(shape Shape) string {
	switch shape {
	case ShapeDocument:
		return "document"
	case ShapeHexagon:
		return "hexagon"
	case ShapePill:
		return "pill"
	case ShapeCylinder:
		return "cylinder"
	default:
		return "rounded rectangle"
	}
}

func semanticLabel(semantic NodeSemantic) string {
	switch semantic {
	case NodeExternal:
		return "external tool"
	case NodePlan:
		return "plan"
	case NodeEvidence:
		return "evidence"
	case NodeMemory:
		return "memory"
	default:
		return "operation"
	}
}

func statusIcon(status string) string {
	return map[string]string{
		"pending": "○", "active": "▶", "passed": "✓", "warning": "!",
		"failed": "×", "blocked": "■", "invalidated": "↺",
	}[status]
}

func humanStatus(status string) string {
	if status == "" {
		return "Unknown"
	}
	runes := []rune(strings.ReplaceAll(status, "-", " "))
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}
