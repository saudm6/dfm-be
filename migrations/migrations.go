// Package migrations holds the goose SQL migrations of the service database.
// New files use goose's timestamp version prefix (YYYYMMDDHHMMSS_name.sql).
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
