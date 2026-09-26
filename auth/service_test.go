package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/griffdawg123/meal-planner/auth"
	mealdb "github.com/griffdawg123/meal-planner/db"
	_ "modernc.org/sqlite"
)

func TestMagicLinkAuthentication(t *testing.T) {
	t.Run("verifies a requested link and resolves the session to its household member", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")
		service := auth.NewService(database)

		if err := service.LinkEmail(context.Background(), "household-2", "member-2", " Person@Example.com "); err != nil {
			t.Fatalf("link email: %v", err)
		}
		magicToken, err := service.RequestMagicLink(context.Background(), "person@example.com")
		if err != nil {
			t.Fatalf("request magic link: %v", err)
		}
		if magicToken == "" {
			t.Fatal("magic-link token: got empty, want non-empty")
		}

		var storedToken string
		if err := database.QueryRow(`SELECT token_hash FROM magic_link`).Scan(&storedToken); err != nil {
			t.Fatalf("read stored magic link: %v", err)
		}
		if storedToken == magicToken {
			t.Errorf("stored magic-link token: got raw token %q, want a hash", storedToken)
		}

		session, err := service.VerifyMagicLink(context.Background(), magicToken)
		if err != nil {
			t.Fatalf("verify magic link: %v", err)
		}
		if session.Token == "" {
			t.Fatal("session token: got empty, want non-empty")
		}

		principal, err := service.ResolveSession(context.Background(), session.Token)
		if err != nil {
			t.Fatalf("resolve session: %v", err)
		}
		if principal.HouseholdID != "household-2" {
			t.Errorf("session household ID: got %q, want %q", principal.HouseholdID, "household-2")
		}
		if principal.MemberID != "member-2" {
			t.Errorf("session member ID: got %q, want %q", principal.MemberID, "member-2")
		}
	})

	t.Run("rejects a second use of the same magic link", func(t *testing.T) {
		service, _, token := newLinkedService(t)

		if _, err := service.VerifyMagicLink(context.Background(), token); err != nil {
			t.Fatalf("first verification: %v", err)
		}
		if _, err := service.VerifyMagicLink(context.Background(), token); !errors.Is(err, auth.ErrInvalidMagicLink) {
			t.Fatalf("second verification error: got %v, want ErrInvalidMagicLink", err)
		}
	})

	t.Run("rejects an expired magic link", func(t *testing.T) {
		service, database, token := newLinkedService(t)

		if _, err := database.Exec(`UPDATE magic_link SET created_at = -1, expires_at = 0`); err != nil {
			t.Fatalf("expire magic link: %v", err)
		}
		if _, err := service.VerifyMagicLink(context.Background(), token); !errors.Is(err, auth.ErrInvalidMagicLink) {
			t.Fatalf("expired verification error: got %v, want ErrInvalidMagicLink", err)
		}
	})

	t.Run("rejects an expired session", func(t *testing.T) {
		service, database, token := newLinkedService(t)
		session, err := service.VerifyMagicLink(context.Background(), token)
		if err != nil {
			t.Fatalf("verify magic link: %v", err)
		}
		if _, err := database.Exec(`UPDATE web_session SET created_at = -1, expires_at = 0`); err != nil {
			t.Fatalf("expire session: %v", err)
		}

		if _, err := service.ResolveSession(context.Background(), session.Token); !errors.Is(err, auth.ErrInvalidSession) {
			t.Fatalf("expired session error: got %v, want ErrInvalidSession", err)
		}
	})

	t.Run("rejects an email that is not linked to a member", func(t *testing.T) {
		service := auth.NewService(newTestDatabase(t))

		_, err := service.RequestMagicLink(context.Background(), "missing@example.com")
		if !errors.Is(err, auth.ErrIdentityNotFound) {
			t.Fatalf("request error: got %v, want ErrIdentityNotFound", err)
		}
	})

	t.Run("rejects linking an email to a member outside the household", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")
		service := auth.NewService(database)

		err := service.LinkEmail(context.Background(), "household-1", "member-2", "person@example.com")
		if !errors.Is(err, auth.ErrMemberNotFound) {
			t.Fatalf("link error: got %v, want ErrMemberNotFound", err)
		}
	})
}

func newLinkedService(t *testing.T) (*auth.Service, *sql.DB, string) {
	t.Helper()

	database := newTestDatabase(t)
	insertHousehold(t, database, "household-1", "member-1")
	service := auth.NewService(database)
	if err := service.LinkEmail(context.Background(), "household-1", "member-1", "person@example.com"); err != nil {
		t.Fatalf("link email: %v", err)
	}
	token, err := service.RequestMagicLink(context.Background(), "person@example.com")
	if err != nil {
		t.Fatalf("request magic link: %v", err)
	}
	return service, database, token
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
		t.Fatalf("insert member: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit household creation: %v", err)
	}
}
