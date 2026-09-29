package telegram_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/auth"
	"github.com/griffdawg123/meal-planner/household"
	"github.com/griffdawg123/meal-planner/telegram"
)

// The auth service is the production source of household Telegram links.
var _ telegram.MemberDirectory = (*auth.Service)(nil)

func TestDraftPlanNotifier(t *testing.T) {
	event := household.DraftPlanCreated{
		HouseholdID: "household-1",
		Dinners: []household.DraftDinner{
			{Night: "2026-10-05", Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
			{Night: "2026-10-06", Title: "Minestrone", Description: "Vegetable soup with pasta."},
		},
	}
	message, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
		{Night: "2026-10-05", Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
		{Night: "2026-10-06", Title: "Minestrone", Description: "Vegetable soup with pasta."},
	})
	if err != nil {
		t.Fatalf("format draft plan: %v", err)
	}
	directory := fakeDirectory{
		"household-1": {
			{MemberID: "member-1", TelegramUserID: 1001},
			{MemberID: "member-2"},
			{MemberID: "member-3", TelegramUserID: 1003},
		},
		"household-2": {
			{MemberID: "member-4", TelegramUserID: 2001},
		},
	}

	t.Run("messages every linked member of the draft's household when a draft is published", func(t *testing.T) {
		sender := &recordingSender{}
		var events household.DraftPlanEvents
		telegram.NewDraftPlanNotifier(directory, sender).Subscribe(&events)

		if err := events.Publish(context.Background(), event); err != nil {
			t.Fatalf("publish: got %v, want nil", err)
		}

		want := []sentMessage{
			{ChatID: 1001, Text: message, ParseMode: telegram.ParseMode},
			{ChatID: 1003, Text: message, ParseMode: telegram.ParseMode},
		}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("does not message members of other households", func(t *testing.T) {
		sender := &recordingSender{}
		other := event
		other.HouseholdID = "household-2"

		if err := telegram.NewDraftPlanNotifier(directory, sender).HandleDraftPlanCreated(context.Background(), other); err != nil {
			t.Fatalf("handle draft plan: got %v, want nil", err)
		}

		if got, want := sender.chatIDs(), []int64{2001}; !reflect.DeepEqual(got, want) {
			t.Fatalf("messaged chats: got %v, want %v", got, want)
		}
	})

	t.Run("reports a failure to resolve recipients and messages nobody", func(t *testing.T) {
		sender := &recordingSender{}
		lookupErr := errors.New("database is locked")
		notifier := telegram.NewDraftPlanNotifier(failingDirectory{err: lookupErr}, sender)

		err := notifier.HandleDraftPlanCreated(context.Background(), event)
		if !errors.Is(err, lookupErr) {
			t.Errorf("handle draft plan: got %v, want it to wrap %v", err, lookupErr)
		}
		if len(sender.sent) != 0 {
			t.Errorf("sent messages: got %+v, want none", sender.sent)
		}
	})

	t.Run("reports a failed send through the published event", func(t *testing.T) {
		sendErr := errors.New("bot was blocked by the user")
		sender := &recordingSender{failFor: map[int64]error{1001: sendErr}}
		var events household.DraftPlanEvents
		telegram.NewDraftPlanNotifier(directory, sender).Subscribe(&events)

		err := events.Publish(context.Background(), event)
		if !errors.Is(err, telegram.ErrDeliveryFailed) || !errors.Is(err, sendErr) {
			t.Errorf("publish: got %v, want it to wrap %v and %v", err, telegram.ErrDeliveryFailed, sendErr)
		}
		if got, want := sender.chatIDs(), []int64{1001, 1003}; !reflect.DeepEqual(got, want) {
			t.Errorf("attempted chats: got %v, want %v", got, want)
		}
	})
}

func TestHouseholdRecipients(t *testing.T) {
	t.Run("resolves every member of the household with their linked Telegram user", func(t *testing.T) {
		directory := fakeDirectory{
			"household-1": {
				{MemberID: "member-1", TelegramUserID: 1001},
				{MemberID: "member-2"},
			},
		}

		got, err := telegram.HouseholdRecipients(context.Background(), directory, "household-1")
		if err != nil {
			t.Fatalf("household recipients: %v", err)
		}

		want := []telegram.Recipient{
			{MemberID: "member-1", TelegramUserID: 1001},
			{MemberID: "member-2"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("recipients: got %+v, want %+v", got, want)
		}
	})
}

// fakeDirectory lists the Telegram members of each household by household ID.
type fakeDirectory map[string][]auth.TelegramMember

func (d fakeDirectory) HouseholdTelegramMembers(_ context.Context, householdID string) ([]auth.TelegramMember, error) {
	return d[householdID], nil
}

type failingDirectory struct {
	err error
}

func (d failingDirectory) HouseholdTelegramMembers(context.Context, string) ([]auth.TelegramMember, error) {
	return nil, d.err
}
