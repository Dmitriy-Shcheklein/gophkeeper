// Package migrations embeds the SQL migration files so they can be
// applied programmatically with golang-migrate (iofs source), making
// the server binary self-contained.
package migrations

import "embed"

// FS holds all migration files (000001_*.up.sql, ...) from this
// directory, ready for source/iofs.New(FS, ".").
//
//go:embed *.sql
var FS embed.FS
