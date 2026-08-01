package graphcanvas

import (
	"math"
	"testing"

	"codeflux.dev/codeflux/internal/atomname"
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

// TestTruncateLabelMatchesAtomNameTruncationContract proves graphcanvas's
// visual-label truncation (M21-166) is not an independent reimplementation
// that could silently drift from internal/atomname.TruncateGraphNodeLabel's
// tested contract (M21-175): for the same text and budget, both functions
// must agree rune-for-rune on the truncated display text and the Truncated
// flag, and TruncateLabel's "full" return must always equal the untruncated
// input regardless of the budget.
func TestTruncateLabelMatchesAtomNameTruncationContract(t *testing.T) {
	canonical, err := atomname.NewCanonicalName("ReserveAccountFundsUntilAuthorizationExpires")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	display := atomname.DeriveDisplayName(canonical)
	text := display.String()
	runeCount := len([]rune(text))

	for _, budget := range []int{-5, 0, 1, 2, 11, 12, runeCount - 1, runeCount, runeCount + 5} {
		want := atomname.TruncateGraphNodeLabel(display, budget)
		gotDisplay, gotFull, gotClipped := TruncateLabel(text, budget)
		if gotDisplay != want.Text || gotClipped != want.Truncated {
			t.Errorf("budget %d: TruncateLabel(%q) = (%q, clipped=%t), want (%q, clipped=%t) from atomname.TruncateGraphNodeLabel",
				budget, text, gotDisplay, gotClipped, want.Text, want.Truncated)
		}
		if gotFull != text {
			t.Errorf("budget %d: full label = %q, want the untruncated %q", budget, gotFull, text)
		}
	}
}

// TestTruncateLabelBoundaryCases exercises the M21-166 boundary behaviors: a
// name longer than the label budget is clipped, a name exactly at the budget
// is not, and a non-positive budget (no rendering room at all) leaves the
// full name untouched rather than applying a silent default truncation.
func TestTruncateLabelBoundaryCases(t *testing.T) {
	const longName = "ValidateProviderEvidenceAcrossRevisions"
	runeCount := len([]rune(longName))

	if display, full, clipped := TruncateLabel(longName, runeCount); clipped || display != full || display != longName {
		t.Fatalf("name exactly at budget must not be truncated, got display=%q full=%q clipped=%t", display, full, clipped)
	}
	if display, full, clipped := TruncateLabel(longName, runeCount-1); !clipped || display == full || full != longName {
		t.Fatalf("name longer than budget must be truncated while preserving the full name, got display=%q full=%q clipped=%t", display, full, clipped)
	}
	if display, full, clipped := TruncateLabel(longName, 0); clipped || display != longName || full != longName {
		t.Fatalf("non-positive budget must skip truncation entirely, got display=%q full=%q clipped=%t", display, full, clipped)
	}
	if display, full, clipped := TruncateLabel(longName, -3); clipped || display != longName || full != longName {
		t.Fatalf("negative budget must skip truncation entirely, got display=%q full=%q clipped=%t", display, full, clipped)
	}
}

func closePoint(left, right Point) bool {
	return math.Abs(left.X-right.X) < 1e-9 && math.Abs(left.Y-right.Y) < 1e-9
}
