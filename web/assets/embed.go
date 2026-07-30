// Package assets embeds reviewed frontend source assets into the local
// Codeflux executable. Generated WASM may join this boundary only through the
// reproducible generation workflow.
package assets

import "embed"

// Files contains frontend assets served by the loopback application.
//
//go:embed static/*
var Files embed.FS
