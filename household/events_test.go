package household_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/household"
)

func TestDraftPlanEvents(t *testing.T) {
	event := household.DraftPlanCreated{
		HouseholdID: "household-1",
		Dinners: []household.DraftDinner{
			{Night: "2026-10-05", Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
		},
	}

	t.Run("delivers a published draft to every subscriber in subscription order", func(t *testing.T) {
		var events household.DraftPlanEvents
		var got []string
		var received []household.DraftPlanCreated
		for _, name := range []string{"first", "second"} {
			events.Subscribe(func(_ context.Context, event household.DraftPlanCreated) error {
				got = append(got, name)
				received = append(received, event)
				return nil
			})
		}

		if err := events.Publish(context.Background(), event); err != nil {
			t.Fatalf("publish: got %v, want nil", err)
		}

		if want := []string{"first", "second"}; !reflect.DeepEqual(got, want) {
			t.Errorf("subscribers called: got %v, want %v", got, want)
		}
		if want := []household.DraftPlanCreated{event, event}; !reflect.DeepEqual(received, want) {
			t.Errorf("events received: got %+v, want %+v", received, want)
		}
	})

	t.Run("publishes without error when nobody has subscribed", func(t *testing.T) {
		var events household.DraftPlanEvents
		if err := events.Publish(context.Background(), event); err != nil {
			t.Fatalf("publish: got %v, want nil", err)
		}
	})

	t.Run("still notifies later subscribers when one fails and reports the failure", func(t *testing.T) {
		var events household.DraftPlanEvents
		subscriberErr := errors.New("telegram unavailable")
		events.Subscribe(func(context.Context, household.DraftPlanCreated) error { return subscriberErr })
		laterCalled := false
		events.Subscribe(func(context.Context, household.DraftPlanCreated) error {
			laterCalled = true
			return nil
		})

		err := events.Publish(context.Background(), event)
		if !errors.Is(err, subscriberErr) {
			t.Errorf("publish: got %v, want it to wrap %v", err, subscriberErr)
		}
		if !laterCalled {
			t.Errorf("later subscriber called: got false, want true")
		}
	})
}
