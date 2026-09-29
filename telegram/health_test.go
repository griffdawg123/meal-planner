package telegram_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/telegram"
)

func TestHealthCheck(t *testing.T) {
	message := func(chatID int64, text string) telegram.Update {
		return telegram.Update{UpdateID: 1, Message: &telegram.Message{Chat: telegram.Chat{ID: chatID}, Text: text}}
	}

	t.Run("answers /ping with pong in the same chat", func(t *testing.T) {
		sender := &recordingSender{}

		if err := telegram.NewHealthCheck(sender, "meal_planner_bot").HandleUpdate(context.Background(), message(1001, "/ping")); err != nil {
			t.Fatalf("handle update: got %v, want nil", err)
		}

		want := []sentMessage{{ChatID: 1001, Text: "pong", ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("answers a /ping addressed to the bot by name", func(t *testing.T) {
		sender := &recordingSender{}

		if err := telegram.NewHealthCheck(sender, "meal_planner_bot").HandleUpdate(context.Background(), message(-500, "/ping@meal_planner_bot")); err != nil {
			t.Fatalf("handle update: got %v, want nil", err)
		}

		want := []sentMessage{{ChatID: -500, Text: "pong", ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("answers a command addressed to the bot by name in any letter case", func(t *testing.T) {
		sender := &recordingSender{}

		if err := telegram.NewHealthCheck(sender, "meal_planner_bot").HandleUpdate(context.Background(), message(-500, "/echo@Meal_Planner_Bot hi")); err != nil {
			t.Fatalf("handle update: got %v, want nil", err)
		}

		want := []sentMessage{{ChatID: -500, Text: "hi", ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("ignores commands addressed to another bot", func(t *testing.T) {
		sender := &recordingSender{}
		health := telegram.NewHealthCheck(sender, "meal_planner_bot")

		for _, text := range []string{"/ping@another_bot", "/echo@another_bot text", "/ping@"} {
			if err := health.HandleUpdate(context.Background(), message(-500, text)); err != nil {
				t.Fatalf("handle update %q: got %v, want nil", text, err)
			}
		}

		if len(sender.sent) != 0 {
			t.Fatalf("sent messages: got %+v, want none", sender.sent)
		}
	})

	t.Run("echoes the text after /echo, escaped for the parse mode", func(t *testing.T) {
		sender := &recordingSender{}

		if err := telegram.NewHealthCheck(sender, "meal_planner_bot").HandleUpdate(context.Background(), message(1001, "/echo fish & <chips>")); err != nil {
			t.Fatalf("handle update: got %v, want nil", err)
		}

		want := []sentMessage{{ChatID: 1001, Text: "fish &amp; &lt;chips&gt;", ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("explains how to use /echo when it has no text", func(t *testing.T) {
		sender := &recordingSender{}

		if err := telegram.NewHealthCheck(sender, "meal_planner_bot").HandleUpdate(context.Background(), message(1001, "/echo")); err != nil {
			t.Fatalf("handle update: got %v, want nil", err)
		}

		want := []sentMessage{{ChatID: 1001, Text: "Send /echo followed by some text and I'll send it back.", ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", sender.sent, want)
		}
	})

	t.Run("ignores other messages and updates without a message", func(t *testing.T) {
		sender := &recordingSender{}
		health := telegram.NewHealthCheck(sender, "meal_planner_bot")

		for _, update := range []telegram.Update{message(1001, "hello"), message(1001, "/pinged"), {UpdateID: 2}} {
			if err := health.HandleUpdate(context.Background(), update); err != nil {
				t.Fatalf("handle update %+v: got %v, want nil", update, err)
			}
		}

		if len(sender.sent) != 0 {
			t.Fatalf("sent messages: got %+v, want none", sender.sent)
		}
	})

	t.Run("returns a failure to send the reply", func(t *testing.T) {
		sendErr := errors.New("bot was blocked by the user")
		sender := &recordingSender{failFor: map[int64]error{1001: sendErr}}

		err := telegram.NewHealthCheck(sender, "meal_planner_bot").HandleUpdate(context.Background(), message(1001, "/ping"))

		if !errors.Is(err, sendErr) {
			t.Fatalf("handle update: got %v, want %v", err, sendErr)
		}
	})
}
