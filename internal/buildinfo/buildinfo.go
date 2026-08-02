// Package buildinfo exposes the exact source, toolchain, schema, and frontend
// identity compiled into a Codeflux executable.
package buildinfo

import "runtime"

var (
	version         = "0.0.1"
	commit          = "unknown"
	buildDate       = "unknown"
	frontendVersion = generatedFrontendVersion
)

// Info is the structured identity of one Codeflux executable.
type Info struct {
	Version         string
	Commit          string
	BuildDate       string
	GoVersion       string
	SchemaVersion   uint32
	FrontendVersion string
}

// Current returns the immutable identity compiled into the running executable.
func Current() Info {
	return Info{
		Version:         version,
		Commit:          commit,
		BuildDate:       buildDate,
		GoVersion:       runtime.Version(),
		SchemaVersion:   generatedSchemaVersion,
		FrontendVersion: frontendVersion,
	}
}
