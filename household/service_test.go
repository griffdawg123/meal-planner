package household_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	mealdb "github.com/griffdawg123/meal-planner/db"
	"github.com/griffdawg123/meal-planner/household"
	_ "modernc.org/sqlite"
)

func TestCreateHousehold(t *testing.T) {
	t.Run("creates the household with exactly the creator as a member", func(t *testing.T) {
		database := newTestDatabase(t)
		service := household.NewService(database)

		created, err := service.CreateHousehold(
			context.Background(), " Test Household ", "Australia/Sydney", " Test Creator ",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}

		var name, timezone, creatorID string
		if err := database.QueryRow(
			`SELECT name, timezone, created_by_member_id FROM household WHERE id = ?`, created.ID,
		).Scan(&name, &timezone, &creatorID); err != nil {
			t.Fatalf("read household: %v", err)
		}
		if name != "Test Household" {
			t.Errorf("household name: got %q, want %q", name, "Test Household")
		}
		if timezone != "Australia/Sydney" {
			t.Errorf("household timezone: got %q, want %q", timezone, "Australia/Sydney")
		}
		if creatorID != created.CreatorID {
			t.Errorf("creator ID: got %q, want %q", creatorID, created.CreatorID)
		}

		var memberCount int
		if err := database.QueryRow(
			`SELECT count(*) FROM member WHERE household_id = ?`, created.ID,
		).Scan(&memberCount); err != nil {
			t.Fatalf("count members: %v", err)
		}
		if memberCount != 1 {
			t.Fatalf("member count: got %d, want 1", memberCount)
		}

		var memberName string
		if err := database.QueryRow(
			`SELECT name FROM member WHERE household_id = ? AND id = ?`, created.ID, created.CreatorID,
		).Scan(&memberName); err != nil {
			t.Fatalf("read creator: %v", err)
		}
		if memberName != "Test Creator" {
			t.Errorf("creator name: got %q, want %q", memberName, "Test Creator")
		}
	})

	t.Run("rejects an unrecognized timezone without writing rows", func(t *testing.T) {
		database := newTestDatabase(t)
		service := household.NewService(database)

		_, err := service.CreateHousehold(context.Background(), "Test Household", "Mars/Olympus_Mons", "Test Creator")
		if !errors.Is(err, household.ErrInvalidInput) {
			t.Fatalf("error: got %v, want ErrInvalidInput", err)
		}
		assertDatabaseEmpty(t, database)
	})

	t.Run("rolls back the household when creator insertion fails", func(t *testing.T) {
		database := newTestDatabase(t)
		if _, err := database.Exec(`
			CREATE TRIGGER reject_member
			BEFORE INSERT ON member
			BEGIN
				SELECT RAISE(ABORT, 'member insert rejected');
			END;
		`); err != nil {
			t.Fatalf("create failure trigger: %v", err)
		}
		service := household.NewService(database)

		_, err := service.CreateHousehold(context.Background(), "Test Household", "Australia/Sydney", "Test Creator")
		if !errors.Is(err, household.ErrInternal) {
			t.Fatalf("error: got %v, want ErrInternal", err)
		}
		if errors.Is(err, household.ErrInvalidInput) {
			t.Fatalf("database failure was classified as invalid input: %v", err)
		}
		assertDatabaseEmpty(t, database)
	})

	for _, test := range []struct {
		name        string
		household   string
		timezone    string
		creatorName string
	}{
		{name: "empty household name", household: "  ", timezone: "Australia/Sydney", creatorName: "Test Creator"},
		{name: "empty timezone", household: "Test Household", timezone: " ", creatorName: "Test Creator"},
		{name: "empty creator name", household: "Test Household", timezone: "Australia/Sydney", creatorName: "\t"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := newTestDatabase(t)
			service := household.NewService(database)

			_, err := service.CreateHousehold(
				context.Background(), test.household, test.timezone, test.creatorName,
			)
			if !errors.Is(err, household.ErrInvalidInput) {
				t.Fatalf("error: got %v, want ErrInvalidInput", err)
			}
			assertDatabaseEmpty(t, database)
		})
	}
}

func TestAddMember(t *testing.T) {
	t.Run("adds a trimmed member to an existing household", func(t *testing.T) {
		database := newTestDatabase(t)
		service := household.NewService(database)
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}

		added, err := service.AddMember(context.Background(), created.ID, " New Member ")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}
		if added.ID == "" {
			t.Error("member ID is empty")
		}
		if added.HouseholdID != created.ID {
			t.Errorf("household ID: got %q, want %q", added.HouseholdID, created.ID)
		}
		if added.Name != "New Member" {
			t.Errorf("member name: got %q, want %q", added.Name, "New Member")
		}

		members, err := service.ListMembers(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("list members: %v", err)
		}
		listed, ok := memberWithID(members, added.ID)
		if !ok {
			t.Fatalf("added member %q not returned by ListMembers: %+v", added.ID, members)
		}
		if listed != added {
			t.Errorf("listed member: got %+v, want %+v", listed, added)
		}
	})

	t.Run("rejects an empty name before touching the database", func(t *testing.T) {
		database := newTestDatabase(t)
		service := household.NewService(database)
		if err := database.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}

		_, err := service.AddMember(context.Background(), "household-id", " \t ")
		if !errors.Is(err, household.ErrInvalidInput) {
			t.Fatalf("error: got %v, want ErrInvalidInput", err)
		}
	})

	t.Run("reports an unknown household", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))

		_, err := service.AddMember(context.Background(), "missing-household", "Member")
		if !errors.Is(err, household.ErrHouseholdNotFound) {
			t.Fatalf("error: got %v, want ErrHouseholdNotFound", err)
		}
		if errors.Is(err, household.ErrInternal) {
			t.Fatalf("unknown household was classified as internal: %v", err)
		}
	})
}

func TestListMembers(t *testing.T) {
	database := newTestDatabase(t)
	service := household.NewService(database)
	first, err := service.CreateHousehold(
		context.Background(), "First Household", "Australia/Sydney", "First Founder",
	)
	if err != nil {
		t.Fatalf("create first household: %v", err)
	}
	second, err := service.CreateHousehold(
		context.Background(), "Second Household", "Australia/Sydney", "Second Founder",
	)
	if err != nil {
		t.Fatalf("create second household: %v", err)
	}
	firstMember, err := service.AddMember(context.Background(), first.ID, "First Member")
	if err != nil {
		t.Fatalf("add first member: %v", err)
	}
	secondFirstMember, err := service.AddMember(context.Background(), first.ID, "Second First Member")
	if err != nil {
		t.Fatalf("add second member to first household: %v", err)
	}
	otherMember, err := service.AddMember(context.Background(), second.ID, "Other Member")
	if err != nil {
		t.Fatalf("add member to second household: %v", err)
	}

	members, err := service.ListMembers(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	want := map[string]string{
		first.CreatorID:      "First Founder",
		firstMember.ID:       "First Member",
		secondFirstMember.ID: "Second First Member",
	}
	if len(members) != len(want) {
		t.Fatalf("member count: got %d (%+v), want %d", len(members), members, len(want))
	}
	for _, member := range members {
		if member.HouseholdID != first.ID {
			t.Errorf("member household ID: got %q, want %q", member.HouseholdID, first.ID)
		}
		name, ok := want[member.ID]
		if !ok {
			t.Errorf("unexpected member: %+v", member)
			continue
		}
		if member.Name != name {
			t.Errorf("member %q name: got %q, want %q", member.ID, member.Name, name)
		}
	}
	if _, ok := memberWithID(members, second.CreatorID); ok {
		t.Errorf("founder from another household was listed: %q", second.CreatorID)
	}
	if _, ok := memberWithID(members, otherMember.ID); ok {
		t.Errorf("member from another household was listed: %q", otherMember.ID)
	}

	missing, err := service.ListMembers(context.Background(), "missing-household")
	if err != nil {
		t.Fatalf("list unknown household: %v", err)
	}
	if missing == nil || len(missing) != 0 {
		t.Errorf("unknown household members: got %#v, want non-nil empty slice", missing)
	}
}

func TestRemoveMember(t *testing.T) {
	t.Run("removes a member and reports a repeated removal", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		added, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}

		if err := service.RemoveMember(context.Background(), created.ID, added.ID); err != nil {
			t.Fatalf("remove member: %v", err)
		}
		members, err := service.ListMembers(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("list members: %v", err)
		}
		if _, ok := memberWithID(members, added.ID); ok {
			t.Errorf("removed member is still listed: %+v", members)
		}

		err = service.RemoveMember(context.Background(), created.ID, added.ID)
		if !errors.Is(err, household.ErrMemberNotFound) {
			t.Fatalf("second removal error: got %v, want ErrMemberNotFound", err)
		}
	})

	t.Run("rejects removal of the founding member without deleting them", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}

		err = service.RemoveMember(context.Background(), created.ID, created.CreatorID)
		if !errors.Is(err, household.ErrFoundingMember) {
			t.Fatalf("error: got %v, want ErrFoundingMember", err)
		}
		if errors.Is(err, household.ErrInternal) {
			t.Fatalf("founding member removal was classified as internal: %v", err)
		}
		members, err := service.ListMembers(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("list members: %v", err)
		}
		if _, ok := memberWithID(members, created.CreatorID); !ok {
			t.Fatalf("founding member %q was removed: %+v", created.CreatorID, members)
		}
	})
}

func TestAddPreference(t *testing.T) {
	t.Run("adds a preference to a member", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		added, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}
		preference, err := service.AddPreference(context.Background(), created.ID, added.ID, sql.NullInt16{}, "vegan", household.DietaryRestriction, household.Hard)

		// make sure returned preference is valid and listing preferences returns int
		if err != nil {
			t.Fatalf("add preference: %v", err)
		}
		if preference.ID == "" {
			t.Error("preference ID is empty")
		}
		if preference.HouseholdID != created.ID {
			t.Errorf("household ID: got %q, want %q", preference.HouseholdID, created.ID)
		}
		if preference.MemberID != added.ID {
			t.Errorf("member ID: got %q, want %q", preference.MemberID, added.ID)
		}
		if preference.Value != "vegan" {
			t.Errorf("preference value: got %q, want %q", preference.Value, "vegan")
		}
		if preference.Category != household.DietaryRestriction {
			t.Errorf("preference type: got %q, want %q", preference.Category, household.DietaryRestriction)
		}
		if preference.Kind != household.Hard {
			t.Errorf("preference severity: got %q, want %q", preference.Kind, household.Hard)
		}
		if preference.Strength.Valid {
			t.Errorf("preference strength: got %v, want null", preference.Strength)
		}

		// list preferences and make sure the added preference is returned
		preferences, err := service.ListPreferences(context.Background(), created.ID, added.ID, household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 1 {
			t.Fatalf("preference count: got %d, want 1", len(preferences))
		}
		listed := preferences[0]
		if listed != preference {
			t.Errorf("listed preference: got %+v, want %+v", listed, preference)
		}

	})

	t.Run("adds a household-wide preference without a member", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		added, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}

		preference, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		if err != nil {
			t.Fatalf("add preference: %v", err)
		}
		if preference.MemberID != "" {
			t.Errorf("member ID: got %q, want empty", preference.MemberID)
		}

		// A household-wide preference is visible to every member, not just the one who happens
		// to be passed in when listing.
		preferences, err := service.ListPreferences(context.Background(), created.ID, added.ID, household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 1 || preferences[0].ID != preference.ID {
			t.Fatalf("preferences for member: got %+v, want only %+v", preferences, preference)
		}
	})

	t.Run("rejects invalid input without creating a preference", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		added, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}

		for _, test := range []struct {
			name     string
			strength sql.NullInt16
			value    string
			kind     household.PreferenceKind
		}{
			{name: "empty value", strength: sql.NullInt16{}, value: "  ", kind: household.Hard},
			{name: "hard preference with a strength", strength: sql.NullInt16{Int16: 3, Valid: true}, value: "peanuts", kind: household.Hard},
			{name: "soft preference without a strength", strength: sql.NullInt16{}, value: "spicy food", kind: household.Soft},
			{name: "soft preference with an out-of-range strength", strength: sql.NullInt16{Int16: 6, Valid: true}, value: "spicy food", kind: household.Soft},
		} {
			t.Run(test.name, func(t *testing.T) {
				_, err := service.AddPreference(context.Background(), created.ID, added.ID, test.strength, test.value, household.Dislike, test.kind)
				if !errors.Is(err, household.ErrInvalidInput) {
					t.Fatalf("error: got %v, want ErrInvalidInput", err)
				}
			})
		}

		preferences, err := service.ListPreferences(context.Background(), created.ID, added.ID, household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 0 {
			t.Errorf("preferences: got %+v, want none", preferences)
		}
	})

	t.Run("rejects an unknown member", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}

		_, err = service.AddPreference(context.Background(), created.ID, "missing-member", sql.NullInt16{}, "vegan", household.DietaryRestriction, household.Hard)
		if !errors.Is(err, household.ErrMemberNotFound) {
			t.Fatalf("error: got %v, want ErrMemberNotFound", err)
		}
	})

	t.Run("rejects an unknown household", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))

		_, err := service.AddPreference(context.Background(), "missing-household", "", sql.NullInt16{}, "vegan", household.DietaryRestriction, household.Hard)
		if !errors.Is(err, household.ErrHouseholdNotFound) {
			t.Fatalf("error: got %v, want ErrHouseholdNotFound", err)
		}
	})
}

func TestListPreferences(t *testing.T) {
	t.Run("lists a member's own and the household's preferences together", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		member, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}
		otherMember, err := service.AddMember(context.Background(), created.ID, "Other Member")
		if err != nil {
			t.Fatalf("add other member: %v", err)
		}

		householdWide, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		if err != nil {
			t.Fatalf("add household preference: %v", err)
		}
		own, err := service.AddPreference(context.Background(), created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft)
		if err != nil {
			t.Fatalf("add member preference: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, otherMember.ID, sql.NullInt16{Int16: 2, Valid: true}, "Thai", household.Cuisine, household.Soft); err != nil {
			t.Fatalf("add other member preference: %v", err)
		}

		preferences, err := service.ListPreferences(context.Background(), created.ID, member.ID, household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		gotIDs := map[string]bool{}
		for _, preference := range preferences {
			gotIDs[preference.ID] = true
		}
		if len(preferences) != 2 || !gotIDs[householdWide.ID] || !gotIDs[own.ID] {
			t.Fatalf("preferences for member: got %+v, want exactly %+v and %+v", preferences, householdWide, own)
		}
	})

	t.Run("lists an empty slice for a member with no preferences", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		member, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}

		preferences, err := service.ListPreferences(context.Background(), created.ID, member.ID, household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if preferences == nil || len(preferences) != 0 {
			t.Errorf("preferences: got %#v, want non-nil empty slice", preferences)
		}
	})

	t.Run("returns an empty slice for an unknown member", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard); err != nil {
			t.Fatalf("add household preference: %v", err)
		}

		// An unknown member still sees household-wide preferences, matching AddPreference's
		// treatment of an empty member as "household-wide" rather than a member match failure.
		preferences, err := service.ListPreferences(context.Background(), created.ID, "missing-member", household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 1 {
			t.Errorf("preferences for unknown member: got %+v, want the household-wide preference only", preferences)
		}
	})

	t.Run("returns an empty slice for an unknown household", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))

		preferences, err := service.ListPreferences(context.Background(), "missing-household", "missing-member", household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if preferences == nil || len(preferences) != 0 {
			t.Errorf("preferences: got %#v, want non-nil empty slice", preferences)
		}
	})

	t.Run("filters by category", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		member, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}
		allergy, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		if err != nil {
			t.Fatalf("add allergy preference: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft); err != nil {
			t.Fatalf("add cuisine preference: %v", err)
		}

		preferences, err := service.ListPreferences(context.Background(), created.ID, member.ID, household.PreferenceFilter{Category: household.Allergy})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 1 || preferences[0].ID != allergy.ID {
			t.Fatalf("preferences filtered by category: got %+v, want only %+v", preferences, allergy)
		}
	})

	t.Run("filters by kind", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		member, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard); err != nil {
			t.Fatalf("add allergy preference: %v", err)
		}
		cuisine, err := service.AddPreference(context.Background(), created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft)
		if err != nil {
			t.Fatalf("add cuisine preference: %v", err)
		}

		preferences, err := service.ListPreferences(context.Background(), created.ID, member.ID, household.PreferenceFilter{Kind: household.Soft})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 1 || preferences[0].ID != cuisine.ID {
			t.Fatalf("preferences filtered by kind: got %+v, want only %+v", preferences, cuisine)
		}
	})

	t.Run("combines category and kind filters", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		member, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, member.ID, sql.NullInt16{Int16: 3, Valid: true}, "Peanut oil", household.Cuisine, household.Soft); err != nil {
			t.Fatalf("add soft cuisine preference: %v", err)
		}
		hardCuisine, err := service.AddPreference(context.Background(), created.ID, member.ID, sql.NullInt16{}, "no shellfish", household.Cuisine, household.Hard)
		if err != nil {
			t.Fatalf("add hard cuisine preference: %v", err)
		}

		preferences, err := service.ListPreferences(context.Background(), created.ID, member.ID, household.PreferenceFilter{Category: household.Cuisine, Kind: household.Hard})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 1 || preferences[0].ID != hardCuisine.ID {
			t.Fatalf("preferences filtered by category and kind: got %+v, want only %+v", preferences, hardCuisine)
		}
	})

	t.Run("lists only household-wide preferences when no member is given", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		member, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}
		householdWide, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		if err != nil {
			t.Fatalf("add household preference: %v", err)
		}
		if _, err := service.AddPreference(context.Background(), created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft); err != nil {
			t.Fatalf("add member preference: %v", err)
		}

		preferences, err := service.ListPreferences(context.Background(), created.ID, "", household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 1 || preferences[0].ID != householdWide.ID {
			t.Fatalf("household-wide preferences: got %+v, want only %+v", preferences, householdWide)
		}
	})
}

func TestRemovePreference(t *testing.T) {
	t.Run("removes a preference from a member", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		member, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}
		preference, err := service.AddPreference(context.Background(), created.ID, member.ID, sql.NullInt16{Int16: 4, Valid: true}, "Italian", household.Cuisine, household.Soft)
		if err != nil {
			t.Fatalf("add preference: %v", err)
		}

		if err := service.RemovePreference(context.Background(), created.ID, member.ID, preference.ID); err != nil {
			t.Fatalf("remove preference: %v", err)
		}
		preferences, err := service.ListPreferences(context.Background(), created.ID, member.ID, household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 0 {
			t.Errorf("preferences after removal: got %+v, want none", preferences)
		}
	})

	t.Run("removes a preference from a household", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		preference, err := service.AddPreference(context.Background(), created.ID, "", sql.NullInt16{}, "peanuts", household.Allergy, household.Hard)
		if err != nil {
			t.Fatalf("add preference: %v", err)
		}

		if err := service.RemovePreference(context.Background(), created.ID, "", preference.ID); err != nil {
			t.Fatalf("remove preference: %v", err)
		}
		preferences, err := service.ListPreferences(context.Background(), created.ID, "", household.PreferenceFilter{})
		if err != nil {
			t.Fatalf("list preferences: %v", err)
		}
		if len(preferences) != 0 {
			t.Errorf("preferences after removal: got %+v, want none", preferences)
		}
	})

	t.Run("rejects an unknown preference", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}
		member, err := service.AddMember(context.Background(), created.ID, "Member")
		if err != nil {
			t.Fatalf("add member: %v", err)
		}

		err = service.RemovePreference(context.Background(), created.ID, member.ID, "missing-preference")
		if !errors.Is(err, household.ErrPreferenceNotFound) {
			t.Fatalf("error: got %v, want ErrPreferenceNotFound", err)
		}
	})

	t.Run("rejects an unknown member", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, err := service.CreateHousehold(
			context.Background(), "Household", "Australia/Sydney", "Founder",
		)
		if err != nil {
			t.Fatalf("create household: %v", err)
		}

		err = service.RemovePreference(context.Background(), created.ID, "missing-member", "missing-preference")
		if !errors.Is(err, household.ErrMemberNotFound) {
			t.Fatalf("error: got %v, want ErrMemberNotFound", err)
		}
	})

	t.Run("rejects an unknown household", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))

		err := service.RemovePreference(context.Background(), "missing-household", "", "missing-preference")
		if !errors.Is(err, household.ErrHouseholdNotFound) {
			t.Fatalf("error: got %v, want ErrHouseholdNotFound", err)
		}
	})
}

func memberWithID(members []household.Member, id string) (household.Member, bool) {
	for _, member := range members {
		if member.ID == id {
			return member, true
		}
	}
	return household.Member{}, false
}

func newTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := mealdb.ApplySchema(context.Background(), database); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	return database
}

func assertDatabaseEmpty(t *testing.T, database *sql.DB) {
	t.Helper()

	for _, table := range []string{"household", "member"} {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s rows: got %d, want 0", table, count)
		}
	}
}
