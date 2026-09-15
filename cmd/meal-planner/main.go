package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/url"
	"path/filepath"

	mealdb "github.com/griffdawg123/meal-planner/db"
	_ "modernc.org/sqlite"
)

func main() {
	databasePath := flag.String("db", "meal-planner.db", "path to the SQLite database file")
	flag.Parse()

	if err := initializeDatabase(context.Background(), *databasePath); err != nil {
		log.Fatalf("initialize database: %v", err)
	}

	log.Printf("database ready: %s", *databasePath)
}

func initializeDatabase(ctx context.Context, databasePath string) error {
	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		return fmt.Errorf("resolve database path: %w", err)
	}

	dsn := (&url.URL{
		Scheme:   "file",
		Path:     absolutePath,
		RawQuery: "_foreign_keys=on",
	}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if err := mealdb.ApplySchema(ctx, database); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	return nil
}
