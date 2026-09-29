package telegram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

// ErrPollingConflict identifies a poll Telegram refused because a webhook is set for the bot or
// another process is already long polling with the same token. Retrying cannot succeed until that
// is resolved.
var ErrPollingConflict = errors.New("telegram refused long polling: delete the bot's webhook and stop any other process polling with the same token")

// Updates fetches incoming Bot API updates by long polling. Client implements it.
type Updates interface {
	GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error)
}

// Handler handles one incoming update.
type Handler interface {
	HandleUpdate(ctx context.Context, update Update) error
}

// HandlerFunc adapts a function to a Handler.
type HandlerFunc func(ctx context.Context, update Update) error

// HandleUpdate calls f.
func (f HandlerFunc) HandleUpdate(ctx context.Context, update Update) error {
	return f(ctx, update)
}

// Handlers passes each update to every one of its handlers in order, so independent features can
// each receive every incoming message. A failed handler does not stop the rest; every failure is
// returned together.
type Handlers []Handler

// HandleUpdate calls each handler with update.
func (h Handlers) HandleUpdate(ctx context.Context, update Update) error {
	var failures []error
	for _, handler := range h {
		if err := handler.HandleUpdate(ctx, update); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Poller receives updates by long polling and passes each to Handler, in order. Long polling needs
// no public URL, so it suits self-hosted deployments; it cannot be used while a webhook is set for
// the bot.
type Poller struct {
	Updates Updates
	Handler Handler
	// Timeout is how long each poll waits for an update to arrive.
	Timeout time.Duration
	// RetryDelay is how long to wait before polling again after a failed poll.
	RetryDelay time.Duration
	// OnError reports a failed poll or update. When nil, errors are logged.
	OnError func(error)
}

// Run polls until ctx is done, then returns ctx's error. A failed update is reported and skipped,
// so one bad message cannot stall the bot; a failed poll is reported and retried after RetryDelay.
// A poll refused with a conflict is not retried: Run returns it, wrapped in ErrPollingConflict.
func (p *Poller) Run(ctx context.Context) error {
	var offset int64
	for {
		updates, err := p.Updates.GetUpdates(ctx, offset, p.Timeout)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusConflict {
			return fmt.Errorf("%w: %w", ErrPollingConflict, err)
		}
		if err != nil {
			p.report(fmt.Errorf("poll telegram updates: %w", err))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(p.RetryDelay):
			}
			continue
		}
		for _, update := range updates {
			if err := p.Handler.HandleUpdate(ctx, update); err != nil {
				p.report(fmt.Errorf("handle telegram update %d: %w", update.UpdateID, err))
			}
			offset = update.UpdateID + 1
		}
	}
}

func (p *Poller) report(err error) {
	if p.OnError != nil {
		p.OnError(err)
		return
	}
	log.Print(err)
}
