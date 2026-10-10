// Package migrations embeds the SQL migrations so tests (and tools) can apply
// them without the migrate CLI.
package migrations

import "embed"

// FS holds every *.up.sql / *.down.sql file.
//
//go:embed *.sql
var FS embed.FS
