package telegram_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/telegram"
)

func TestDeliverDraftPlan(t *testing.T) {
	dinners := []telegram.DraftDinner{
		{Night: "2026-10-05", Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
	}
	message, err := telegram.FormatDraftPlan(dinners)
	if err != nil {
		t.Fatalf("format draft plan: %v", err)
	}

	t.Run("sends the formatted draft to every linked member of the household", func(t *testing.T) {
		sender := &recordingSender{}
		members := []telegram.Recipient{
			{MemberID: "member-1", TelegramUserID: 1001},
			{MemberID: "member-2", TelegramUserID: 1002},
			{MemberID: "member-3", TelegramUserID: 1003},
		}

		if err := telegram.DeliverDraftPlan(context.Background(), sender, members, dinners); err != nil {
			t.Fatalf("deliver draft plan: %v", err)
		}

		want := []sentMessage{
			{ChatID: 1001, Text: message, ParseMode: telegram.ParseMode},
			{ChatID: 1002, Text: message, ParseMode: telegram.ParseMode},
			{ChatID: 1003, Text: message, ParseMode: telegram.ParseMode},
		}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("skips unlinked members without erroring and still messages the linked members after them", func(t *testing.T) {
		sender := &recordingSender{}
		members := []telegram.Recipient{
			{MemberID: "member-1"},
			{MemberID: "member-2", TelegramUserID: 1002},
			{MemberID: "member-3"},
			{MemberID: "member-4", TelegramUserID: 1004},
		}

		if err := telegram.DeliverDraftPlan(context.Background(), sender, members, dinners); err != nil {
			t.Fatalf("deliver draft plan: got %v, want nil", err)
		}

		want := []int64{1002, 1004}
		if got := sender.chatIDs(); !reflect.DeepEqual(got, want) {
			t.Fatalf("messaged chats: got %v, want %v", got, want)
		}
	})

	t.Run("sends nothing and does not error when no member is linked", func(t *testing.T) {
		sender := &recordingSender{}
		members := []telegram.Recipient{{MemberID: "member-1"}, {MemberID: "member-2"}}

		if err := telegram.DeliverDraftPlan(context.Background(), sender, members, dinners); err != nil {
			t.Fatalf("deliver draft plan: got %v, want nil", err)
		}
		if len(sender.sent) != 0 {
			t.Fatalf("sent messages: got %+v, want none", sender.sent)
		}
	})

	t.Run("keeps messaging the remaining members when one send fails and reports the failure", func(t *testing.T) {
		sendErr := errors.New("bot was blocked by the user")
		sender := &recordingSender{failFor: map[int64]error{1002: sendErr}}
		members := []telegram.Recipient{
			{MemberID: "member-1", TelegramUserID: 1001},
			{MemberID: "member-2", TelegramUserID: 1002},
			{MemberID: "member-3", TelegramUserID: 1003},
		}

		err := telegram.DeliverDraftPlan(context.Background(), sender, members, dinners)
		if !errors.Is(err, telegram.ErrDeliveryFailed) {
			t.Errorf("error: got %v, want %v", err, telegram.ErrDeliveryFailed)
		}
		if !errors.Is(err, sendErr) {
			t.Errorf("error: got %v, want it to wrap %v", err, sendErr)
		}

		want := []int64{1001, 1002, 1003}
		if got := sender.chatIDs(); !reflect.DeepEqual(got, want) {
			t.Fatalf("attempted chats: got %v, want %v", got, want)
		}
	})

	t.Run("rejects an invalid draft before messaging anyone", func(t *testing.T) {
		sender := &recordingSender{}
		members := []telegram.Recipient{{MemberID: "member-1", TelegramUserID: 1001}}

		err := telegram.DeliverDraftPlan(context.Background(), sender, members, nil)
		if !errors.Is(err, telegram.ErrInvalidDraft) {
			t.Fatalf("error: got %v, want %v", err, telegram.ErrInvalidDraft)
		}
		if len(sender.sent) != 0 {
			t.Fatalf("sent messages: got %+v, want none", sender.sent)
		}
	})
}

type sentMessage struct {
	ChatID    int64
	Text      string
	ParseMode string
}

// recordingSender records every message it is asked to send, failing those addressed to a chat in
// failFor with the given error.
type recordingSender struct {
	sent    []sentMessage
	failFor map[int64]error
}

func (s *recordingSender) SendMessage(_ context.Context, chatID int64, text, parseMode string) error {
	s.sent = append(s.sent, sentMessage{ChatID: chatID, Text: text, ParseMode: parseMode})
	return s.failFor[chatID]
}

func (s *recordingSender) chatIDs() []int64 {
	var chatIDs []int64
	for _, message := range s.sent {
		chatIDs = append(chatIDs, message.ChatID)
	}
	return chatIDs
}
