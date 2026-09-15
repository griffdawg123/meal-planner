package household_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	mealdb "github.com/griffdawg123/meal-planner/db"
	"github.com/griffdawg123/meal-planner/household"
	_ "modernc.org/sqlite"
)

func TestCreateHousehold(t *testing.T) {
	t.Run("creates the household with exactly the creator as a member", func(t *testing.T) {
		database := newTestDatabase(t)
		service := household.NewService(database)

		created, err := service.CreateHousehold(
			context.Background(), " Test Household ", "Australia/Sydney", " Test Creator ",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}

		var name, timezone, creatorID string
		if err := database.QueryRow(
			`SELECT name, timezone, created_by_member_id FROM household WHERE id = ?`, created.ID,
		).Scan(&name, &timezone, &creatorID); err != nil {
			t.Fatalf("read household: %v", err)
		}
		if name != "Test Household" {
			t.Errorf("household name: got %q, want %q", name, "Test Household")
		}
		if timezone != "Australia/Sydney" {
			t.Errorf("household timezone: got %q, want %q", timezone, "Australia/Sydney")
		}
		if creatorID != created.CreatorID {
			t.Errorf("creator ID: got %q, want %q", creatorID, created.CreatorID)
		}

		var memberCount int
		if err := database.QueryRow(
			`SELECT count(*) FROM member WHERE household_id = ?`, created.ID,
		).Scan(&memberCount); err != nil {
			t.Fatalf("count members: %v", err)
		}
		if memberCount != 1 {
			t.Fatalf("member count: got %d, want 1", memberCount)
		}

		var memberName string
		if err := database.QueryRow(
			`SELECT name FROM member WHERE household_id = ? AND id = ?`, created.ID, created.CreatorID,
		).Scan(&memberName); err != nil {
			t.Fatalf("read creator: %v", err)
		}
		if memberName != "Test Creator" {
			t.Errorf("creator name: got %q, want %q", memberName, "Test Creator")
		}
	})

	t.Run("rejects an unrecognized timezone without writing rows", func(t *testing.T) {
		database := newTestDatabase(t)
		service := household.NewService(database)

		_, err := service.CreateHousehold(context.Background(), "Test Household", "Mars/Olympus_Mons", "Test Creator")
		if !errors.Is(err, household.ErrInvalidInput) {
			t.Fatalf("error: got %v, want ErrInvalidInput", err)
		}
		assertDatabaseEmpty(t, database)
	})

	t.Run("rolls back the household when creator insertion fails", func(t *testing.T) {
		database := newTestDatabase(t)
		if _, err := database.Exec(`
			CREATE TRIGGER reject_member
			BEFORE INSERT ON member
			BEGIN
				SELECT RAISE(ABORT, 'member insert rejected');
			END;
		`); err != nil {
			t.Fatalf("create failure trigger: %v", err)
		}
		service := household.NewService(database)

		_, err := service.CreateHousehold(context.Background(), "Test Household", "Australia/Sydney", "Test Creator")
		if !errors.Is(err, household.ErrInternal) {
			t.Fatalf("error: got %v, want ErrInternal", err)
		}
		if errors.Is(err, household.ErrInvalidInput) {
			t.Fatalf("database failure was classified as invalid input: %v", err)
		}
		assertDatabaseEmpty(t, database)
	})

	for _, test := range []struct {
		name        string
		household   string
		timezone    string
		creatorName string
	}{
		{name: "empty household name", household: "  ", timezone: "Australia/Sydney", creatorName: "Test Creator"},
		{name: "empty timezone", household: "Test Household", timezone: " ", creatorName: "Test Creator"},
		{name: "empty creator name", household: "Test Household", timezone: "Australia/Sydney", creatorName: "\t"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := newTestDatabase(t)
			service := household.NewService(database)

			_, err := service.CreateHousehold(
				context.Background(), test.household, test.timezone, test.creatorName,
			)
			if !errors.Is(err, household.ErrInvalidInput) {
				t.Fatalf("error: got %v, want ErrInvalidInput", err)
			}
			assertDatabaseEmpty(t, database)
		})
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

func assertDatabaseEmpty(t *testing.T, database *sql.DB) {
	t.Helper()

	for _, table := range []string{"household", "member"} {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s rows: got %d, want 0", table, count)
		}
	}
}
