package telegram_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/telegram"
)

func TestHandlers(t *testing.T) {
	t.Run("passes each update to every handler in order", func(t *testing.T) {
		var calls []string
		record := func(name string) telegram.Handler {
			return telegram.HandlerFunc(func(_ context.Context, update telegram.Update) error {
				calls = append(calls, name)
				if update.UpdateID != 7 {
					t.Errorf("%s: got update %d, want 7", name, update.UpdateID)
				}
				return nil
			})
		}
		handlers := telegram.Handlers{record("health"), record("commands")}

		if err := handlers.HandleUpdate(context.Background(), telegram.Update{UpdateID: 7}); err != nil {
			t.Fatalf("handle update: got %v, want nil", err)
		}

		if want := []string{"health", "commands"}; !reflect.DeepEqual(calls, want) {
			t.Fatalf("handlers called: got %v, want %v", calls, want)
		}
	})

	t.Run("still calls later handlers after one fails and returns every failure", func(t *testing.T) {
		first := errors.New("first failed")
		third := errors.New("third failed")
		var calls int
		failWith := func(err error) telegram.Handler {
			return telegram.HandlerFunc(func(context.Context, telegram.Update) error {
				calls++
				return err
			})
		}
		handlers := telegram.Handlers{failWith(first), failWith(nil), failWith(third)}

		err := handlers.HandleUpdate(context.Background(), telegram.Update{UpdateID: 1})

		if !errors.Is(err, first) || !errors.Is(err, third) {
			t.Errorf("handle update: got %v, want it to wrap %v and %v", err, first, third)
		}
		if calls != 3 {
			t.Errorf("handlers called: got %d, want 3", calls)
		}
	})
}
