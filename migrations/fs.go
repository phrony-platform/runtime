package migrations

import "embed"

// FS holds versioned SQL migrations applied by the runtime (see golang-migrate).
//
//go:embed *.sql
var FS embed.FS
