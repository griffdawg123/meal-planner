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

func TestPreferenceSchema(t *testing.T) {
	database := newTestDatabase(t)
	insertHousehold(t, database, "household-1", "member-1")
	insertHousehold(t, database, "household-2", "member-2")

	if _, err := database.Exec(`
		INSERT INTO preference (id, household_id, kind, category, value)
		VALUES (?, ?, ?, ?, ?)
	`, "preference-household", "household-1", "hard", "allergy", "peanuts"); err != nil {
		t.Fatalf("insert household preference: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO preference (id, household_id, member_id, kind, category, value, strength)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "preference-member", "household-1", "member-1", "soft", "cuisine", "Italian", 4); err != nil {
		t.Fatalf("insert member preference: %v", err)
	}

	var householdKind, memberKind string
	if err := database.QueryRow(`SELECT kind FROM preference WHERE id = ?`, "preference-household").Scan(&householdKind); err != nil {
		t.Fatalf("read household preference kind: %v", err)
	}
	if err := database.QueryRow(`SELECT kind FROM preference WHERE id = ?`, "preference-member").Scan(&memberKind); err != nil {
		t.Fatalf("read member preference kind: %v", err)
	}
	if householdKind != "hard" || memberKind != "soft" {
		t.Fatalf("preference kinds: got household=%q member=%q, want household=%q member=%q", householdKind, memberKind, "hard", "soft")
	}

	if _, err := database.Exec(`
		INSERT INTO preference (id, household_id, member_id, kind, category, value, strength)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "preference-wrong-household", "household-1", "member-2", "soft", "dislike", "mushrooms", 3); err == nil {
		t.Fatal("insert accepted a preference for a member belonging to another household")
	}
}

func TestAwayNightSchema(t *testing.T) {
	t.Run("records the nights a member is away", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		insertAwayNight(t, database, "household-1", "member-1", "2026-10-06")
		insertAwayNight(t, database, "household-1", "member-1", "2026-10-08")

		rows, err := database.Query(
			`SELECT night FROM away_night WHERE household_id = ? AND member_id = ? ORDER BY night`,
			"household-1", "member-1",
		)
		if err != nil {
			t.Fatalf("query away nights: %v", err)
		}
		defer rows.Close()

		var nights []string
		for rows.Next() {
			var night string
			if err := rows.Scan(&night); err != nil {
				t.Fatalf("scan away night: %v", err)
			}
			nights = append(nights, night)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate away nights: %v", err)
		}
		if len(nights) != 2 || nights[0] != "2026-10-06" || nights[1] != "2026-10-08" {
			t.Fatalf("away nights: got %q, want %q", nights, []string{"2026-10-06", "2026-10-08"})
		}
	})

	t.Run("rejects recording the same member away twice on one night", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertAwayNight(t, database, "household-1", "member-1", "2026-10-06")

		if _, err := database.Exec(
			`INSERT INTO away_night (household_id, member_id, night) VALUES (?, ?, ?)`,
			"household-1", "member-1", "2026-10-06",
		); err == nil {
			t.Fatal("insert accepted a duplicate away night for the same member")
		}
	})

	t.Run("rejects an away night for a member of another household", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")

		if _, err := database.Exec(
			`INSERT INTO away_night (household_id, member_id, night) VALUES (?, ?, ?)`,
			"household-1", "member-2", "2026-10-06",
		); err == nil {
			t.Fatal("insert accepted an away night for a member belonging to another household")
		}
	})

	t.Run("rejects nights that are not calendar dates", func(t *testing.T) {
		for _, night := range []string{"", "Tuesday", "2026-10-6", "2026-02-30", "2026-10-06T18:00:00", " 2026-10-06"} {
			database := newTestDatabase(t)
			insertHousehold(t, database, "household-1", "member-1")

			if _, err := database.Exec(
				`INSERT INTO away_night (household_id, member_id, night) VALUES (?, ?, ?)`,
				"household-1", "member-1", night,
			); err == nil {
				t.Errorf("insert accepted away night %q, want rejection", night)
			}
		}
	})

	t.Run("marking a member present again removes only that night", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertAwayNight(t, database, "household-1", "member-1", "2026-10-06")
		insertAwayNight(t, database, "household-1", "member-1", "2026-10-07")

		if _, err := database.Exec(
			`DELETE FROM away_night WHERE member_id = ? AND night = ?`, "member-1", "2026-10-06",
		); err != nil {
			t.Fatalf("delete away night: %v", err)
		}

		var remaining string
		if err := database.QueryRow(
			`SELECT group_concat(night) FROM away_night WHERE member_id = ?`, "member-1",
		).Scan(&remaining); err != nil {
			t.Fatalf("read remaining away nights: %v", err)
		}
		if remaining != "2026-10-07" {
			t.Fatalf("remaining away nights: got %q, want %q", remaining, "2026-10-07")
		}
	})

	t.Run("deleting a household cascades to its away nights", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertAwayNight(t, database, "household-1", "member-1", "2026-10-06")

		if _, err := database.Exec(`DELETE FROM household WHERE id = ?`, "household-1"); err != nil {
			t.Fatalf("delete household: %v", err)
		}

		var awayNights int
		if err := database.QueryRow(`SELECT count(*) FROM away_night`).Scan(&awayNights); err != nil {
			t.Fatalf("count away nights: %v", err)
		}
		if awayNights != 0 {
			t.Fatalf("away nights remaining after household deletion: got %d, want 0", awayNights)
		}
	})
}

func TestApplySchemaCanBeReapplied(t *testing.T) {
	database := newTestDatabase(t)
	insertHousehold(t, database, "household-1", "member-1")

	var householdCreatedAt, memberCreatedAt string
	if err := database.QueryRow(
		`SELECT created_at FROM household WHERE id = ?`, "household-1",
	).Scan(&householdCreatedAt); err != nil {
		t.Fatalf("read household creation time: %v", err)
	}
	if err := database.QueryRow(
		`SELECT created_at FROM member WHERE id = ?`, "member-1",
	).Scan(&memberCreatedAt); err != nil {
		t.Fatalf("read member creation time: %v", err)
	}

	if err := mealdb.ApplySchema(context.Background(), database); err != nil {
		t.Fatalf("reapply schema: %v", err)
	}

	var householdID, householdName, timezone, creatorID, gotHouseholdCreatedAt string
	if err := database.QueryRow(
		`SELECT id, name, timezone, created_by_member_id, created_at FROM household WHERE id = ?`,
		"household-1",
	).Scan(&householdID, &householdName, &timezone, &creatorID, &gotHouseholdCreatedAt); err != nil {
		t.Fatalf("read household after schema reapplication: %v", err)
	}
	if householdID != "household-1" || householdName != "Test Household" || timezone != "Australia/Sydney" || creatorID != "member-1" || gotHouseholdCreatedAt != householdCreatedAt {
		t.Fatalf(
			"household changed after schema reapplication: got (%q, %q, %q, %q, %q), want (%q, %q, %q, %q, %q)",
			householdID, householdName, timezone, creatorID, gotHouseholdCreatedAt,
			"household-1", "Test Household", "Australia/Sydney", "member-1", householdCreatedAt,
		)
	}

	var memberID, memberHouseholdID, memberName, gotMemberCreatedAt string
	if err := database.QueryRow(
		`SELECT id, household_id, name, created_at FROM member WHERE id = ?`,
		"member-1",
	).Scan(&memberID, &memberHouseholdID, &memberName, &gotMemberCreatedAt); err != nil {
		t.Fatalf("read member after schema reapplication: %v", err)
	}
	if memberID != "member-1" || memberHouseholdID != "household-1" || memberName != "Test Member" || gotMemberCreatedAt != memberCreatedAt {
		t.Fatalf(
			"member changed after schema reapplication: got (%q, %q, %q, %q), want (%q, %q, %q, %q)",
			memberID, memberHouseholdID, memberName, gotMemberCreatedAt,
			"member-1", "household-1", "Test Member", memberCreatedAt,
		)
	}
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

func insertAwayNight(t *testing.T, database *sql.DB, householdID, memberID, night string) {
	t.Helper()

	if _, err := database.Exec(
		`INSERT INTO away_night (household_id, member_id, night) VALUES (?, ?, ?)`,
		householdID, memberID, night,
	); err != nil {
		t.Fatalf("insert away night %s for %s: %v", night, memberID, err)
	}
}
