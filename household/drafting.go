package household

import (
	"context"
	"fmt"
)

// Planner is the planning workflow: it produces a household's draft plan and publishes it to the
// draft-plan-created subscribers.
type Planner struct {
	households *Service
	events     *DraftPlanEvents
}

// NewPlanner returns a planner that resolves preferences through households and publishes each draft
// it creates to events.
func NewPlanner(households *Service, events *DraftPlanEvents) *Planner {
	return &Planner{households: households, events: events}
}

// CreateDraftPlan resolves the household's preferences for each night, chooses a dinner per night as
// PlanDinners does, and publishes the draft to every subscriber. Nothing is published if a draft
// cannot be planned. If a subscriber fails, the draft is still returned along with the failure.
func (p *Planner) CreateDraftPlan(ctx context.Context, householdID string, nights []string, candidates, recent []Meal) ([]Meal, error) {
	preferences := make([]NightPreferences, 0, len(nights))
	for _, night := range nights {
		resolved, err := p.households.NightPreferences(ctx, householdID, night)
		if err != nil {
			return nil, err
		}
		preferences = append(preferences, resolved)
	}
	dinners, err := PlanDinners(preferences, candidates, recent)
	if err != nil {
		return nil, err
	}

	event := DraftPlanCreated{HouseholdID: householdID, Dinners: make([]DraftDinner, 0, len(dinners))}
	for i, dinner := range dinners {
		event.Dinners = append(event.Dinners, DraftDinner{Night: nights[i], Title: dinner.Title, Description: dinner.Description})
	}
	if err := p.events.Publish(ctx, event); err != nil {
		return dinners, fmt.Errorf("publish draft plan for household %q: %w", householdID, err)
	}
	return dinners, nil
}

// ReviseDinner replaces one night's dinner with meal, as when a member asks for a different dinner,
// and publishes the revised night to every subscriber. The household's preferences for that night
// are resolved first, and a meal that breaks any hard constraint of a member present that night
// yields ErrHardConstraint without anything being published.
func (p *Planner) ReviseDinner(ctx context.Context, householdID, night string, meal Meal) error {
	preferences, err := p.households.NightPreferences(ctx, householdID, night)
	if err != nil {
		return err
	}
	if blocked(meal, preferences.HardConstraints) {
		return fmt.Errorf("%w: %q on %s", ErrHardConstraint, meal.Title, night)
	}

	event := DraftPlanCreated{
		HouseholdID: householdID,
		Dinners:     []DraftDinner{{Night: night, Title: meal.Title, Description: meal.Description}},
	}
	if err := p.events.Publish(ctx, event); err != nil {
		return fmt.Errorf("publish revised dinner for household %q: %w", householdID, err)
	}
	return nil
}
