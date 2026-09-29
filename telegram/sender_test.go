package telegram_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/griffdawg123/meal-planner/telegram"
)

func TestRetryingSender(t *testing.T) {
	rateLimited := func(retryAfter time.Duration) error {
		return &telegram.APIError{Method: "sendMessage", Code: 429, Description: "Too Many Requests", RetryAfter: retryAfter}
	}

	t.Run("sends the message once when Telegram accepts it", func(t *testing.T) {
		inner := &scriptedSender{}
		sender := &telegram.RetryingSender{Sender: inner, MaxRetries: 2}

		if err := sender.SendMessage(context.Background(), 1001, "hello", telegram.ParseMode); err != nil {
			t.Fatalf("send message: got %v, want nil", err)
		}

		want := []sentMessage{{ChatID: 1001, Text: "hello", ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(inner.sent, want) {
			t.Fatalf("sent messages: got %+v, want %+v", inner.sent, want)
		}
	})

	t.Run("waits as long as Telegram asks and resends a rate-limited message", func(t *testing.T) {
		inner := &scriptedSender{failures: []error{rateLimited(2 * time.Second), rateLimited(3 * time.Second)}}
		var waited []time.Duration
		sender := &telegram.RetryingSender{
			Sender:     inner,
			MaxRetries: 2,
			Wait: func(_ context.Context, d time.Duration) error {
				waited = append(waited, d)
				return nil
			},
		}

		if err := sender.SendMessage(context.Background(), 1001, "hello", ""); err != nil {
			t.Fatalf("send message: got %v, want nil", err)
		}

		if len(inner.sent) != 3 {
			t.Errorf("send attempts: got %d, want 3", len(inner.sent))
		}
		if want := []time.Duration{2 * time.Second, 3 * time.Second}; !reflect.DeepEqual(waited, want) {
			t.Errorf("waits: got %v, want %v", waited, want)
		}
	})

	t.Run("returns the rate limit once the retries are used up", func(t *testing.T) {
		limit := rateLimited(time.Second)
		inner := &scriptedSender{failures: []error{limit, limit, limit}}
		sender := &telegram.RetryingSender{
			Sender:     inner,
			MaxRetries: 1,
			Wait:       func(context.Context, time.Duration) error { return nil },
		}

		err := sender.SendMessage(context.Background(), 1001, "hello", "")

		if !errors.Is(err, limit) {
			t.Fatalf("send message: got %v, want %v", err, limit)
		}
		if len(inner.sent) != 2 {
			t.Errorf("send attempts: got %d, want 2", len(inner.sent))
		}
	})

	t.Run("does not resend after any other failure, which may have been delivered", func(t *testing.T) {
		for _, failure := range []error{
			&telegram.APIError{Method: "sendMessage", Code: 403, Description: "Forbidden: bot was blocked by the user"},
			&telegram.APIError{Method: "sendMessage", Code: 429, Description: "Too Many Requests"},
			errors.New("connection reset"),
		} {
			inner := &scriptedSender{failures: []error{failure}}
			sender := &telegram.RetryingSender{
				Sender:     inner,
				MaxRetries: 3,
				Wait: func(context.Context, time.Duration) error {
					t.Errorf("waited after %v, want no retry", failure)
					return nil
				},
			}

			if err := sender.SendMessage(context.Background(), 1001, "hello", ""); !errors.Is(err, failure) {
				t.Errorf("send message: got %v, want %v", err, failure)
			}
			if len(inner.sent) != 1 {
				t.Errorf("send attempts after %v: got %d, want 1", failure, len(inner.sent))
			}
		}
	})

	t.Run("stops waiting when the context is done", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		inner := &scriptedSender{failures: []error{rateLimited(time.Hour)}}
		sender := &telegram.RetryingSender{Sender: inner, MaxRetries: 1}

		err := sender.SendMessage(ctx, 1001, "hello", "")

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("send message: got %v, want %v", err, context.Canceled)
		}
		if len(inner.sent) != 1 {
			t.Errorf("send attempts: got %d, want 1", len(inner.sent))
		}
	})
}

// scriptedSender records every message it is asked to send, failing the attempts in order with
// failures until they run out.
type scriptedSender struct {
	failures []error
	sent     []sentMessage
}

func (s *scriptedSender) SendMessage(_ context.Context, chatID int64, text, parseMode string) error {
	attempt := len(s.sent)
	s.sent = append(s.sent, sentMessage{ChatID: chatID, Text: text, ParseMode: parseMode})
	if attempt < len(s.failures) {
		return s.failures[attempt]
	}
	return nil
}
