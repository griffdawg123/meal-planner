package db_test

import (
	"context"
	"crypto/sha256"
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

func TestMagicLinkSchema(t *testing.T) {
	t.Run("stores an unused link for a member", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		insertMagicLink(t, database, tokenHash(1), "member-1", 100, 200)
	})

	t.Run("rejects a link for an unknown member", func(t *testing.T) {
		database := newTestDatabase(t)

		if err := execMagicLink(database, tokenHash(1), "missing-member", 100, 200); err == nil {
			t.Fatal("insert accepted a magic link for an unknown member: got nil error, want error")
		}
	})

	t.Run("rejects a token hash that is not a SHA-256 digest", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		if err := execMagicLink(database, []byte("raw-token"), "member-1", 100, 200); err == nil {
			t.Fatal("insert accepted a non-digest token hash: got nil error, want error")
		}
	})

	t.Run("rejects a link that expires before it is created", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		if err := execMagicLink(database, tokenHash(1), "member-1", 200, 200); err == nil {
			t.Fatal("insert accepted expires_at equal to created_at: got nil error, want error")
		}
	})

	t.Run("allows consuming a link within its lifetime", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertMagicLink(t, database, tokenHash(1), "member-1", 100, 200)

		if _, err := database.Exec(`UPDATE magic_link SET used_at = 150`); err != nil {
			t.Fatalf("consume magic link: got %v, want nil", err)
		}
	})

	t.Run("rejects consuming a link at or after its expiry", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertMagicLink(t, database, tokenHash(1), "member-1", 100, 200)

		for _, usedAt := range []int64{200, 201} {
			if _, err := database.Exec(`UPDATE magic_link SET used_at = ?`, usedAt); err == nil {
				t.Errorf("consume at %d: got nil error, want error", usedAt)
			}
		}
	})

	t.Run("rejects consuming a link before it was created", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertMagicLink(t, database, tokenHash(1), "member-1", 100, 200)

		if _, err := database.Exec(`UPDATE magic_link SET used_at = 99`); err == nil {
			t.Fatal("consume before creation: got nil error, want error")
		}
	})

	t.Run("rejects clearing or changing the use of a consumed link", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertMagicLink(t, database, tokenHash(1), "member-1", 100, 200)
		if _, err := database.Exec(`UPDATE magic_link SET used_at = 150`); err != nil {
			t.Fatalf("consume magic link: %v", err)
		}

		for _, statement := range []string{
			`UPDATE magic_link SET used_at = NULL`,
			`UPDATE magic_link SET used_at = 160`,
		} {
			if _, err := database.Exec(statement); err == nil {
				t.Errorf("%s: got nil error, want error", statement)
			}
		}

		var usedAt int64
		if err := database.QueryRow(`SELECT used_at FROM magic_link`).Scan(&usedAt); err != nil {
			t.Fatalf("read used_at: %v", err)
		}
		if usedAt != 150 {
			t.Errorf("used_at after rejected updates: got %d, want 150", usedAt)
		}
	})

	t.Run("deleting a member cascades to its magic links", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		if _, err := database.Exec(
			`INSERT INTO member (id, household_id, name) VALUES (?, ?, ?)`,
			"member-2", "household-1", "Second Member",
		); err != nil {
			t.Fatalf("insert second member: %v", err)
		}
		insertMagicLink(t, database, tokenHash(1), "member-2", 100, 200)

		if _, err := database.Exec(`DELETE FROM member WHERE id = ?`, "member-2"); err != nil {
			t.Fatalf("delete member: %v", err)
		}

		var links int
		if err := database.QueryRow(`SELECT count(*) FROM magic_link`).Scan(&links); err != nil {
			t.Fatalf("count magic links: %v", err)
		}
		if links != 0 {
			t.Errorf("magic links remaining after member deletion: got %d, want 0", links)
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

func insertMagicLink(t *testing.T, database *sql.DB, hash []byte, memberID string, createdAt, expiresAt int64) {
	t.Helper()

	if err := execMagicLink(database, hash, memberID, createdAt, expiresAt); err != nil {
		t.Fatalf("insert magic link: %v", err)
	}
}

func execMagicLink(database *sql.DB, hash []byte, memberID string, createdAt, expiresAt int64) error {
	_, err := database.Exec(
		`INSERT INTO magic_link (token_hash, member_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hash, memberID, createdAt, expiresAt,
	)
	return err
}

// tokenHash returns a distinct 32-byte value shaped like a SHA-256 digest.
func tokenHash(seed byte) []byte {
	hash := sha256.Sum256([]byte{seed})
	return hash[:]
}
