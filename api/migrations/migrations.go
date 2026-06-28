package migrations

import "embed"

// FS contains SQL migrations applied by the backend at startup.
//
//go:embed *.sql
var FS embed.FS
