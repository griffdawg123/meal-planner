package telegram

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// RetryingSender sends messages through Sender, resending a message Telegram rejected for exceeding
// its rate limit once the wait Telegram asked for has passed. Any other failure is returned without
// resending, because the message may already have been delivered.
type RetryingSender struct {
	Sender Sender
	// MaxRetries is how many times a rate-limited message is resent before the rate limit is
	// returned.
	MaxRetries int
	// Wait pauses for d, returning early with ctx's error if ctx is done first. When nil, it
	// sleeps.
	Wait func(ctx context.Context, d time.Duration) error
}

// SendMessage sends text to chatID as Sender does, retrying while Telegram rate limits it.
func (s *RetryingSender) SendMessage(ctx context.Context, chatID int64, text, parseMode string) error {
	for retries := 0; ; retries++ {
		err := s.Sender.SendMessage(ctx, chatID, text, parseMode)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != http.StatusTooManyRequests || apiErr.RetryAfter <= 0 || retries >= s.MaxRetries {
			return err
		}
		if waitErr := s.wait(ctx, apiErr.RetryAfter); waitErr != nil {
			return errors.Join(err, waitErr)
		}
	}
}

func (s *RetryingSender) wait(ctx context.Context, d time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
