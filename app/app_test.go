package app_test

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"testing"

	"github.com/griffdawg123/meal-planner/app"
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
