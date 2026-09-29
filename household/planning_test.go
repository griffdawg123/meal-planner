package household_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/household"
)

var (
	padThai    = household.Meal{Title: "Pad thai", Satisfies: []string{"Thai"}, Conflicts: []string{"peanuts"}}
	greenCurry = household.Meal{Title: "Green curry", Satisfies: []string{"Thai"}}
	lasagne    = household.Meal{Title: "Lasagne", Satisfies: []string{"Italian"}}
	risotto    = household.Meal{Title: "Plain risotto"}
)

func TestPlanDinners(t *testing.T) {
	t.Run("never plans a meal blocked by a member's hard constraint even when household defaults favor it", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, "", sql.NullInt16{Int16: 5, Valid: true}, "Thai", household.Cuisine, household.Soft)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		nights := weekPreferences(t, service, created.ID, "2026-10-05", "2026-10-06", "2026-10-07")

		got := planDinners(t, nights, []household.Meal{padThai, risotto}, nil)

		want := []household.Meal{risotto, risotto, risotto}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("dinners: got %v, want %v", titles(got), titles(want))
		}
	})

	t.Run("matches a hard constraint to a meal regardless of letter case", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{}, "Peanuts", household.Allergy, household.Hard)
		nights := weekPreferences(t, service, created.ID, "2026-10-05")

		got := planDinners(t, nights, []household.Meal{padThai, risotto}, nil)

		if want := []household.Meal{risotto}; !reflect.DeepEqual(got, want) {
			t.Fatalf("dinners: got %v, want %v", titles(got), titles(want))
		}
	})

	t.Run("allows a member's blocked meal only on nights that member is away", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, "", sql.NullInt16{Int16: 5, Valid: true}, "Thai", household.Cuisine, household.Soft)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("record away night: %v", err)
		}
		nights := weekPreferences(t, service, created.ID, "2026-10-05", "2026-10-06", "2026-10-07")

		got := planDinners(t, nights, []household.Meal{padThai, risotto}, nil)

		want := []household.Meal{risotto, padThai, risotto}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("dinners: got %v, want %v", titles(got), titles(want))
		}
	})

	t.Run("reports a night on which every meal is blocked", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		nights := weekPreferences(t, service, created.ID, "2026-10-05")

		_, err := household.PlanDinners(nights, []household.Meal{padThai}, nil)
		if !errors.Is(err, household.ErrNoSuitableMeal) {
			t.Fatalf("error: got %v, want ErrNoSuitableMeal", err)
		}
	})

	t.Run("shares dinners between members with equally strong conflicting preferences", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, created.CreatorID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Thai", household.Cuisine, household.Soft)
		nights := weekPreferences(t, service, created.ID, "2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10")

		got := planDinners(t, nights, []household.Meal{lasagne, greenCurry}, nil)

		if italian, thai := count(got, lasagne), count(got, greenCurry); italian != 3 || thai != 3 {
			t.Errorf("Italian and Thai dinners: got %d and %d, want 3 and 3", italian, thai)
		}
		if streak := longestStreak(got); streak != 1 {
			t.Errorf("longest run of one meal: got %d, want 1 in %v", streak, titles(got))
		}
	})

	t.Run("gives a stronger preference more dinners without letting it dominate", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, created.CreatorID, sql.NullInt16{Int16: 5, Valid: true}, "Italian", household.Cuisine, household.Soft)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 3, Valid: true}, "Thai", household.Cuisine, household.Soft)
		nights := weekPreferences(t, service, created.ID, "2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10")

		got := planDinners(t, nights, []household.Meal{lasagne, greenCurry}, nil)

		italian, thai := count(got, lasagne), count(got, greenCurry)
		if italian <= thai {
			t.Errorf("Italian dinners: got %d, want more than the %d Thai dinners", italian, thai)
		}
		if thai < 2 {
			t.Errorf("Thai dinners: got %d, want at least 2 of 6 in %v", thai, titles(got))
		}
		if streak := longestStreak(got); streak > 2 {
			t.Errorf("longest run of one meal: got %d, want at most 2 in %v", streak, titles(got))
		}
	})

	t.Run("balances preferences across separate planning runs using recent dinners", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, created.CreatorID, sql.NullInt16{Int16: 5, Valid: true}, "Italian", household.Cuisine, household.Soft)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 3, Valid: true}, "Thai", household.Cuisine, household.Soft)

		var recent []household.Meal
		for _, night := range []string{"2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08", "2026-10-09", "2026-10-10"} {
			planned := planDinners(t, weekPreferences(t, service, created.ID, night), []household.Meal{lasagne, greenCurry}, recent)
			recent = append(recent, planned...)
		}

		italian, thai := count(recent, lasagne), count(recent, greenCurry)
		if italian <= thai {
			t.Errorf("Italian dinners: got %d, want more than the %d Thai dinners", italian, thai)
		}
		if thai < 2 {
			t.Errorf("Thai dinners: got %d, want at least 2 of 6 in %v", thai, titles(recent))
		}
	})

	t.Run("favors present members over an away member's equally strong preference", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, created.CreatorID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Thai", household.Cuisine, household.Soft)
		if err := service.RecordAwayNights(context.Background(), created.ID, created.CreatorID, "2026-10-05"); err != nil {
			t.Fatalf("record away night: %v", err)
		}
		nights := weekPreferences(t, service, created.ID, "2026-10-05")

		got := planDinners(t, nights, []household.Meal{lasagne, greenCurry}, nil)

		if want := []household.Meal{greenCurry}; !reflect.DeepEqual(got, want) {
			t.Fatalf("dinners: got %v, want %v", titles(got), titles(want))
		}
	})

	t.Run("avoids a meal that conflicts with a soft preference when another is available", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 3, Valid: true}, "peanuts", household.Dislike, household.Soft)
		nights := weekPreferences(t, service, created.ID, "2026-10-05")

		got := planDinners(t, nights, []household.Meal{padThai, risotto}, nil)

		if want := []household.Meal{risotto}; !reflect.DeepEqual(got, want) {
			t.Fatalf("dinners: got %v, want %v", titles(got), titles(want))
		}
	})
}

func weekPreferences(t *testing.T, service *household.Service, householdID string, nights ...string) []household.NightPreferences {
	t.Helper()

	week := make([]household.NightPreferences, 0, len(nights))
	for _, night := range nights {
		week = append(week, nightPreferences(t, service, householdID, night))
	}
	return week
}

func planDinners(t *testing.T, nights []household.NightPreferences, candidates, recent []household.Meal) []household.Meal {
	t.Helper()

	dinners, err := household.PlanDinners(nights, candidates, recent)
	if err != nil {
		t.Fatalf("plan dinners: %v", err)
	}
	if len(dinners) != len(nights) {
		t.Fatalf("dinner count: got %d, want %d", len(dinners), len(nights))
	}
	return dinners
}

func titles(meals []household.Meal) []string {
	names := make([]string, 0, len(meals))
	for _, meal := range meals {
		names = append(names, meal.Title)
	}
	return names
}

func count(meals []household.Meal, want household.Meal) int {
	n := 0
	for _, meal := range meals {
		if meal.Title == want.Title {
			n++
		}
	}
	return n
}

// longestStreak is the most consecutive dinners of the same meal.
func longestStreak(meals []household.Meal) int {
	longest, current := 0, 0
	for i, meal := range meals {
		if i > 0 && meal.Title == meals[i-1].Title {
			current++
		} else {
			current = 1
		}
		longest = max(longest, current)
	}
	return longest
}
