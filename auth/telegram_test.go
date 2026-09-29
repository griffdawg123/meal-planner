package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/griffdawg123/meal-planner/auth"
)

func TestTelegramIdentityLinking(t *testing.T) {
	t.Run("rejects a Telegram user who has never linked an account", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		service := auth.NewService(database)

		_, err := service.ResolveTelegram(context.Background(), 1001)
		if !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Fatalf("resolve error: got %v, want ErrTelegramNotLinked", err)
		}
	})

	t.Run("rejects a Telegram user who requested a code but has not used it", func(t *testing.T) {
		service, _, _ := newTelegramCodeService(t)

		_, err := service.ResolveTelegram(context.Background(), 1001)
		if !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Fatalf("resolve error: got %v, want ErrTelegramNotLinked", err)
		}
	})

	t.Run("rejects zero and negative Telegram user IDs", func(t *testing.T) {
		service, _, code := newTelegramCodeService(t)

		for _, telegramUserID := range []int64{0, -1} {
			if _, err := service.ResolveTelegram(context.Background(), telegramUserID); !errors.Is(err, auth.ErrTelegramNotLinked) {
				t.Errorf("resolve %d error: got %v, want ErrTelegramNotLinked", telegramUserID, err)
			}
			if _, err := service.LinkTelegram(context.Background(), telegramUserID, code); !errors.Is(err, auth.ErrInvalidInput) {
				t.Errorf("link %d error: got %v, want ErrInvalidInput", telegramUserID, err)
			}
		}
	})

	t.Run("links a Telegram user with a code and resolves them to the code's household member", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")
		service := auth.NewService(database)

		code, err := service.RequestTelegramLinkCode(context.Background(), "household-2", "member-2")
		if err != nil {
			t.Fatalf("request link code: %v", err)
		}
		if code == "" {
			t.Fatal("link code: got empty, want non-empty")
		}

		var storedCode string
		if err := database.QueryRow(`SELECT code_hash FROM telegram_link_code`).Scan(&storedCode); err != nil {
			t.Fatalf("read stored link code: %v", err)
		}
		if storedCode == code {
			t.Errorf("stored link code: got raw code %q, want a hash", storedCode)
		}

		want := auth.Principal{HouseholdID: "household-2", MemberID: "member-2"}
		linked, err := service.LinkTelegram(context.Background(), 1001, code)
		if err != nil {
			t.Fatalf("link Telegram: %v", err)
		}
		if linked != want {
			t.Errorf("linked principal: got %+v, want %+v", linked, want)
		}

		resolved, err := service.ResolveTelegram(context.Background(), 1001)
		if err != nil {
			t.Fatalf("resolve Telegram: %v", err)
		}
		if resolved != want {
			t.Errorf("resolved principal: got %+v, want %+v", resolved, want)
		}
	})

	t.Run("accepts a link code typed in lower case with surrounding spaces and without its separator", func(t *testing.T) {
		service, _, code := newTelegramCodeService(t)

		typed := "  " + strings.ToLower(strings.ReplaceAll(code, "-", "")) + " "
		if _, err := service.LinkTelegram(context.Background(), 1001, typed); err != nil {
			t.Fatalf("link Telegram with %q: %v", typed, err)
		}
	})

	t.Run("resolves each linked Telegram user to their own member across households", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertMember(t, database, "household-1", "member-2")
		insertHousehold(t, database, "household-2", "member-3")
		service := auth.NewService(database)

		want := map[int64]auth.Principal{
			1001: {HouseholdID: "household-1", MemberID: "member-1"},
			1002: {HouseholdID: "household-1", MemberID: "member-2"},
			1003: {HouseholdID: "household-2", MemberID: "member-3"},
		}
		for telegramUserID, principal := range want {
			code, err := service.RequestTelegramLinkCode(context.Background(), principal.HouseholdID, principal.MemberID)
			if err != nil {
				t.Fatalf("request link code for %s: %v", principal.MemberID, err)
			}
			if _, err := service.LinkTelegram(context.Background(), telegramUserID, code); err != nil {
				t.Fatalf("link Telegram user %d: %v", telegramUserID, err)
			}
		}

		for telegramUserID, principal := range want {
			got, err := service.ResolveTelegram(context.Background(), telegramUserID)
			if err != nil {
				t.Fatalf("resolve Telegram user %d: %v", telegramUserID, err)
			}
			if got != principal {
				t.Errorf("principal for Telegram user %d: got %+v, want %+v", telegramUserID, got, principal)
			}
		}
		if _, err := service.ResolveTelegram(context.Background(), 1004); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("resolve unlinked Telegram user error: got %v, want ErrTelegramNotLinked", err)
		}
	})

	t.Run("rejects a second use of the same link code by another Telegram user", func(t *testing.T) {
		service, database, code := newTelegramCodeService(t)

		if _, err := service.LinkTelegram(context.Background(), 1001, code); err != nil {
			t.Fatalf("first link: %v", err)
		}
		if _, err := service.LinkTelegram(context.Background(), 1002, code); !errors.Is(err, auth.ErrInvalidTelegramLinkCode) {
			t.Fatalf("second link error: got %v, want ErrInvalidTelegramLinkCode", err)
		}

		if _, err := service.ResolveTelegram(context.Background(), 1002); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("resolve second Telegram user error: got %v, want ErrTelegramNotLinked", err)
		}
		if got := countRows(t, database, "telegram_identity"); got != 1 {
			t.Errorf("Telegram identities: got %d, want 1", got)
		}
	})

	t.Run("rejects an expired link code and leaves it unused", func(t *testing.T) {
		service, database, code := newTelegramCodeService(t)

		if _, err := database.Exec(`UPDATE telegram_link_code SET created_at = -1, expires_at = 0`); err != nil {
			t.Fatalf("expire link code: %v", err)
		}
		if _, err := service.LinkTelegram(context.Background(), 1001, code); !errors.Is(err, auth.ErrInvalidTelegramLinkCode) {
			t.Fatalf("expired link error: got %v, want ErrInvalidTelegramLinkCode", err)
		}

		if _, err := service.ResolveTelegram(context.Background(), 1001); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("resolve after expired link error: got %v, want ErrTelegramNotLinked", err)
		}
		var usedAt sql.NullInt64
		if err := database.QueryRow(`SELECT used_at FROM telegram_link_code`).Scan(&usedAt); err != nil {
			t.Fatalf("read link code: %v", err)
		}
		if usedAt.Valid {
			t.Errorf("expired link code used_at: got %d, want NULL", usedAt.Int64)
		}
	})

	t.Run("rejects a link code at the instant it expires", func(t *testing.T) {
		service, database, code := newTelegramCodeService(t)

		if _, err := database.Exec(`UPDATE telegram_link_code SET created_at = unixepoch() - 60, expires_at = unixepoch()`); err != nil {
			t.Fatalf("expire link code now: %v", err)
		}
		if _, err := service.LinkTelegram(context.Background(), 1001, code); !errors.Is(err, auth.ErrInvalidTelegramLinkCode) {
			t.Fatalf("boundary link error: got %v, want ErrInvalidTelegramLinkCode", err)
		}
	})

	t.Run("rejects unknown and empty link codes without linking", func(t *testing.T) {
		service, database, _ := newTelegramCodeService(t)

		for _, code := range []string{"", "   ", "NOT-A-CODE"} {
			if _, err := service.LinkTelegram(context.Background(), 1001, code); !errors.Is(err, auth.ErrInvalidTelegramLinkCode) {
				t.Errorf("link with %q error: got %v, want ErrInvalidTelegramLinkCode", code, err)
			}
		}
		if got := countRows(t, database, "telegram_identity"); got != 0 {
			t.Errorf("Telegram identities: got %d, want 0", got)
		}
	})

	t.Run("locks out a Telegram user after too many invalid link codes, even with the right code", func(t *testing.T) {
		service, database, code := newTelegramCodeService(t)

		guessInvalidTelegramCodes(t, service, 1001, auth.MaxFailedTelegramLinkAttempts)

		if _, err := service.LinkTelegram(context.Background(), 1001, "WRONG-CODE"); !errors.Is(err, auth.ErrTooManyTelegramLinkAttempts) {
			t.Errorf("excess guess error: got %v, want ErrTooManyTelegramLinkAttempts", err)
		}
		if _, err := service.LinkTelegram(context.Background(), 1001, code); !errors.Is(err, auth.ErrTooManyTelegramLinkAttempts) {
			t.Fatalf("correct code while locked out error: got %v, want ErrTooManyTelegramLinkAttempts", err)
		}
		if _, err := service.ResolveTelegram(context.Background(), 1001); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("resolve locked-out Telegram user error: got %v, want ErrTelegramNotLinked", err)
		}
		var usedAt sql.NullInt64
		if err := database.QueryRow(`SELECT used_at FROM telegram_link_code`).Scan(&usedAt); err != nil {
			t.Fatalf("read link code: %v", err)
		}
		if usedAt.Valid {
			t.Errorf("link code used_at while locked out: got %d, want NULL", usedAt.Int64)
		}
	})

	t.Run("persists the lockout across service instances", func(t *testing.T) {
		service, database, code := newTelegramCodeService(t)

		guessInvalidTelegramCodes(t, service, 1001, auth.MaxFailedTelegramLinkAttempts)

		restarted := auth.NewService(database)
		if _, err := restarted.LinkTelegram(context.Background(), 1001, code); !errors.Is(err, auth.ErrTooManyTelegramLinkAttempts) {
			t.Fatalf("link after restart error: got %v, want ErrTooManyTelegramLinkAttempts", err)
		}
	})

	t.Run("counts invalid link codes separately for each Telegram user", func(t *testing.T) {
		service, _, code := newTelegramCodeService(t)

		guessInvalidTelegramCodes(t, service, 1001, auth.MaxFailedTelegramLinkAttempts)

		if _, err := service.LinkTelegram(context.Background(), 1002, code); err != nil {
			t.Fatalf("link another Telegram user: %v", err)
		}
	})

	t.Run("allows linking again once the failed-attempt window has passed", func(t *testing.T) {
		service, database, code := newTelegramCodeService(t)

		guessInvalidTelegramCodes(t, service, 1001, auth.MaxFailedTelegramLinkAttempts)
		if _, err := database.Exec(`UPDATE telegram_link_attempt SET window_started_at = unixepoch() - 3600`); err != nil {
			t.Fatalf("age failed attempts: %v", err)
		}

		if _, err := service.LinkTelegram(context.Background(), 1001, code); err != nil {
			t.Fatalf("link after window: %v", err)
		}
	})

	t.Run("clears failed attempts after a successful link", func(t *testing.T) {
		service, database, code := newTelegramCodeService(t)

		guessInvalidTelegramCodes(t, service, 1001, auth.MaxFailedTelegramLinkAttempts-1)
		if _, err := service.LinkTelegram(context.Background(), 1001, code); err != nil {
			t.Fatalf("link with remaining attempt: %v", err)
		}

		if got := countRows(t, database, "telegram_link_attempt"); got != 0 {
			t.Errorf("failed-attempt records after link: got %d, want 0", got)
		}
	})

	t.Run("rejects requesting a link code for a member outside the household", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")
		service := auth.NewService(database)

		_, err := service.RequestTelegramLinkCode(context.Background(), "household-1", "member-2")
		if !errors.Is(err, auth.ErrMemberNotFound) {
			t.Fatalf("request error: got %v, want ErrMemberNotFound", err)
		}
		if got := countRows(t, database, "telegram_link_code"); got != 0 {
			t.Errorf("link codes: got %d, want 0", got)
		}
	})

	t.Run("rejects linking a Telegram user who is already linked to another member", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")
		service := auth.NewService(database)

		first, err := service.RequestTelegramLinkCode(context.Background(), "household-1", "member-1")
		if err != nil {
			t.Fatalf("request first link code: %v", err)
		}
		if _, err := service.LinkTelegram(context.Background(), 1001, first); err != nil {
			t.Fatalf("first link: %v", err)
		}
		second, err := service.RequestTelegramLinkCode(context.Background(), "household-2", "member-2")
		if err != nil {
			t.Fatalf("request second link code: %v", err)
		}

		if _, err := service.LinkTelegram(context.Background(), 1001, second); !errors.Is(err, auth.ErrTelegramAlreadyLinked) {
			t.Fatalf("second link error: got %v, want ErrTelegramAlreadyLinked", err)
		}

		want := auth.Principal{HouseholdID: "household-1", MemberID: "member-1"}
		got, err := service.ResolveTelegram(context.Background(), 1001)
		if err != nil {
			t.Fatalf("resolve Telegram: %v", err)
		}
		if got != want {
			t.Errorf("principal after rejected relink: got %+v, want %+v", got, want)
		}
		var usedAt sql.NullInt64
		if err := database.QueryRow(`SELECT used_at FROM telegram_link_code WHERE member_id = 'member-2'`).Scan(&usedAt); err != nil {
			t.Fatalf("read second link code: %v", err)
		}
		if usedAt.Valid {
			t.Errorf("rejected link code used_at: got %d, want NULL", usedAt.Int64)
		}
	})

	t.Run("relinks a member to a new Telegram account and unlinks the old one", func(t *testing.T) {
		service, _, first := newTelegramCodeService(t)

		if _, err := service.LinkTelegram(context.Background(), 1001, first); err != nil {
			t.Fatalf("first link: %v", err)
		}
		second, err := service.RequestTelegramLinkCode(context.Background(), "household-1", "member-1")
		if err != nil {
			t.Fatalf("request second link code: %v", err)
		}
		if _, err := service.LinkTelegram(context.Background(), 1002, second); err != nil {
			t.Fatalf("second link: %v", err)
		}

		if _, err := service.ResolveTelegram(context.Background(), 1001); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("resolve old Telegram user error: got %v, want ErrTelegramNotLinked", err)
		}
		want := auth.Principal{HouseholdID: "household-1", MemberID: "member-1"}
		got, err := service.ResolveTelegram(context.Background(), 1002)
		if err != nil {
			t.Fatalf("resolve new Telegram user: %v", err)
		}
		if got != want {
			t.Errorf("principal for new Telegram user: got %+v, want %+v", got, want)
		}
	})

	t.Run("rejects a Telegram user whose member has been removed", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertMember(t, database, "household-1", "member-2")
		service := auth.NewService(database)

		code, err := service.RequestTelegramLinkCode(context.Background(), "household-1", "member-2")
		if err != nil {
			t.Fatalf("request link code: %v", err)
		}
		if _, err := service.LinkTelegram(context.Background(), 1001, code); err != nil {
			t.Fatalf("link Telegram: %v", err)
		}
		if _, err := database.Exec(`DELETE FROM member WHERE id = 'member-2'`); err != nil {
			t.Fatalf("remove member: %v", err)
		}

		if _, err := service.ResolveTelegram(context.Background(), 1001); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Fatalf("resolve removed member error: got %v, want ErrTelegramNotLinked", err)
		}
	})
}

func TestTelegramIdentityUnlinking(t *testing.T) {
	t.Run("unlinks a Telegram user so they no longer resolve to their member", func(t *testing.T) {
		service, database, code := newTelegramCodeService(t)
		linkTelegram(t, service, 1001, code)

		if err := service.UnlinkTelegram(context.Background(), 1001); err != nil {
			t.Fatalf("unlink Telegram: %v", err)
		}

		if _, err := service.ResolveTelegram(context.Background(), 1001); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("resolve after unlink error: got %v, want ErrTelegramNotLinked", err)
		}
		if got := countRows(t, database, "member"); got != 1 {
			t.Errorf("members after unlink: got %d, want 1", got)
		}
	})

	t.Run("reports a Telegram user who is not linked, including on a repeated unlink", func(t *testing.T) {
		service, _, code := newTelegramCodeService(t)

		if err := service.UnlinkTelegram(context.Background(), 1001); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("unlink never-linked error: got %v, want ErrTelegramNotLinked", err)
		}
		linkTelegram(t, service, 1001, code)
		if err := service.UnlinkTelegram(context.Background(), 1001); err != nil {
			t.Fatalf("first unlink: %v", err)
		}
		if err := service.UnlinkTelegram(context.Background(), 1001); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("repeated unlink error: got %v, want ErrTelegramNotLinked", err)
		}
	})

	t.Run("rejects zero and negative Telegram user IDs", func(t *testing.T) {
		service := auth.NewService(newTestDatabase(t))

		for _, telegramUserID := range []int64{0, -1} {
			if err := service.UnlinkTelegram(context.Background(), telegramUserID); !errors.Is(err, auth.ErrInvalidInput) {
				t.Errorf("unlink %d error: got %v, want ErrInvalidInput", telegramUserID, err)
			}
		}
	})

	t.Run("leaves other linked Telegram users in place", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertMember(t, database, "household-1", "member-2")
		service := auth.NewService(database)
		linkTelegram(t, service, 1001, requestTelegramLinkCode(t, service, "household-1", "member-1"))
		linkTelegram(t, service, 1002, requestTelegramLinkCode(t, service, "household-1", "member-2"))

		if err := service.UnlinkTelegram(context.Background(), 1001); err != nil {
			t.Fatalf("unlink Telegram: %v", err)
		}

		want := auth.Principal{HouseholdID: "household-1", MemberID: "member-2"}
		got, err := service.ResolveTelegram(context.Background(), 1002)
		if err != nil {
			t.Fatalf("resolve remaining Telegram user: %v", err)
		}
		if got != want {
			t.Errorf("remaining principal: got %+v, want %+v", got, want)
		}
	})

	t.Run("allows an unlinked Telegram user to link again with a new code", func(t *testing.T) {
		service, _, code := newTelegramCodeService(t)
		linkTelegram(t, service, 1001, code)
		if err := service.UnlinkTelegram(context.Background(), 1001); err != nil {
			t.Fatalf("unlink Telegram: %v", err)
		}

		linkTelegram(t, service, 1001, requestTelegramLinkCode(t, service, "household-1", "member-1"))

		want := auth.Principal{HouseholdID: "household-1", MemberID: "member-1"}
		got, err := service.ResolveTelegram(context.Background(), 1001)
		if err != nil {
			t.Fatalf("resolve relinked Telegram user: %v", err)
		}
		if got != want {
			t.Errorf("relinked principal: got %+v, want %+v", got, want)
		}
	})

	t.Run("unlinks a member's Telegram account from the member's side", func(t *testing.T) {
		service, _, code := newTelegramCodeService(t)
		linkTelegram(t, service, 1001, code)

		if err := service.UnlinkMemberTelegram(context.Background(), "household-1", "member-1"); err != nil {
			t.Fatalf("unlink member Telegram: %v", err)
		}

		if _, err := service.ResolveTelegram(context.Background(), 1001); !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("resolve after member unlink error: got %v, want ErrTelegramNotLinked", err)
		}
	})

	t.Run("reports a member with no linked Telegram account", func(t *testing.T) {
		service, _, _ := newTelegramCodeService(t)

		err := service.UnlinkMemberTelegram(context.Background(), "household-1", "member-1")
		if !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Fatalf("unlink member error: got %v, want ErrTelegramNotLinked", err)
		}
	})

	t.Run("rejects unlinking a member outside the household and leaves their link in place", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertHousehold(t, database, "household-2", "member-2")
		service := auth.NewService(database)
		linkTelegram(t, service, 1002, requestTelegramLinkCode(t, service, "household-2", "member-2"))

		for _, target := range []struct{ householdID, memberID string }{
			{"household-1", "member-2"},
			{"household-1", "missing-member"},
		} {
			err := service.UnlinkMemberTelegram(context.Background(), target.householdID, target.memberID)
			if !errors.Is(err, auth.ErrMemberNotFound) {
				t.Errorf("unlink %s/%s error: got %v, want ErrMemberNotFound", target.householdID, target.memberID, err)
			}
		}

		if got := countRows(t, database, "telegram_identity"); got != 1 {
			t.Errorf("Telegram identities: got %d, want 1", got)
		}
	})

	t.Run("rejects an empty household or member", func(t *testing.T) {
		service := auth.NewService(newTestDatabase(t))

		for _, target := range []struct{ householdID, memberID string }{
			{"", "member-1"},
			{"household-1", " "},
		} {
			err := service.UnlinkMemberTelegram(context.Background(), target.householdID, target.memberID)
			if !errors.Is(err, auth.ErrInvalidInput) {
				t.Errorf("unlink %q/%q error: got %v, want ErrInvalidInput", target.householdID, target.memberID, err)
			}
		}
	})
}

func TestHouseholdTelegramMembers(t *testing.T) {
	t.Run("lists every member of the household with their linked Telegram user, or zero if unlinked", func(t *testing.T) {
		database := newTestDatabase(t)
		insertHousehold(t, database, "household-1", "member-1")
		insertMember(t, database, "household-1", "member-2")
		insertMember(t, database, "household-1", "member-3")
		insertHousehold(t, database, "household-2", "member-4")
		service := auth.NewService(database)
		linkTelegram(t, service, 1001, requestTelegramLinkCode(t, service, "household-1", "member-1"))
		linkTelegram(t, service, 1003, requestTelegramLinkCode(t, service, "household-1", "member-3"))
		linkTelegram(t, service, 1004, requestTelegramLinkCode(t, service, "household-2", "member-4"))

		got, err := service.HouseholdTelegramMembers(context.Background(), "household-1")
		if err != nil {
			t.Fatalf("list household Telegram members: %v", err)
		}

		want := []auth.TelegramMember{
			{MemberID: "member-1", TelegramUserID: 1001},
			{MemberID: "member-2"},
			{MemberID: "member-3", TelegramUserID: 1003},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("members: got %+v, want %+v", got, want)
		}
	})

	t.Run("reports a member as unlinked after their Telegram account is unlinked", func(t *testing.T) {
		service, _, code := newTelegramCodeService(t)
		linkTelegram(t, service, 1001, code)
		if err := service.UnlinkTelegram(context.Background(), 1001); err != nil {
			t.Fatalf("unlink Telegram: %v", err)
		}

		got, err := service.HouseholdTelegramMembers(context.Background(), "household-1")
		if err != nil {
			t.Fatalf("list household Telegram members: %v", err)
		}

		want := []auth.TelegramMember{{MemberID: "member-1"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("members: got %+v, want %+v", got, want)
		}
	})

	t.Run("returns no members for an unknown household", func(t *testing.T) {
		service, _, _ := newTelegramCodeService(t)

		got, err := service.HouseholdTelegramMembers(context.Background(), "missing-household")
		if err != nil {
			t.Fatalf("list household Telegram members: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("members: got %+v, want none", got)
		}
	})

	t.Run("rejects an empty household", func(t *testing.T) {
		service := auth.NewService(newTestDatabase(t))

		if _, err := service.HouseholdTelegramMembers(context.Background(), " "); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("error: got %v, want ErrInvalidInput", err)
		}
	})
}

func requestTelegramLinkCode(t *testing.T, service *auth.Service, householdID, memberID string) string {
	t.Helper()

	code, err := service.RequestTelegramLinkCode(context.Background(), householdID, memberID)
	if err != nil {
		t.Fatalf("request link code for %s: %v", memberID, err)
	}
	return code
}

func linkTelegram(t *testing.T, service *auth.Service, telegramUserID int64, code string) {
	t.Helper()

	if _, err := service.LinkTelegram(context.Background(), telegramUserID, code); err != nil {
		t.Fatalf("link Telegram user %d: %v", telegramUserID, err)
	}
}

func newTelegramCodeService(t *testing.T) (*auth.Service, *sql.DB, string) {
	t.Helper()

	database := newTestDatabase(t)
	insertHousehold(t, database, "household-1", "member-1")
	service := auth.NewService(database)
	code, err := service.RequestTelegramLinkCode(context.Background(), "household-1", "member-1")
	if err != nil {
		t.Fatalf("request link code: %v", err)
	}
	return service, database, code
}

func guessInvalidTelegramCodes(t *testing.T, service *auth.Service, telegramUserID int64, count int) {
	t.Helper()

	for i := 0; i < count; i++ {
		if _, err := service.LinkTelegram(context.Background(), telegramUserID, "WRONG-CODE"); !errors.Is(err, auth.ErrInvalidTelegramLinkCode) {
			t.Fatalf("invalid guess %d error: got %v, want ErrInvalidTelegramLinkCode", i+1, err)
		}
	}
}
