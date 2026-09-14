// Package db owns the database schema and schema installation.
package db

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed schema.sql
var schema string

// ApplySchema installs the embedded schema in database.
func ApplySchema(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, schema)
	return err
}
