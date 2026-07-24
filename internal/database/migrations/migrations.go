package migrations

import "embed"

// Files contains the versioned database schema used by the worker and tests.
//
//go:embed *.sql
var Files embed.FS
