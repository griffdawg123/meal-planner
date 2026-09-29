package household_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/griffdawg123/meal-planner/household"
)

func TestNightPreferences(t *testing.T) {
	t.Run("weights every soft preference by its strength when everyone is present", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		shared := addPreference(t, service, created.ID, "", sql.NullInt16{Int16: 2, Valid: true}, "Thai", household.Cuisine, household.Soft)
		own := addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft)

		got := nightPreferences(t, service, created.ID, "2026-10-06")

		want := []household.WeightedPreference{
			{Preference: shared, Weight: 2},
			{Preference: own, Weight: 4},
		}
		if !reflect.DeepEqual(sortedSoft(got.SoftPreferences), sortedSoft(want)) {
			t.Fatalf("soft preferences: got %+v, want %+v", got.SoftPreferences, want)
		}
	})

	t.Run("down-weights an away member's soft preferences for that night only", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		away := addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft)
		present := addPreference(t, service, created.ID, created.CreatorID, sql.NullInt16{Int16: 4, Valid: true}, "spicy", household.Cuisine, household.Soft)
		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("record away night: %v", err)
		}

		gotAway := nightPreferences(t, service, created.ID, "2026-10-06")
		wantAway := []household.WeightedPreference{
			{Preference: away, Weight: 4 * household.AwaySoftPreferenceFactor},
			{Preference: present, Weight: 4},
		}
		if !reflect.DeepEqual(sortedSoft(gotAway.SoftPreferences), sortedSoft(wantAway)) {
			t.Fatalf("away night soft preferences: got %+v, want %+v", gotAway.SoftPreferences, wantAway)
		}
		if weight := wantAway[0].Weight; weight <= 0 || weight >= 4 {
			t.Errorf("away member weight: got %v, want between 0 and 4 exclusive", weight)
		}

		gotHome := nightPreferences(t, service, created.ID, "2026-10-07")
		wantHome := []household.WeightedPreference{
			{Preference: away, Weight: 4},
			{Preference: present, Weight: 4},
		}
		if !reflect.DeepEqual(sortedSoft(gotHome.SoftPreferences), sortedSoft(wantHome)) {
			t.Fatalf("home night soft preferences: got %+v, want %+v", gotHome.SoftPreferences, wantHome)
		}
	})

	t.Run("keeps household-wide soft preferences at full weight when members are away", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		shared := addPreference(t, service, created.ID, "", sql.NullInt16{Int16: 3, Valid: true}, "under 30 minutes", household.CookingTime, household.Soft)
		for _, memberID := range []string{created.CreatorID, member.ID} {
			if err := service.RecordAwayNights(context.Background(), created.ID, memberID, "2026-10-06"); err != nil {
				t.Fatalf("record away night: %v", err)
			}
		}

		got := nightPreferences(t, service, created.ID, "2026-10-06")

		want := []household.WeightedPreference{{Preference: shared, Weight: 3}}
		if !reflect.DeepEqual(got.SoftPreferences, want) {
			t.Fatalf("soft preferences: got %+v, want %+v", got.SoftPreferences, want)
		}
	})

	t.Run("enforces hard constraints of present members and the household but not away members", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		shared := addPreference(t, service, created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		present := addPreference(t, service, created.ID, created.CreatorID, sql.NullInt16{}, "vegetarian", household.DietaryRestriction, household.Hard)
		away := addPreference(t, service, created.ID, member.ID, sql.NullInt16{}, "shellfish", household.Allergy, household.Hard)
		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("record away night: %v", err)
		}

		gotAway := nightPreferences(t, service, created.ID, "2026-10-06")
		wantAway := []household.Preference{shared, present}
		if !reflect.DeepEqual(sortedHard(gotAway.HardConstraints), sortedHard(wantAway)) {
			t.Fatalf("away night hard constraints: got %+v, want %+v", gotAway.HardConstraints, wantAway)
		}
		if len(gotAway.SoftPreferences) != 0 {
			t.Errorf("away night soft preferences: got %+v, want none", gotAway.SoftPreferences)
		}

		gotHome := nightPreferences(t, service, created.ID, "2026-10-07")
		wantHome := []household.Preference{shared, present, away}
		if !reflect.DeepEqual(sortedHard(gotHome.HardConstraints), sortedHard(wantHome)) {
			t.Fatalf("home night hard constraints: got %+v, want %+v", gotHome.HardConstraints, wantHome)
		}
	})

	t.Run("applies each member's away nights only to that member on those nights", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		sharedHard := addPreference(t, service, created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		founderHard := addPreference(t, service, created.ID, created.CreatorID, sql.NullInt16{}, "vegetarian", household.DietaryRestriction, household.Hard)
		founderSoft := addPreference(t, service, created.ID, created.CreatorID, sql.NullInt16{Int16: 4, Valid: true}, "spicy", household.Cuisine, household.Soft)
		memberHard := addPreference(t, service, created.ID, member.ID, sql.NullInt16{}, "shellfish", household.Allergy, household.Hard)
		memberSoft := addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 2, Valid: true}, "Italian", household.Cuisine, household.Soft)
		if err := service.RecordAwayNights(context.Background(), created.ID, created.CreatorID, "2026-10-06"); err != nil {
			t.Fatalf("record founder away night: %v", err)
		}
		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-08"); err != nil {
			t.Fatalf("record member away night: %v", err)
		}

		for _, test := range []struct {
			night    string
			wantHard []household.Preference
			wantSoft []household.WeightedPreference
		}{
			{
				night:    "2026-10-06",
				wantHard: []household.Preference{sharedHard, memberHard},
				wantSoft: []household.WeightedPreference{
					{Preference: founderSoft, Weight: 4 * household.AwaySoftPreferenceFactor},
					{Preference: memberSoft, Weight: 2},
				},
			},
			{
				night:    "2026-10-07",
				wantHard: []household.Preference{sharedHard, founderHard, memberHard},
				wantSoft: []household.WeightedPreference{
					{Preference: founderSoft, Weight: 4},
					{Preference: memberSoft, Weight: 2},
				},
			},
			{
				night:    "2026-10-08",
				wantHard: []household.Preference{sharedHard, founderHard},
				wantSoft: []household.WeightedPreference{
					{Preference: founderSoft, Weight: 4},
					{Preference: memberSoft, Weight: 2 * household.AwaySoftPreferenceFactor},
				},
			},
		} {
			got := nightPreferences(t, service, created.ID, test.night)
			if !reflect.DeepEqual(sortedHard(got.HardConstraints), sortedHard(test.wantHard)) {
				t.Errorf("%s hard constraints: got %+v, want %+v", test.night, got.HardConstraints, test.wantHard)
			}
			if !reflect.DeepEqual(sortedSoft(got.SoftPreferences), sortedSoft(test.wantSoft)) {
				t.Errorf("%s soft preferences: got %+v, want %+v", test.night, got.SoftPreferences, test.wantSoft)
			}
		}
	})

	t.Run("reflects attendance changed after the initial draft", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		own := addPreference(t, service, created.ID, member.ID, sql.NullInt16{Int16: 5, Valid: true}, "mushrooms", household.Dislike, household.Soft)

		if got := nightPreferences(t, service, created.ID, "2026-10-06").SoftPreferences[0].Weight; got != 5 {
			t.Fatalf("draft weight: got %v, want 5", got)
		}

		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("record away night: %v", err)
		}
		got := nightPreferences(t, service, created.ID, "2026-10-06").SoftPreferences
		want := []household.WeightedPreference{{Preference: own, Weight: 5 * household.AwaySoftPreferenceFactor}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("revised soft preferences: got %+v, want %+v", got, want)
		}

		if err := service.ClearAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("clear away night: %v", err)
		}
		if got := nightPreferences(t, service, created.ID, "2026-10-06").SoftPreferences[0].Weight; got != 5 {
			t.Fatalf("weight after returning: got %v, want 5", got)
		}
	})

	t.Run("ignores another household's preferences and away nights", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		first, _ := newHouseholdWithMember(t, service)
		second, otherMember := newHouseholdWithMember(t, service)
		addPreference(t, service, second.ID, otherMember.ID, sql.NullInt16{}, "gluten", household.Allergy, household.Hard)
		if err := service.RecordAwayNights(context.Background(), second.ID, otherMember.ID, "2026-10-06"); err != nil {
			t.Fatalf("record away night: %v", err)
		}

		got := nightPreferences(t, service, first.ID, "2026-10-06")

		want := household.NightPreferences{
			Night:           "2026-10-06",
			HardConstraints: []household.Preference{},
			SoftPreferences: []household.WeightedPreference{},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("night preferences: got %#v, want %#v", got, want)
		}
	})

	t.Run("rejects an invalid night", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, _ := newHouseholdWithMember(t, service)

		_, err := service.NightPreferences(context.Background(), created.ID, "2026-02-30")
		if !errors.Is(err, household.ErrInvalidInput) {
			t.Fatalf("error: got %v, want ErrInvalidInput", err)
		}
	})

	t.Run("rejects an unknown household", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))

		_, err := service.NightPreferences(context.Background(), "missing-household", "2026-10-06")
		if !errors.Is(err, household.ErrHouseholdNotFound) {
			t.Fatalf("error: got %v, want ErrHouseholdNotFound", err)
		}
	})
}

func addPreference(t *testing.T, service *household.Service, householdID, memberID string, strength sql.NullInt16, value string, category household.PreferenceCategory, kind household.PreferenceKind) household.Preference {
	t.Helper()

	preference, err := service.AddPreference(context.Background(), householdID, memberID, strength, value, category, kind)
	if err != nil {
		t.Fatalf("add preference: %v", err)
	}
	return preference
}

func nightPreferences(t *testing.T, service *household.Service, householdID, night string) household.NightPreferences {
	t.Helper()

	preferences, err := service.NightPreferences(context.Background(), householdID, night)
	if err != nil {
		t.Fatalf("night preferences: %v", err)
	}
	return preferences
}

// sortedHard orders preferences by ID so comparisons do not depend on rows created within the same
// millisecond keeping their insertion order.
func sortedHard(preferences []household.Preference) []household.Preference {
	return slices.SortedFunc(slices.Values(preferences), func(a, b household.Preference) int {
		return strings.Compare(a.ID, b.ID)
	})
}

// sortedSoft orders weighted preferences by ID for the same reason as sortedHard.
func sortedSoft(preferences []household.WeightedPreference) []household.WeightedPreference {
	return slices.SortedFunc(slices.Values(preferences), func(a, b household.WeightedPreference) int {
		return strings.Compare(a.ID, b.ID)
	})
}
