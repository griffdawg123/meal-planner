package household_test

import (
	"context"
	"errors"
	"testing"

	"github.com/griffdawg123/meal-planner/household"
)

func TestAuthorizeMember(t *testing.T) {
	service := household.NewService(newTestDatabase(t))
	first, err := service.CreateHousehold(context.Background(), "First Household", "Australia/Sydney", "First Founder")
	if err != nil {
		t.Fatalf("create first household: %v", err)
	}
	member, err := service.AddMember(context.Background(), first.ID, "Member")
	if err != nil {
		t.Fatalf("add member to first household: %v", err)
	}
	second, err := service.CreateHousehold(context.Background(), "Second Household", "Australia/Sydney", "Second Founder")
	if err != nil {
		t.Fatalf("create second household: %v", err)
	}

	t.Run("allows acting on any member of the actor's own household", func(t *testing.T) {
		for _, memberID := range []string{first.CreatorID, member.ID} {
			if err := service.AuthorizeMember(context.Background(), first.ID, memberID); err != nil {
				t.Errorf("authorize member %q: got %v, want nil", memberID, err)
			}
		}
	})

	t.Run("denies acting on a member of another household", func(t *testing.T) {
		err := service.AuthorizeMember(context.Background(), first.ID, second.CreatorID)
		if !errors.Is(err, household.ErrPermissionDenied) {
			t.Fatalf("authorize member: got %v, want it to wrap %v", err, household.ErrPermissionDenied)
		}
	})

	t.Run("reports a member that does not exist anywhere as not found rather than denied", func(t *testing.T) {
		err := service.AuthorizeMember(context.Background(), first.ID, "no-such-member")
		if !errors.Is(err, household.ErrMemberNotFound) {
			t.Fatalf("authorize member: got %v, want it to wrap %v", err, household.ErrMemberNotFound)
		}
		if errors.Is(err, household.ErrPermissionDenied) {
			t.Fatalf("authorize member: got %v, want it not to wrap %v", err, household.ErrPermissionDenied)
		}
	})
}
