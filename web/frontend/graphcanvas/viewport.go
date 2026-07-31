package graphcanvas

import "math"

const (
	MinimumZoom = 0.35
	MaximumZoom = 2.40
)

type Viewport struct {
	PanX, PanY float64
	Zoom       float64
}

func (viewport Viewport) Normalize() Viewport {
	if math.IsNaN(viewport.PanX) || math.IsInf(viewport.PanX, 0) {
		viewport.PanX = 0
	}
	if math.IsNaN(viewport.PanY) || math.IsInf(viewport.PanY, 0) {
		viewport.PanY = 0
	}
	if math.IsNaN(viewport.Zoom) || math.IsInf(viewport.Zoom, 0) || viewport.Zoom == 0 {
		viewport.Zoom = 1
	}
	viewport.Zoom = math.Max(MinimumZoom, math.Min(MaximumZoom, viewport.Zoom))
	return viewport
}

func FitViewport(bounds Rect, width, height, padding float64) Viewport {
	if width <= 0 || height <= 0 || bounds.Width <= 0 || bounds.Height <= 0 {
		return Viewport{Zoom: 1}
	}
	availableWidth := math.Max(width-2*padding, 1)
	availableHeight := math.Max(height-2*padding, 1)
	zoom := math.Min(availableWidth/bounds.Width, availableHeight/bounds.Height)
	zoom = math.Max(MinimumZoom, math.Min(MaximumZoom, zoom))
	return Viewport{
		PanX: (width-bounds.Width*zoom)/2 - bounds.X*zoom,
		PanY: (height-bounds.Height*zoom)/2 - bounds.Y*zoom,
		Zoom: zoom,
	}
}

func PanViewport(viewport Viewport, deltaX, deltaY float64) Viewport {
	viewport = viewport.Normalize()
	viewport.PanX += deltaX
	viewport.PanY += deltaY
	return viewport
}

func ZoomViewportAt(viewport Viewport, factor float64, anchor Point) Viewport {
	viewport = viewport.Normalize()
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return viewport
	}
	world := Point{
		X: (anchor.X - viewport.PanX) / viewport.Zoom,
		Y: (anchor.Y - viewport.PanY) / viewport.Zoom,
	}
	nextZoom := math.Max(MinimumZoom, math.Min(MaximumZoom, viewport.Zoom*factor))
	viewport.PanX = anchor.X - world.X*nextZoom
	viewport.PanY = anchor.Y - world.Y*nextZoom
	viewport.Zoom = nextZoom
	return viewport
}

func ScreenRect(bounds Rect, viewport Viewport) Rect {
	viewport = viewport.Normalize()
	return Rect{
		X:      viewport.PanX + bounds.X*viewport.Zoom,
		Y:      viewport.PanY + bounds.Y*viewport.Zoom,
		Width:  bounds.Width * viewport.Zoom,
		Height: bounds.Height * viewport.Zoom,
	}
}

func HitTest(layout Layout, viewport Viewport, screen Point) (string, bool) {
	viewport = viewport.Normalize()
	world := Point{X: (screen.X - viewport.PanX) / viewport.Zoom, Y: (screen.Y - viewport.PanY) / viewport.Zoom}
	for index := len(layout.Nodes) - 1; index >= 0; index-- {
		if layout.Nodes[index].Bounds.Contains(world) {
			return layout.Nodes[index].Node.ID, true
		}
	}
	return "", false
}

type BackingStoreSize struct {
	Width, Height int
	DPR           float64
}

func BackingStore(width, height, devicePixelRatio float64) BackingStoreSize {
	if devicePixelRatio < 1 || math.IsNaN(devicePixelRatio) || math.IsInf(devicePixelRatio, 0) {
		devicePixelRatio = 1
	}
	devicePixelRatio = math.Min(devicePixelRatio, 4)
	return BackingStoreSize{
		Width:  max(1, int(math.Round(math.Max(width, 1)*devicePixelRatio))),
		Height: max(1, int(math.Round(math.Max(height, 1)*devicePixelRatio))),
		DPR:    devicePixelRatio,
	}
}
