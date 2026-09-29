package household_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/household"
)

func TestPlannerCreateDraftPlan(t *testing.T) {
	candidates := []household.Meal{
		{Title: "Satay chicken", Description: "Chicken skewers with peanut sauce.", Conflicts: []string{"peanuts"}},
		{Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
	}

	t.Run("publishes the draft with the household's dinners to subscribers", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(context.Background(), "Household", "Australia/Sydney", "Founder")
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard); err != nil {
			t.Fatalf("add household allergy: %v", err)
		}
		var events household.DraftPlanEvents
		var published []household.DraftPlanCreated
		events.Subscribe(func(_ context.Context, event household.DraftPlanCreated) error {
			published = append(published, event)
			return nil
		})

		dinners, err := household.NewPlanner(service, &events).CreateDraftPlan(
			context.Background(), created.ID, []string{"2026-10-05", "2026-10-06"}, candidates, nil,
		)
		if err != nil {
			t.Fatalf("create draft plan: got %v, want nil", err)
		}

		if want := []household.Meal{candidates[1], candidates[1]}; !reflect.DeepEqual(dinners, want) {
			t.Errorf("dinners: got %+v, want %+v", dinners, want)
		}
		want := []household.DraftPlanCreated{{
			HouseholdID: created.ID,
			Dinners: []household.DraftDinner{
				{Night: "2026-10-05", Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
				{Night: "2026-10-06", Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
			},
		}}
		if !reflect.DeepEqual(published, want) {
			t.Errorf("published events: got %+v, want %+v", published, want)
		}
	})

	t.Run("publishes nothing when no draft can be planned", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(context.Background(), "Household", "Australia/Sydney", "Founder")
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard); err != nil {
			t.Fatalf("add household allergy: %v", err)
		}
		var events household.DraftPlanEvents
		published := 0
		events.Subscribe(func(context.Context, household.DraftPlanCreated) error {
			published++
			return nil
		})

		_, err = household.NewPlanner(service, &events).CreateDraftPlan(
			context.Background(), created.ID, []string{"2026-10-05"}, candidates[:1], nil,
		)
		if !errors.Is(err, household.ErrNoSuitableMeal) {
			t.Errorf("create draft plan: got %v, want it to wrap %v", err, household.ErrNoSuitableMeal)
		}
		if published != 0 {
			t.Errorf("published events: got %d, want 0", published)
		}
	})

	t.Run("returns the draft and reports a subscriber failure", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(context.Background(), "Household", "Australia/Sydney", "Founder")
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		var events household.DraftPlanEvents
		subscriberErr := errors.New("telegram unavailable")
		events.Subscribe(func(context.Context, household.DraftPlanCreated) error { return subscriberErr })

		dinners, err := household.NewPlanner(service, &events).CreateDraftPlan(
			context.Background(), created.ID, []string{"2026-10-05"}, candidates[1:], nil,
		)
		if !errors.Is(err, subscriberErr) {
			t.Errorf("create draft plan: got %v, want it to wrap %v", err, subscriberErr)
		}
		if want := candidates[1:]; !reflect.DeepEqual(dinners, want) {
			t.Errorf("dinners: got %+v, want %+v", dinners, want)
		}
	})
}
