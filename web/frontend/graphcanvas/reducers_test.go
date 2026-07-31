package graphcanvas

import (
	"math"
	"testing"

	"codeflux.dev/codeflux/web/frontend/state"
)

func TestViewportReducersPreserveZoomAnchorAndResizeCenter(t *testing.T) {
	viewport := PanViewport(Viewport{Zoom: 1}, 25, -10)
	anchor := Point{X: 300, Y: 200}
	worldBefore := Point{X: (anchor.X - viewport.PanX) / viewport.Zoom, Y: (anchor.Y - viewport.PanY) / viewport.Zoom}
	viewport = ZoomViewportAt(viewport, 1.75, anchor)
	worldAfter := Point{X: (anchor.X - viewport.PanX) / viewport.Zoom, Y: (anchor.Y - viewport.PanY) / viewport.Zoom}
	if !closePoint(worldBefore, worldAfter) {
		t.Fatalf("zoom anchor moved from %#v to %#v", worldBefore, worldAfter)
	}
	centerBefore := Point{X: (400 - viewport.PanX) / viewport.Zoom, Y: (300 - viewport.PanY) / viewport.Zoom}
	resized := ResizeViewport(viewport, 800, 600, 1200, 720)
	centerAfter := Point{X: (600 - resized.PanX) / resized.Zoom, Y: (360 - resized.PanY) / resized.Zoom}
	if !closePoint(centerBefore, centerAfter) {
		t.Fatalf("resize center moved from %#v to %#v", centerBefore, centerAfter)
	}
	if ResetViewport() != (Viewport{Zoom: 1}) {
		t.Fatalf("reset viewport = %#v", ResetViewport())
	}
}

func TestGeometryHitSelectionStyleAndLabelContracts(t *testing.T) {
	layout, err := BuildLayout([]state.GraphNodeView{
		{ID: "plan", Title: "Plan", Status: "invalidated"},
		{ID: "evidence", Title: "Validation evidence", Status: "passed"},
	}, []Edge{{ID: "edge", FromID: "plan", ToID: "evidence", Kind: EdgeEvidence}})
	if err != nil {
		t.Fatal(err)
	}
	viewport := Viewport{Zoom: 2}.Normalize()
	plan := placement(layout, "plan")
	center := plan.Bounds.Center()
	screenCenter := Point{X: center.X*viewport.Zoom + viewport.PanX, Y: center.Y*viewport.Zoom + viewport.PanY}
	hit := HitTestGeometry(layout, viewport, screenCenter, 8)
	if hit.Kind != HitNode || hit.NodeID != "plan" {
		t.Fatalf("node hit = %#v", hit)
	}
	edgePoint := cubicPoint(layout.Edges[0], 0.5)
	edgeHit := HitTestGeometry(layout, viewport, Point{X: edgePoint.X * viewport.Zoom, Y: edgePoint.Y * viewport.Zoom}, 8)
	if edgeHit.Kind != HitEdge || edgeHit.EdgeID != "edge" {
		t.Fatalf("edge hit = %#v", edgeHit)
	}
	interaction := Interaction{Viewport: viewport}
	interaction, ok := SelectNode(interaction, layout, "plan")
	if !ok {
		t.Fatal("stable node selection failed")
	}
	interaction, ok = ActivateNode(interaction, layout, "evidence")
	if !ok || interaction.SelectedID != "plan" || interaction.ActiveID != "evidence" {
		t.Fatalf("interaction = %#v", interaction)
	}
	style := StyleFor(plan, interaction.SelectedID, interaction.ActiveID)
	if style.Shape != ShapeDocument || style.StatusIcon == "" || style.StatusLabel != "Invalidated" || style.Border != BorderDouble || !style.Selected {
		t.Fatalf("semantic style = %#v", style)
	}
	display, full, clipped := TruncateLabel("ValidateProviderEvidenceAcrossRevisions界面", 12)
	if !clipped || display != "ValidatePro…" || full != "ValidateProviderEvidenceAcrossRevisions界面" {
		t.Fatalf("label = %q full=%q clipped=%t", display, full, clipped)
	}
	withoutPlan, _ := BuildLayout([]state.GraphNodeView{{ID: "evidence", Title: "Validation evidence", Status: "passed"}}, nil)
	reconciled := ReconcileInteraction(interaction, withoutPlan)
	if reconciled.SelectedID != "" || reconciled.ActiveID != "evidence" || reconciled.Viewport != viewport {
		t.Fatalf("revision reconciliation = %#v", reconciled)
	}
}

func TestFitAndBackingStoreAreBounded(t *testing.T) {
	viewport := FitViewport(Rect{X: 100, Y: 50, Width: 1000, Height: 500}, 800, 600, 40)
	topLeft := ScreenRect(Rect{X: 100, Y: 50}, viewport)
	if topLeft.X < 39.99 || topLeft.Y < 39.99 || viewport.Zoom < MinimumZoom || viewport.Zoom > MaximumZoom {
		t.Fatalf("fit viewport = %#v top-left=%#v", viewport, topLeft)
	}
	backing := BackingStore(400, 300, 10)
	if backing.Width != 1600 || backing.Height != 1200 || backing.DPR != 4 {
		t.Fatalf("backing store = %#v", backing)
	}
}

func TestEveryNodePurposeHasADistinctShape(t *testing.T) {
	expected := map[NodeSemantic]Shape{
		NodeOperation: ShapeRounded,
		NodeExternal:  ShapeHexagon,
		NodePlan:      ShapeDocument,
		NodeEvidence:  ShapePill,
		NodeMemory:    ShapeCylinder,
	}
	seen := make(map[Shape]NodeSemantic, len(expected))
	for purpose, want := range expected {
		got := shapeForSemantic(purpose)
		if got != want {
			t.Fatalf("%s shape = %s, want %s", purpose, got, want)
		}
		if previous, duplicate := seen[got]; duplicate {
			t.Fatalf("%s and %s share shape %s", previous, purpose, got)
		}
		seen[got] = purpose
		if shapeLabel(got) == "" || semanticLabel(purpose) == "" {
			t.Fatalf("%s shape lacks an accessible purpose description", purpose)
		}
	}
}

func closePoint(left, right Point) bool {
	return math.Abs(left.X-right.X) < 1e-9 && math.Abs(left.Y-right.Y) < 1e-9
}
