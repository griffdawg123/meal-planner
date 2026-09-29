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

func TestPlannerReviseDinner(t *testing.T) {
	satay := household.Meal{Title: "Satay chicken", Description: "Chicken skewers with peanut sauce.", Conflicts: []string{"peanuts"}}
	padThai := household.Meal{Title: "Pad thai", Description: "Rice noodles with tofu and lime."}

	// newAllergicHousehold creates a household whose founder is allergic to peanuts and a planner that
	// records every draft it publishes.
	newAllergicHousehold := func(t *testing.T) (*household.Service, household.Household, *household.Planner, *[]household.DraftPlanCreated) {
		t.Helper()
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(context.Background(), "Household", "Australia/Sydney", "Founder")
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, created.CreatorID, sql.NullInt16{}, "peanuts", household.Allergy, household.Hard); err != nil {
			t.Fatalf("add founder allergy: %v", err)
		}
		var events household.DraftPlanEvents
		published := &[]household.DraftPlanCreated{}
		events.Subscribe(func(_ context.Context, event household.DraftPlanCreated) error {
			*published = append(*published, event)
			return nil
		})
		return service, created, household.NewPlanner(service, &events), published
	}

	t.Run("publishes the revised dinner when it meets every present member's hard constraints", func(t *testing.T) {
		_, created, planner, published := newAllergicHousehold(t)

		if err := planner.ReviseDinner(context.Background(), created.ID, "2026-10-05", padThai); err != nil {
			t.Fatalf("revise dinner: got %v, want nil", err)
		}

		want := []household.DraftPlanCreated{{
			HouseholdID: created.ID,
			Dinners:     []household.DraftDinner{{Night: "2026-10-05", Title: "Pad thai", Description: "Rice noodles with tofu and lime."}},
		}}
		if !reflect.DeepEqual(*published, want) {
			t.Errorf("published events: got %+v, want %+v", *published, want)
		}
	})

	t.Run("blocks a dinner that breaks a present member's allergy and publishes nothing", func(t *testing.T) {
		_, created, planner, published := newAllergicHousehold(t)

		err := planner.ReviseDinner(context.Background(), created.ID, "2026-10-05", satay)
		if !errors.Is(err, household.ErrHardConstraint) {
			t.Errorf("revise dinner: got %v, want it to wrap %v", err, household.ErrHardConstraint)
		}
		if len(*published) != 0 {
			t.Errorf("published events: got %+v, want none", *published)
		}
	})

	t.Run("allows a dinner that only breaks the allergy of a member who is away that night", func(t *testing.T) {
		service, created, planner, published := newAllergicHousehold(t)
		if err := service.RecordAwayNights(context.Background(), created.ID, created.CreatorID, "2026-10-05"); err != nil {
			t.Fatalf("record away night: %v", err)
		}

		if err := planner.ReviseDinner(context.Background(), created.ID, "2026-10-05", satay); err != nil {
			t.Fatalf("revise dinner: got %v, want nil", err)
		}
		if len(*published) != 1 {
			t.Errorf("published events: got %d, want 1", len(*published))
		}
	})

	t.Run("reports an unknown household and publishes nothing", func(t *testing.T) {
		_, _, planner, published := newAllergicHousehold(t)

		err := planner.ReviseDinner(context.Background(), "no-such-household", "2026-10-05", padThai)
		if !errors.Is(err, household.ErrHouseholdNotFound) {
			t.Errorf("revise dinner: got %v, want it to wrap %v", err, household.ErrHouseholdNotFound)
		}
		if len(*published) != 0 {
			t.Errorf("published events: got %+v, want none", *published)
		}
	})
}
