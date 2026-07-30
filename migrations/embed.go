package migrations

import "embed"

// Files contains immutable, ordered SQL migration source compiled into the
// Codeflux executable.
//
//go:embed *.sql
var Files embed.FS
