package shell

// RenderProbe is the development-build hook for component render counts.
// Production callers leave it nil and pay only one nil check per boundary.
type RenderProbe interface {
	Rendered(boundary string, revision uint64)
}

func recordRender(probe RenderProbe, boundary string, revision uint64) {
	if probe != nil {
		probe.Rendered(boundary, revision)
	}
}
