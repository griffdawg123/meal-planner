package telegram_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/telegram"
)

func TestPlanReadyNotifier(t *testing.T) {
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

	t.Run("tells every linked member of the household the recipes and shopping list are ready", func(t *testing.T) {
		sender := &recordingSender{}

		if err := telegram.NewPlanReadyNotifier(directory, sender).HandlePlanReady(context.Background(), "household-1"); err != nil {
			t.Fatalf("handle plan ready: got %v, want nil", err)
		}

		want := []sentMessage{
			{ChatID: 1001, Text: telegram.PlanReadyMessage, ParseMode: telegram.ParseMode},
			{ChatID: 1003, Text: telegram.PlanReadyMessage, ParseMode: telegram.ParseMode},
		}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("is never announced by confirming alone, before the planning workflow reports the plan ready", func(t *testing.T) {
		sender := &recordingSender{}
		identities := fakeIdentities{1001: {HouseholdID: "household-1", MemberID: "member-1"}}
		commands := telegram.NewCommands(identities, &recordingHouseholds{}, &recordingDraftPlans{}, &recordingConfirmations{}, sender)

		if err := commands.ConfirmPlan(context.Background(), 1001); err != nil {
			t.Fatalf("confirm plan: got %v, want nil", err)
		}

		for _, message := range sender.sent {
			if message.Text == telegram.PlanReadyMessage {
				t.Fatalf("sent messages: got %+v, want no ready message", sender.sent)
			}
		}
	})

	t.Run("does not message members of other households", func(t *testing.T) {
		sender := &recordingSender{}

		if err := telegram.NewPlanReadyNotifier(directory, sender).HandlePlanReady(context.Background(), "household-2"); err != nil {
			t.Fatalf("handle plan ready: got %v, want nil", err)
		}

		if got, want := sender.chatIDs(), []int64{2001}; !reflect.DeepEqual(got, want) {
			t.Fatalf("messaged chats: got %v, want %v", got, want)
		}
	})

	t.Run("reports a failure to resolve recipients and messages nobody", func(t *testing.T) {
		sender := &recordingSender{}
		lookupErr := errors.New("database is locked")

		err := telegram.NewPlanReadyNotifier(failingDirectory{err: lookupErr}, sender).HandlePlanReady(context.Background(), "household-1")
		if !errors.Is(err, lookupErr) {
			t.Errorf("handle plan ready: got %v, want it to wrap %v", err, lookupErr)
		}
		if len(sender.sent) != 0 {
			t.Errorf("sent messages: got %+v, want none", sender.sent)
		}
	})

	t.Run("still messages the remaining members after a failed send and reports it", func(t *testing.T) {
		sendErr := errors.New("bot was blocked by the user")
		sender := &recordingSender{failFor: map[int64]error{1001: sendErr}}

		err := telegram.NewPlanReadyNotifier(directory, sender).HandlePlanReady(context.Background(), "household-1")
		if !errors.Is(err, sendErr) {
			t.Errorf("handle plan ready: got %v, want it to wrap %v", err, sendErr)
		}
		if got, want := sender.chatIDs(), []int64{1001, 1003}; !reflect.DeepEqual(got, want) {
			t.Errorf("attempted chats: got %v, want %v", got, want)
		}
	})
}
