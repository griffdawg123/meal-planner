package household

import (
	"context"
	"errors"
	"sync"
)

// DraftDinner is one night of a draft plan. Night is a YYYY-MM-DD date in the household's
// timezone, and Description is a short summary of the dinner rather than a full recipe.
type DraftDinner struct {
	Night       string
	Title       string
	Description string
}

// DraftPlanCreated is published when the planning workflow produces a draft plan for a household.
type DraftPlanCreated struct {
	HouseholdID string
	Dinners     []DraftDinner
}

// DraftPlanCreatedHandler reacts to a draft plan, such as by sending it to household members.
type DraftPlanCreatedHandler func(ctx context.Context, event DraftPlanCreated) error

// DraftPlanEvents delivers draft-plan-created events from the planning workflow to its
// subscribers. The zero value has no subscribers and is ready to use; it is safe for concurrent use.
type DraftPlanEvents struct {
	mu       sync.Mutex
	handlers []DraftPlanCreatedHandler
}

// Subscribe registers handler to be called for every draft plan published afterwards.
func (e *DraftPlanEvents) Subscribe(handler DraftPlanCreatedHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = append(e.handlers, handler)
}

// Publish calls every subscriber with event, in subscription order. A failing subscriber does not
// stop the remaining subscribers from being called; the failures are returned together.
func (e *DraftPlanEvents) Publish(ctx context.Context, event DraftPlanCreated) error {
	e.mu.Lock()
	handlers := append([]DraftPlanCreatedHandler(nil), e.handlers...)
	e.mu.Unlock()

	var failures []error
	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
