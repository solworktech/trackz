// Package trackz exposes the canonical database schema so that every binary
// creates exactly the same table as trackz_schema.sql.
package trackz

import _ "embed"

// Schema is the contents of trackz_schema.sql.
//
//go:embed trackz_schema.sql
var Schema string
