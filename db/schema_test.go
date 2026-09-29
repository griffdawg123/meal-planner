package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"strings"
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

	t.Run("rejects a token hash that is null or not a blob", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		assertTokenHashRequired(t, database)
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

	t.Run("rejects timestamps that are not integers", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		for _, values := range [][2]any{
			{"never", 200},
			{100, "never"},
			{100, 200.5},
		} {
			if _, err := database.Exec(
				`INSERT INTO magic_link (token_hash, member_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
				tokenHash(1), "member-1", values[0], values[1],
			); err == nil {
				t.Errorf("insert created_at=%v expires_at=%v: got nil error, want error", values[0], values[1])
			}
		}

		insertMagicLink(t, database, tokenHash(2), "member-1", 100, 200)
		for _, statement := range []string{
			`UPDATE magic_link SET expires_at = 'never'`,
			`UPDATE magic_link SET used_at = 'never'`,
			`UPDATE magic_link SET used_at = 150.5`,
		} {
			if _, err := database.Exec(statement); err == nil {
				t.Errorf("%s: got nil error, want error", statement)
			}
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

func TestApplySchemaUpgradesMagicLinkLifecycle(t *testing.T) {
	// The fixture is the schema as released before the magic-link lifecycle
	// rules, so existing databases must gain them when the schema is reapplied.
	legacySchema, err := os.ReadFile("testdata/schema_before_magic_link_lifecycle.sql")
	if err != nil {
		t.Fatalf("read legacy schema: %v", err)
	}
	database := openTestDatabase(t)
	if _, err := database.Exec(string(legacySchema)); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	insertHousehold(t, database, "household-1", "member-1")
	insertMagicLink(t, database, tokenHash(1), "member-1", 100, 200)

	if err := mealdb.ApplySchema(context.Background(), database); err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}

	for _, statement := range []string{
		`UPDATE magic_link SET used_at = 201`,
		`UPDATE magic_link SET used_at = 99`,
		`UPDATE magic_link SET expires_at = 'never'`,
	} {
		if _, err := database.Exec(statement); err == nil {
			t.Errorf("%s: got nil error, want error", statement)
		}
	}
	if err := execMagicLink(database, tokenHash(2), "member-1", 100, 200); err != nil {
		t.Fatalf("insert magic link after upgrade: got %v, want nil", err)
	}
	if _, err := database.Exec(
		`INSERT INTO magic_link (token_hash, member_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash(3), "member-1", 100, "never",
	); err == nil {
		t.Error("insert non-expiring magic link after upgrade: got nil error, want error")
	}
	assertTokenHashRequired(t, database)

	if _, err := database.Exec(`UPDATE magic_link SET used_at = 150 WHERE token_hash = ?`, tokenHash(1)); err != nil {
		t.Fatalf("consume magic link after upgrade: got %v, want nil", err)
	}
	if _, err := database.Exec(`UPDATE magic_link SET used_at = NULL WHERE token_hash = ?`, tokenHash(1)); err == nil {
		t.Error("reset consumed magic link after upgrade: got nil error, want error")
	}
}

func TestApplySchemaDiscardsInvalidLegacyMagicLinks(t *testing.T) {
	legacySchema, err := os.ReadFile("testdata/schema_before_magic_link_lifecycle.sql")
	if err != nil {
		t.Fatalf("read legacy schema: %v", err)
	}
	database := openTestDatabase(t)
	if _, err := database.Exec(string(legacySchema)); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	insertHousehold(t, database, "household-1", "member-1")

	// Every row below was accepted by the legacy schema's constraints.
	legacyRows := []struct {
		name      string
		hash      any
		createdAt any
		expiresAt any
		usedAt    any
	}{
		{"valid unused link", tokenHash(1), 100, 200, nil},
		{"valid consumed link", tokenHash(2), 100, 200, 150},
		{"non-expiring link", tokenHash(3), 100, "never", nil},
		{"fractional created_at", tokenHash(4), 100.5, 200, nil},
		{"null token hash", nil, 100, 200, nil},
		{"text token hash", strings.Repeat("x", 32), 100, 200, nil},
		{"consumed at expiry", tokenHash(5), 100, 200, 200},
		{"consumed before creation", tokenHash(6), 100, 200, 99},
		{"text used_at", tokenHash(7), 100, 200, "soon"},
	}
	for _, row := range legacyRows {
		if _, err := database.Exec(
			`INSERT INTO magic_link (token_hash, member_id, created_at, expires_at, used_at) VALUES (?, ?, ?, ?, ?)`,
			row.hash, "member-1", row.createdAt, row.expiresAt, row.usedAt,
		); err != nil {
			t.Fatalf("insert legacy %s: %v", row.name, err)
		}
	}

	if err := mealdb.ApplySchema(context.Background(), database); err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}

	rows, err := database.Query(`SELECT token_hash FROM magic_link ORDER BY created_at, used_at`)
	if err != nil {
		t.Fatalf("query magic links: %v", err)
	}
	defer rows.Close()

	var remaining [][]byte
	for rows.Next() {
		var hash []byte
		if err := rows.Scan(&hash); err != nil {
			t.Fatalf("scan magic link: %v", err)
		}
		remaining = append(remaining, hash)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate magic links: %v", err)
	}

	want := [][]byte{tokenHash(1), tokenHash(2)}
	if len(remaining) != len(want) || !bytes.Equal(remaining[0], want[0]) || !bytes.Equal(remaining[1], want[1]) {
		t.Fatalf("magic links after upgrade: got %x, want %x", remaining, want)
	}
}

func newTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database := openTestDatabase(t)
	if err := mealdb.ApplySchema(context.Background(), database); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	return database
}

func openTestDatabase(t *testing.T) *sql.DB {
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
	return execMagicLinkValue(database, hash, memberID, createdAt, expiresAt)
}

func execMagicLinkValue(database *sql.DB, hash any, memberID string, createdAt, expiresAt int64) error {
	_, err := database.Exec(
		`INSERT INTO magic_link (token_hash, member_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hash, memberID, createdAt, expiresAt,
	)
	return err
}

// assertTokenHashRequired checks that magic links cannot be stored or updated
// with a token hash that is NULL or a 32-character string rather than a
// 32-byte digest. The database must contain member-1.
func assertTokenHashRequired(t *testing.T, database *sql.DB) {
	t.Helper()

	textHash := strings.Repeat("a", 32)
	for _, hash := range []any{nil, textHash} {
		for attempt := 0; attempt < 2; attempt++ {
			if err := execMagicLinkValue(database, hash, "member-1", 100, 200); err == nil {
				t.Errorf("insert token_hash=%v (attempt %d): got nil error, want error", hash, attempt+1)
			}
		}
	}

	valid := tokenHash(99)
	insertMagicLink(t, database, valid, "member-1", 100, 200)
	for _, hash := range []any{nil, textHash} {
		if _, err := database.Exec(
			`UPDATE magic_link SET token_hash = ? WHERE token_hash = ?`, hash, valid,
		); err == nil {
			t.Errorf("update token_hash to %v: got nil error, want error", hash)
		}
	}

	var invalid int
	if err := database.QueryRow(
		`SELECT count(*) FROM magic_link WHERE typeof(token_hash) <> 'blob' OR length(token_hash) <> 32`,
	).Scan(&invalid); err != nil {
		t.Fatalf("count invalid token hashes: %v", err)
	}
	if invalid != 0 {
		t.Errorf("magic links with invalid token hashes: got %d, want 0", invalid)
	}
}

// tokenHash returns a distinct 32-byte value shaped like a SHA-256 digest.
func tokenHash(seed byte) []byte {
	hash := sha256.Sum256([]byte{seed})
	return hash[:]
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

func insertAwayNight(t *testing.T, database *sql.DB, householdID, memberID, night string) {
	t.Helper()

	if _, err := database.Exec(
		`INSERT INTO away_night (household_id, member_id, night) VALUES (?, ?, ?)`,
		householdID, memberID, night,
	); err != nil {
		t.Fatalf("insert away night %s for %s: %v", night, memberID, err)
	}
}

func TestTelegramIdentitySchema(t *testing.T) {
	t.Run("links a telegram user to a member and resolves their household", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertTelegramIdentity(t, database, 123456789, "member-1")

		var householdID, memberID string
		if err := database.QueryRow(`
			SELECT member.household_id, member.id
			FROM telegram_identity
			JOIN member ON member.id = telegram_identity.member_id
			WHERE telegram_identity.telegram_user_id = ?
		`, 123456789).Scan(&householdID, &memberID); err != nil {
			t.Fatalf("resolve telegram identity: %v", err)
		}
		if householdID != "household-1" || memberID != "member-1" {
			t.Fatalf("resolved identity: got (%q, %q), want (%q, %q)", householdID, memberID, "household-1", "member-1")
		}
	})

	t.Run("an unlinked telegram user resolves to no member", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertTelegramIdentity(t, database, 123456789, "member-1")

		var memberID string
		err := database.QueryRow(
			`SELECT member_id FROM telegram_identity WHERE telegram_user_id = ?`, 987654321,
		).Scan(&memberID)
		if err != sql.ErrNoRows {
			t.Fatalf("resolve unlinked telegram user: got %v, want %v", err, sql.ErrNoRows)
		}
	})

	t.Run("rejects linking one telegram user to two members", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")
		insertTelegramIdentity(t, database, 123456789, "member-1")

		if err := execTelegramIdentity(database, 123456789, "member-2", 100); err == nil {
			t.Fatal("insert telegram user linked to a second member: got nil error, want error")
		}
	})

	t.Run("rejects linking two telegram users to one member", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertTelegramIdentity(t, database, 123456789, "member-1")

		if err := execTelegramIdentity(database, 987654321, "member-1", 100); err == nil {
			t.Fatal("insert second telegram user for one member: got nil error, want error")
		}
	})

	t.Run("rejects a link for an unknown member", func(t *testing.T) {
		database := newTestDatabase(t)

		if err := execTelegramIdentity(database, 123456789, "missing-member", 100); err == nil {
			t.Fatal("insert telegram identity for an unknown member: got nil error, want error")
		}
	})

	t.Run("rejects telegram user ids that are not positive integers", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		for _, userID := range []any{nil, 0, -5, 12.5, "alice"} {
			if err := execTelegramIdentity(database, userID, "member-1", 100); err == nil {
				t.Errorf("insert telegram_user_id=%v: got nil error, want error", userID)
			}
		}
	})

	t.Run("rejects a link time that is not an integer", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		for _, linkedAt := range []any{nil, "now", 100.5} {
			if err := execTelegramIdentity(database, 123456789, "member-1", linkedAt); err == nil {
				t.Errorf("insert linked_at=%v: got nil error, want error", linkedAt)
			}
		}
	})

	t.Run("deleting a member cascades to its telegram identity", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertSecondMember(t, database, "household-1", "member-2")
		insertTelegramIdentity(t, database, 123456789, "member-2")

		if _, err := database.Exec(`DELETE FROM member WHERE id = ?`, "member-2"); err != nil {
			t.Fatalf("delete member: %v", err)
		}

		assertRowCount(t, database, "telegram_identity", 0)
	})
}

func TestTelegramLinkCodeSchema(t *testing.T) {
	t.Run("stores an unused code for a member", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		insertTelegramLinkCode(t, database, tokenHash(1), "member-1", 100, 200)
	})

	t.Run("rejects a code for an unknown member", func(t *testing.T) {
		database := newTestDatabase(t)

		if err := execTelegramLinkCode(database, tokenHash(1), "missing-member", 100, 200); err == nil {
			t.Fatal("insert link code for an unknown member: got nil error, want error")
		}
	})

	t.Run("rejects a code hash that is not a SHA-256 digest", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		for _, hash := range []any{nil, []byte("ABCD-1234"), strings.Repeat("a", 32)} {
			if err := execTelegramLinkCode(database, hash, "member-1", 100, 200); err == nil {
				t.Errorf("insert code_hash=%v: got nil error, want error", hash)
			}
		}
	})

	t.Run("rejects a code that expires before it is created", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		if err := execTelegramLinkCode(database, tokenHash(1), "member-1", 200, 200); err == nil {
			t.Fatal("insert expires_at equal to created_at: got nil error, want error")
		}
	})

	t.Run("rejects timestamps that are not integers", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")

		for _, values := range [][2]any{
			{"never", 200},
			{100, "never"},
			{100, 200.5},
		} {
			if err := execTelegramLinkCode(database, tokenHash(1), "member-1", values[0], values[1]); err == nil {
				t.Errorf("insert created_at=%v expires_at=%v: got nil error, want error", values[0], values[1])
			}
		}

		insertTelegramLinkCode(t, database, tokenHash(2), "member-1", 100, 200)
		for _, statement := range []string{
			`UPDATE telegram_link_code SET used_at = 'never'`,
			`UPDATE telegram_link_code SET used_at = 150.5`,
		} {
			if _, err := database.Exec(statement); err == nil {
				t.Errorf("%s: got nil error, want error", statement)
			}
		}
	})

	t.Run("allows consuming a code within its lifetime", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertTelegramLinkCode(t, database, tokenHash(1), "member-1", 100, 200)

		if _, err := database.Exec(`UPDATE telegram_link_code SET used_at = 150`); err != nil {
			t.Fatalf("consume link code: got %v, want nil", err)
		}
	})

	t.Run("rejects consuming a code outside its lifetime", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertTelegramLinkCode(t, database, tokenHash(1), "member-1", 100, 200)

		for _, usedAt := range []int64{99, 200, 201} {
			if _, err := database.Exec(`UPDATE telegram_link_code SET used_at = ?`, usedAt); err == nil {
				t.Errorf("consume at %d: got nil error, want error", usedAt)
			}
		}
	})

	t.Run("rejects clearing or changing the use of a consumed code", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertTelegramLinkCode(t, database, tokenHash(1), "member-1", 100, 200)
		if _, err := database.Exec(`UPDATE telegram_link_code SET used_at = 150`); err != nil {
			t.Fatalf("consume link code: %v", err)
		}

		for _, statement := range []string{
			`UPDATE telegram_link_code SET used_at = NULL`,
			`UPDATE telegram_link_code SET used_at = 160`,
		} {
			if _, err := database.Exec(statement); err == nil {
				t.Errorf("%s: got nil error, want error", statement)
			}
		}

		var usedAt int64
		if err := database.QueryRow(`SELECT used_at FROM telegram_link_code`).Scan(&usedAt); err != nil {
			t.Fatalf("read used_at: %v", err)
		}
		if usedAt != 150 {
			t.Errorf("used_at after rejected updates: got %d, want 150", usedAt)
		}
	})

	t.Run("deleting a member cascades to its link codes", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertSecondMember(t, database, "household-1", "member-2")
		insertTelegramLinkCode(t, database, tokenHash(1), "member-2", 100, 200)

		if _, err := database.Exec(`DELETE FROM member WHERE id = ?`, "member-2"); err != nil {
			t.Fatalf("delete member: %v", err)
		}

		assertRowCount(t, database, "telegram_link_code", 0)
	})
}

func TestApplySchemaAddsTelegramTablesToExistingDatabase(t *testing.T) {
	legacySchema, err := os.ReadFile("testdata/schema_before_magic_link_lifecycle.sql")
	if err != nil {
		t.Fatalf("read legacy schema: %v", err)
	}
	database := openTestDatabase(t)
	if _, err := database.Exec(string(legacySchema)); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	insertHousehold(t, database, "household-1", "member-1")

	if err := mealdb.ApplySchema(context.Background(), database); err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}

	insertTelegramLinkCode(t, database, tokenHash(1), "member-1", 100, 200)
	insertTelegramIdentity(t, database, 123456789, "member-1")
}

func insertSecondMember(t *testing.T, database *sql.DB, householdID, memberID string) {
	t.Helper()

	if _, err := database.Exec(
		`INSERT INTO member (id, household_id, name) VALUES (?, ?, ?)`,
		memberID, householdID, "Second Member",
	); err != nil {
		t.Fatalf("insert member %s: %v", memberID, err)
	}
}

func assertRowCount(t *testing.T, database *sql.DB, table string, want int) {
	t.Helper()

	var got int
	if err := database.QueryRow(`SELECT count(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatalf("count %s rows: %v", table, err)
	}
	if got != want {
		t.Errorf("%s rows: got %d, want %d", table, got, want)
	}
}

func insertTelegramIdentity(t *testing.T, database *sql.DB, telegramUserID int64, memberID string) {
	t.Helper()

	if err := execTelegramIdentity(database, telegramUserID, memberID, 100); err != nil {
		t.Fatalf("insert telegram identity: %v", err)
	}
}

func execTelegramIdentity(database *sql.DB, telegramUserID any, memberID string, linkedAt any) error {
	_, err := database.Exec(
		`INSERT INTO telegram_identity (telegram_user_id, member_id, linked_at) VALUES (?, ?, ?)`,
		telegramUserID, memberID, linkedAt,
	)
	return err
}

func insertTelegramLinkCode(t *testing.T, database *sql.DB, hash []byte, memberID string, createdAt, expiresAt int64) {
	t.Helper()

	if err := execTelegramLinkCode(database, hash, memberID, createdAt, expiresAt); err != nil {
		t.Fatalf("insert telegram link code: %v", err)
	}
}

func execTelegramLinkCode(database *sql.DB, hash any, memberID string, createdAt, expiresAt any) error {
	_, err := database.Exec(
		`INSERT INTO telegram_link_code (code_hash, member_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hash, memberID, createdAt, expiresAt,
	)
	return err
}
