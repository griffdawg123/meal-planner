package db_test

import (
	"context"
	"database/sql"
	"testing"

	mealdb "github.com/griffdawg123/meal-planner/db"
	// modernc.org/sqlite is pure Go, so self-hosters do not need a C toolchain.
	_ "modernc.org/sqlite"
)

func TestHouseholdMemberSchema(t *testing.T) {
	t.Run("creates a household with its first member", func(t *testing.T) {
		database := newTestDatabase(t)

		insertHousehold(t, database, "household-1", "member-1")
	})

	t.Run("rejects a creator from another household", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")

		tx, err := database.Begin()
		if err != nil {
			t.Fatalf("begin creator update: %v", err)
		}
		if _, err := tx.Exec(`UPDATE household SET created_by_member_id = ? WHERE id = ?`, "member-2", "household-1"); err != nil {
			t.Fatalf("update creator before deferred constraint check: %v", err)
		}
		if err := tx.Commit(); err == nil {
			t.Fatal("commit accepted a creator belonging to another household")
		}
	})

	t.Run("deleting a household cascades to its members", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		if _, err := database.Exec(`DELETE FROM household WHERE id = ?`, "household-1"); err != nil {
			t.Fatalf("delete household: %v", err)
		}

		var members int
		if err := database.QueryRow(`SELECT count(*) FROM member WHERE household_id = ?`, "household-1").Scan(&members); err != nil {
			t.Fatalf("count members: %v", err)
		}
		if members != 0 {
			t.Fatalf("members remaining after household deletion: got %d, want 0", members)
		}
	})
}

func newTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := mealdb.ApplySchema(context.Background(), database); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	return database
}

func insertHousehold(t *testing.T, database *sql.DB, householdID, memberID string) {
	t.Helper()

	tx, err := database.Begin()
	if err != nil {
		t.Fatalf("begin household creation: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO household (id, name, timezone, created_by_member_id) VALUES (?, ?, ?, ?)`,
		householdID, "Test Household", "Australia/Sydney", memberID,
	); err != nil {
		t.Fatalf("insert household: %v", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO member (id, household_id, name) VALUES (?, ?, ?)`,
		memberID, householdID, "Test Member",
	); err != nil {
		t.Fatalf("insert first member: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit household creation: %v", err)
	}
}
