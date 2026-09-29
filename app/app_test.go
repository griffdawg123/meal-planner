package app_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/griffdawg123/meal-planner/app"
	"github.com/griffdawg123/meal-planner/auth"
	mealdb "github.com/griffdawg123/meal-planner/db"
	"github.com/griffdawg123/meal-planner/household"
	"github.com/griffdawg123/meal-planner/telegram"
	_ "modernc.org/sqlite"
)

func TestDraftPlanDelivery(t *testing.T) {
	t.Run("a draft created by the planning workflow is messaged to every linked household member", func(t *testing.T) {
		ctx := context.Background()
		sender := &recordingSender{}
		application := app.New(newTestDatabase(t), sender)

		created, err := application.Households.CreateHousehold(ctx, "Household", "Australia/Sydney", "Founder")
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		linked, err := application.Households.AddMember(ctx, created.ID, "Linked Member")
		if err != nil {
			t.Fatalf("add linked member: %v", err)
		}
		if _, err := application.Households.AddMember(ctx, created.ID, "Unlinked Member"); err != nil {
			t.Fatalf("add unlinked member: %v", err)
		}
		for telegramUserID, memberID := range map[int64]string{1001: created.CreatorID, 1002: linked.ID} {
			code, err := application.Auth.RequestTelegramLinkCode(ctx, created.ID, memberID)
			if err != nil {
				t.Fatalf("request telegram link code: %v", err)
			}
			if _, err := application.Auth.LinkTelegram(ctx, telegramUserID, code); err != nil {
				t.Fatalf("link telegram: %v", err)
			}
		}
		other, err := application.Households.CreateHousehold(ctx, "Other Household", "Australia/Sydney", "Other Founder")
		if err != nil {
			t.Fatalf("create other household: %v", err)
		}
		code, err := application.Auth.RequestTelegramLinkCode(ctx, other.ID, other.CreatorID)
		if err != nil {
			t.Fatalf("request other telegram link code: %v", err)
		}
		if _, err := application.Auth.LinkTelegram(ctx, 2001, code); err != nil {
			t.Fatalf("link other telegram: %v", err)
		}

		candidates := []household.Meal{{Title: "Pad thai", Description: "Rice noodles with tofu and lime."}}
		if _, err := application.Planner.CreateDraftPlan(ctx, created.ID, []string{"2026-10-05"}, candidates, nil); err != nil {
			t.Fatalf("create draft plan: got %v, want nil", err)
		}

		message, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
			{Night: "2026-10-05", Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
		})
		if err != nil {
			t.Fatalf("format draft plan: %v", err)
		}
		// Members are messaged in member ID order, which is random, so compare by chat.
		sort.Slice(sender.sent, func(i, j int) bool { return sender.sent[i].ChatID < sender.sent[j].ChatID })
		want := []sentMessage{
			{ChatID: 1001, Text: message, ParseMode: telegram.ParseMode},
			{ChatID: 1002, Text: message, ParseMode: telegram.ParseMode},
		}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})
}

func TestTelegramBlockedMutations(t *testing.T) {
	// newLinkedHousehold composes the application and creates a household whose founder, linked as
	// Telegram user 1001, is allergic to peanuts, alongside another household linked as user 2001.
	newLinkedHousehold := func(t *testing.T) (*app.App, *recordingSender, household.Household, household.Household) {
		t.Helper()
		ctx := context.Background()
		sender := &recordingSender{}
		application := app.New(newTestDatabase(t), sender)

		created, err := application.Households.CreateHousehold(ctx, "Household", "Australia/Sydney", "Founder")
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		if _, err := application.Households.AddPreference(ctx, created.ID, created.CreatorID, sql.NullInt16{}, "peanuts", household.Allergy, household.Hard); err != nil {
			t.Fatalf("add founder allergy: %v", err)
		}
		other, err := application.Households.CreateHousehold(ctx, "Other Household", "Australia/Sydney", "Other Founder")
		if err != nil {
			t.Fatalf("create other household: %v", err)
		}
		for telegramUserID, member := range map[int64]household.Household{1001: created, 2001: other} {
			code, err := application.Auth.RequestTelegramLinkCode(ctx, member.ID, member.CreatorID)
			if err != nil {
				t.Fatalf("request telegram link code: %v", err)
			}
			if _, err := application.Auth.LinkTelegram(ctx, telegramUserID, code); err != nil {
				t.Fatalf("link telegram: %v", err)
			}
		}
		return application, sender, created, other
	}

	t.Run("a dinner that breaks a present member's allergy is never sent to the household and the member is told why", func(t *testing.T) {
		application, sender, _, _ := newLinkedHousehold(t)
		satay := household.Meal{Title: "Satay chicken", Description: "Chicken skewers with peanut sauce.", Conflicts: []string{"peanuts"}}

		err := application.Commands.ReplaceDinner(context.Background(), 1001, "2026-10-05", satay)
		if !errors.Is(err, household.ErrHardConstraint) {
			t.Errorf("replace dinner: got %v, want it to wrap %v", err, household.ErrHardConstraint)
		}

		want := []sentMessage{{ChatID: 1001, Text: telegram.ErrorReply(household.ErrHardConstraint), ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("marking a member of another household away stores nothing and the member is told why", func(t *testing.T) {
		application, sender, _, other := newLinkedHousehold(t)

		err := application.Commands.MarkAway(context.Background(), 1001, other.CreatorID, "2026-10-05")
		if !errors.Is(err, household.ErrPermissionDenied) {
			t.Errorf("mark away: got %v, want it to wrap %v", err, household.ErrPermissionDenied)
		}

		stored, err := application.Households.ListAwayNights(context.Background(), other.ID, "2026-10-01", "2026-10-31")
		if err != nil {
			t.Fatalf("list other household away nights: %v", err)
		}
		if len(stored) != 0 {
			t.Errorf("other household away nights: got %+v, want none", stored)
		}
		want := []sentMessage{{ChatID: 1001, Text: telegram.ErrorReply(household.ErrPermissionDenied), ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("an unlinked Telegram user stores nothing and is told to link first", func(t *testing.T) {
		application, sender, created, _ := newLinkedHousehold(t)

		err := application.Commands.MarkAway(context.Background(), 4040, created.CreatorID, "2026-10-05")
		if !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("mark away: got %v, want it to wrap %v", err, auth.ErrTelegramNotLinked)
		}

		stored, err := application.Households.ListAwayNights(context.Background(), created.ID, "2026-10-01", "2026-10-31")
		if err != nil {
			t.Fatalf("list away nights: %v", err)
		}
		if len(stored) != 0 {
			t.Errorf("away nights: got %+v, want none", stored)
		}
		want := []sentMessage{{ChatID: 4040, Text: telegram.ErrorReply(auth.ErrTelegramNotLinked), ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("a permitted change within the member's own household is stored", func(t *testing.T) {
		application, _, created, _ := newLinkedHousehold(t)

		if err := application.Commands.MarkAway(context.Background(), 1001, created.CreatorID, "2026-10-05"); err != nil {
			t.Fatalf("mark away: got %v, want nil", err)
		}

		stored, err := application.Households.ListAwayNights(context.Background(), created.ID, "2026-10-01", "2026-10-31")
		if err != nil {
			t.Fatalf("list away nights: %v", err)
		}
		want := []household.AwayNight{{HouseholdID: created.ID, MemberID: created.CreatorID, Night: "2026-10-05"}}
		if !reflect.DeepEqual(stored, want) {
			t.Errorf("away nights: got %+v, want %+v", stored, want)
		}
	})
}

type sentMessage struct {
	ChatID    int64
	Text      string
	ParseMode string
}

// recordingSender records every message it is asked to send.
type recordingSender struct {
	sent []sentMessage
}

func (s *recordingSender) SendMessage(_ context.Context, chatID int64, text, parseMode string) error {
	s.sent = append(s.sent, sentMessage{ChatID: chatID, Text: text, ParseMode: parseMode})
	return nil
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
