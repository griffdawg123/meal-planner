package household_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/household"
)

func TestRecordAwayNights(t *testing.T) {
	t.Run("records the nights a member is away ahead of planning", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)

		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06", "2026-10-08"); err != nil {
			t.Fatalf("record away nights: %v", err)
		}

		got := listAwayNights(t, service, created.ID, "2026-10-05", "2026-10-11")
		want := []household.AwayNight{
			{HouseholdID: created.ID, MemberID: member.ID, Night: "2026-10-06"},
			{HouseholdID: created.ID, MemberID: member.ID, Night: "2026-10-08"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("away nights: got %+v, want %+v", got, want)
		}
	})

	t.Run("treats recording an already-away night as a no-op", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("record first away night: %v", err)
		}

		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06", "2026-10-07", "2026-10-07"); err != nil {
			t.Fatalf("record overlapping away nights: %v", err)
		}

		got := listAwayNights(t, service, created.ID, "2026-10-05", "2026-10-11")
		want := []household.AwayNight{
			{HouseholdID: created.ID, MemberID: member.ID, Night: "2026-10-06"},
			{HouseholdID: created.ID, MemberID: member.ID, Night: "2026-10-07"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("away nights: got %+v, want %+v", got, want)
		}
	})

	t.Run("rejects invalid nights without recording any of them", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)

		for _, test := range []struct {
			name   string
			nights []string
		}{
			{name: "no nights", nights: nil},
			{name: "empty night", nights: []string{""}},
			{name: "impossible date", nights: []string{"2026-10-06", "2026-02-30"}},
			{name: "date with a time", nights: []string{"2026-10-06T18:00:00Z"}},
			{name: "non-ISO format", nights: []string{"06/10/2026"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				err := service.RecordAwayNights(context.Background(), created.ID, member.ID, test.nights...)
				if !errors.Is(err, household.ErrInvalidInput) {
					t.Fatalf("error: got %v, want ErrInvalidInput", err)
				}
			})
		}

		if got := listAwayNights(t, service, created.ID, "2026-01-01", "2026-12-31"); len(got) != 0 {
			t.Errorf("away nights: got %+v, want none", got)
		}
	})

	t.Run("rejects an unknown member", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, _ := newHouseholdWithMember(t, service)

		err := service.RecordAwayNights(context.Background(), created.ID, "missing-member", "2026-10-06")
		if !errors.Is(err, household.ErrMemberNotFound) {
			t.Fatalf("error: got %v, want ErrMemberNotFound", err)
		}
	})

	t.Run("rejects an unknown household", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))

		err := service.RecordAwayNights(context.Background(), "missing-household", "missing-member", "2026-10-06")
		if !errors.Is(err, household.ErrHouseholdNotFound) {
			t.Fatalf("error: got %v, want ErrHouseholdNotFound", err)
		}
	})
}

func TestClearAwayNights(t *testing.T) {
	t.Run("marks a member present again after the plan is drafted", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06", "2026-10-07"); err != nil {
			t.Fatalf("record away nights: %v", err)
		}

		if err := service.ClearAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("clear away night: %v", err)
		}

		got := listAwayNights(t, service, created.ID, "2026-10-05", "2026-10-11")
		want := []household.AwayNight{
			{HouseholdID: created.ID, MemberID: member.ID, Night: "2026-10-07"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("away nights: got %+v, want %+v", got, want)
		}
	})

	t.Run("allows attendance to change repeatedly mid-week", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)

		steps := []struct {
			away   bool
			nights []string
			want   []string
		}{
			{away: true, nights: []string{"2026-10-06"}, want: []string{"2026-10-06"}},
			{away: false, nights: []string{"2026-10-06"}, want: nil},
			{away: true, nights: []string{"2026-10-06", "2026-10-09"}, want: []string{"2026-10-06", "2026-10-09"}},
			{away: false, nights: []string{"2026-10-09"}, want: []string{"2026-10-06"}},
		}
		for i, step := range steps {
			var err error
			if step.away {
				err = service.RecordAwayNights(context.Background(), created.ID, member.ID, step.nights...)
			} else {
				err = service.ClearAwayNights(context.Background(), created.ID, member.ID, step.nights...)
			}
			if err != nil {
				t.Fatalf("step %d: update attendance: %v", i, err)
			}
			var got []string
			for _, away := range listAwayNights(t, service, created.ID, "2026-10-05", "2026-10-11") {
				got = append(got, away.Night)
			}
			if !reflect.DeepEqual(got, step.want) {
				t.Fatalf("step %d: away nights: got %q, want %q", i, got, step.want)
			}
		}
	})

	t.Run("treats clearing a night the member was not away as a no-op", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)

		if err := service.ClearAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("clear away night: %v", err)
		}
	})

	t.Run("leaves other members' away nights untouched", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("record member away night: %v", err)
		}
		if err := service.RecordAwayNights(context.Background(), created.ID, created.CreatorID, "2026-10-06"); err != nil {
			t.Fatalf("record founder away night: %v", err)
		}

		if err := service.ClearAwayNights(context.Background(), created.ID, member.ID, "2026-10-06"); err != nil {
			t.Fatalf("clear away night: %v", err)
		}

		got := listAwayNights(t, service, created.ID, "2026-10-05", "2026-10-11")
		want := []household.AwayNight{
			{HouseholdID: created.ID, MemberID: created.CreatorID, Night: "2026-10-06"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("away nights: got %+v, want %+v", got, want)
		}
	})

	t.Run("rejects an invalid night", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)

		err := service.ClearAwayNights(context.Background(), created.ID, member.ID, "2026-13-01")
		if !errors.Is(err, household.ErrInvalidInput) {
			t.Fatalf("error: got %v, want ErrInvalidInput", err)
		}
	})

	t.Run("rejects an unknown member", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, _ := newHouseholdWithMember(t, service)

		err := service.ClearAwayNights(context.Background(), created.ID, "missing-member", "2026-10-06")
		if !errors.Is(err, household.ErrMemberNotFound) {
			t.Fatalf("error: got %v, want ErrMemberNotFound", err)
		}
	})
}

func TestListAwayNights(t *testing.T) {
	t.Run("lists away nights within an inclusive range ordered by night", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		created, member := newHouseholdWithMember(t, service)
		if err := service.RecordAwayNights(context.Background(), created.ID, member.ID, "2026-10-04", "2026-10-11", "2026-10-05", "2026-10-12"); err != nil {
			t.Fatalf("record member away nights: %v", err)
		}
		if err := service.RecordAwayNights(context.Background(), created.ID, created.CreatorID, "2026-10-08"); err != nil {
			t.Fatalf("record founder away night: %v", err)
		}

		got := listAwayNights(t, service, created.ID, "2026-10-05", "2026-10-11")
		want := []household.AwayNight{
			{HouseholdID: created.ID, MemberID: member.ID, Night: "2026-10-05"},
			{HouseholdID: created.ID, MemberID: created.CreatorID, Night: "2026-10-08"},
			{HouseholdID: created.ID, MemberID: member.ID, Night: "2026-10-11"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("away nights: got %+v, want %+v", got, want)
		}
	})

	t.Run("returns an empty slice for an unknown household", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))

		got := listAwayNights(t, service, "missing-household", "2026-10-05", "2026-10-11")
		if got == nil || len(got) != 0 {
			t.Errorf("away nights: got %#v, want non-nil empty slice", got)
		}
	})

	t.Run("excludes another household's away nights", func(t *testing.T) {
		service := household.NewService(newTestDatabase(t))
		first, _ := newHouseholdWithMember(t, service)
		second, otherMember := newHouseholdWithMember(t, service)
		if err := service.RecordAwayNights(context.Background(), second.ID, otherMember.ID, "2026-10-06"); err != nil {
			t.Fatalf("record other household away night: %v", err)
		}

		if got := listAwayNights(t, service, first.ID, "2026-10-05", "2026-10-11"); len(got) != 0 {
			t.Errorf("away nights: got %+v, want none", got)
		}
		err := service.RecordAwayNights(context.Background(), first.ID, otherMember.ID, "2026-10-07")
		if !errors.Is(err, household.ErrMemberNotFound) {
			t.Fatalf("cross-household record error: got %v, want ErrMemberNotFound", err)
		}
		err = service.ClearAwayNights(context.Background(), first.ID, otherMember.ID, "2026-10-06")
		if !errors.Is(err, household.ErrMemberNotFound) {
			t.Fatalf("cross-household clear error: got %v, want ErrMemberNotFound", err)
		}
		if got := listAwayNights(t, service, second.ID, "2026-10-05", "2026-10-11"); len(got) != 1 {
			t.Errorf("other household away nights after write attempts: got %+v, want one", got)
		}
	})

	for _, test := range []struct {
		name string
		from string
		to   string
	}{
		{name: "rejects an invalid start night", from: "2026-10-32", to: "2026-10-11"},
		{name: "rejects an invalid end night", from: "2026-10-05", to: ""},
		{name: "rejects a range that ends before it starts", from: "2026-10-11", to: "2026-10-05"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := household.NewService(newTestDatabase(t))

			_, err := service.ListAwayNights(context.Background(), "household-id", test.from, test.to)
			if !errors.Is(err, household.ErrInvalidInput) {
				t.Fatalf("error: got %v, want ErrInvalidInput", err)
			}
		})
	}
}

func newHouseholdWithMember(t *testing.T, service *household.Service) (household.Household, household.Member) {
	t.Helper()

	created, err := service.CreateHousehold(context.Background(), "Household", "Australia/Sydney", "Founder")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	member, err := service.AddMember(context.Background(), created.ID, "Member")
	if err != nil {
		t.Fatalf("add member: %v", err)
	}
	return created, member
}

func listAwayNights(t *testing.T, service *household.Service, householdID, from, to string) []household.AwayNight {
	t.Helper()

	awayNights, err := service.ListAwayNights(context.Background(), householdID, from, to)
	if err != nil {
		t.Fatalf("list away nights: %v", err)
	}
	return awayNights
}
