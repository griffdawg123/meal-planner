package telegram_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/griffdawg123/meal-planner/auth"
	"github.com/griffdawg123/meal-planner/household"
	"github.com/griffdawg123/meal-planner/telegram"
)

// The production services are the command gate's identity, household, and draft plan sources.
var (
	_ telegram.Identities = (*auth.Service)(nil)
	_ telegram.Households = (*household.Service)(nil)
	_ telegram.DraftPlans = (*household.Planner)(nil)
)

func TestCommandsMarkAway(t *testing.T) {
	identities := fakeIdentities{1001: {HouseholdID: "household-1", MemberID: "member-1"}}

	t.Run("records the away nights of a member of the user's household and confirms", func(t *testing.T) {
		households := &recordingHouseholds{members: map[string]string{"member-2": "household-1"}}
		sender := &recordingSender{}
		commands := telegram.NewCommands(identities, households, &recordingDraftPlans{}, sender)

		if err := commands.MarkAway(context.Background(), 1001, "member-2", "2026-10-05"); err != nil {
			t.Fatalf("mark away: got %v, want nil", err)
		}

		want := []awayCall{{HouseholdID: "household-1", MemberID: "member-2", Nights: []string{"2026-10-05"}}}
		if !reflect.DeepEqual(households.recorded, want) {
			t.Errorf("recorded away nights: got %+v, want %+v", households.recorded, want)
		}
		wantSent := []sentMessage{{ChatID: 1001, Text: "Got it, I've noted who's away.", ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, wantSent) {
			t.Errorf("sent messages: got %+v, want %+v", sender.sent, wantSent)
		}
	})

	t.Run("never records away nights for a member of another household and tells the user why", func(t *testing.T) {
		households := &recordingHouseholds{members: map[string]string{"member-9": "household-9"}}
		sender := &recordingSender{}
		commands := telegram.NewCommands(identities, households, &recordingDraftPlans{}, sender)

		err := commands.MarkAway(context.Background(), 1001, "member-9", "2026-10-05")
		if !errors.Is(err, household.ErrPermissionDenied) {
			t.Errorf("mark away: got %v, want it to wrap %v", err, household.ErrPermissionDenied)
		}
		if len(households.recorded) != 0 {
			t.Errorf("recorded away nights: got %+v, want none", households.recorded)
		}
		assertRepliedWith(t, sender, 1001, household.ErrPermissionDenied)
	})

	t.Run("never records anything for an unlinked Telegram user and tells them to link", func(t *testing.T) {
		households := &recordingHouseholds{members: map[string]string{"member-1": "household-1"}}
		sender := &recordingSender{}
		commands := telegram.NewCommands(identities, households, &recordingDraftPlans{}, sender)

		err := commands.MarkAway(context.Background(), 4040, "member-1", "2026-10-05")
		if !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("mark away: got %v, want it to wrap %v", err, auth.ErrTelegramNotLinked)
		}
		if len(households.recorded) != 0 {
			t.Errorf("recorded away nights: got %+v, want none", households.recorded)
		}
		assertRepliedWith(t, sender, 4040, auth.ErrTelegramNotLinked)
	})

	t.Run("reports a failure to send the reply alongside the blocked change", func(t *testing.T) {
		households := &recordingHouseholds{members: map[string]string{"member-9": "household-9"}}
		sendErr := errors.New("bot was blocked by the user")
		sender := &recordingSender{failFor: map[int64]error{1001: sendErr}}
		commands := telegram.NewCommands(identities, households, &recordingDraftPlans{}, sender)

		err := commands.MarkAway(context.Background(), 1001, "member-9", "2026-10-05")
		if !errors.Is(err, household.ErrPermissionDenied) || !errors.Is(err, sendErr) {
			t.Errorf("mark away: got %v, want it to wrap %v and %v", err, household.ErrPermissionDenied, sendErr)
		}
	})
}

func TestCommandsReplaceDinner(t *testing.T) {
	identities := fakeIdentities{1001: {HouseholdID: "household-1", MemberID: "member-1"}}
	satay := household.Meal{Title: "Satay chicken", Conflicts: []string{"peanuts"}}
	padThai := household.Meal{Title: "Pad thai"}

	t.Run("revises the dinner in the user's own household", func(t *testing.T) {
		drafts := &recordingDraftPlans{}
		sender := &recordingSender{}
		commands := telegram.NewCommands(identities, &recordingHouseholds{}, drafts, sender)

		if err := commands.ReplaceDinner(context.Background(), 1001, "2026-10-05", padThai); err != nil {
			t.Fatalf("replace dinner: got %v, want nil", err)
		}

		want := []dinnerCall{{HouseholdID: "household-1", Night: "2026-10-05", Meal: padThai}}
		if !reflect.DeepEqual(drafts.revised, want) {
			t.Errorf("revised dinners: got %+v, want %+v", drafts.revised, want)
		}
		wantSent := []sentMessage{{ChatID: 1001, Text: "Done, I've updated the plan and sent it to everyone.", ParseMode: telegram.ParseMode}}
		if !reflect.DeepEqual(sender.sent, wantSent) {
			t.Errorf("sent messages: got %+v, want %+v", sender.sent, wantSent)
		}
	})

	t.Run("tells the user a dinner breaking an allergy was blocked", func(t *testing.T) {
		drafts := &recordingDraftPlans{blocked: map[string]bool{"Satay chicken": true}}
		sender := &recordingSender{}
		commands := telegram.NewCommands(identities, &recordingHouseholds{}, drafts, sender)

		err := commands.ReplaceDinner(context.Background(), 1001, "2026-10-05", satay)
		if !errors.Is(err, household.ErrHardConstraint) {
			t.Errorf("replace dinner: got %v, want it to wrap %v", err, household.ErrHardConstraint)
		}
		if len(drafts.revised) != 0 {
			t.Errorf("revised dinners: got %+v, want none", drafts.revised)
		}
		assertRepliedWith(t, sender, 1001, household.ErrHardConstraint)
	})

	t.Run("never revises a dinner for an unlinked Telegram user", func(t *testing.T) {
		drafts := &recordingDraftPlans{}
		sender := &recordingSender{}
		commands := telegram.NewCommands(identities, &recordingHouseholds{}, drafts, sender)

		err := commands.ReplaceDinner(context.Background(), 4040, "2026-10-05", padThai)
		if !errors.Is(err, auth.ErrTelegramNotLinked) {
			t.Errorf("replace dinner: got %v, want it to wrap %v", err, auth.ErrTelegramNotLinked)
		}
		if len(drafts.revised) != 0 {
			t.Errorf("revised dinners: got %+v, want none", drafts.revised)
		}
		assertRepliedWith(t, sender, 4040, auth.ErrTelegramNotLinked)
	})
}

// assertRepliedWith fails unless the only message sent was the plain-language reply for want,
// addressed to chatID.
func assertRepliedWith(t *testing.T, sender *recordingSender, chatID int64, want error) {
	t.Helper()
	wantSent := []sentMessage{{ChatID: chatID, Text: telegram.ErrorReply(want), ParseMode: telegram.ParseMode}}
	if !reflect.DeepEqual(sender.sent, wantSent) {
		t.Errorf("sent messages: got %+v, want %+v", sender.sent, wantSent)
	}
}

// fakeIdentities maps linked Telegram users to the member they act as.
type fakeIdentities map[int64]auth.Principal

func (f fakeIdentities) ResolveTelegram(_ context.Context, telegramUserID int64) (auth.Principal, error) {
	principal, ok := f[telegramUserID]
	if !ok {
		return auth.Principal{}, auth.ErrTelegramNotLinked
	}
	return principal, nil
}

type awayCall struct {
	HouseholdID string
	MemberID    string
	Nights      []string
}

// recordingHouseholds authorizes members by the household each belongs to and records every away
// night change that reaches it.
type recordingHouseholds struct {
	members  map[string]string
	recorded []awayCall
}

func (h *recordingHouseholds) AuthorizeMember(_ context.Context, householdID, memberID string) error {
	owner, ok := h.members[memberID]
	if !ok {
		return fmt.Errorf("%w: %q", household.ErrMemberNotFound, memberID)
	}
	if owner != householdID {
		return fmt.Errorf("%w: member %q", household.ErrPermissionDenied, memberID)
	}
	return nil
}

func (h *recordingHouseholds) RecordAwayNights(_ context.Context, householdID, memberID string, nights ...string) error {
	h.recorded = append(h.recorded, awayCall{HouseholdID: householdID, MemberID: memberID, Nights: nights})
	return nil
}

type dinnerCall struct {
	HouseholdID string
	Night       string
	Meal        household.Meal
}

// recordingDraftPlans rejects meals whose titles are blocked, as a hard constraint would, and
// records every revision it accepts.
type recordingDraftPlans struct {
	blocked map[string]bool
	revised []dinnerCall
}

func (d *recordingDraftPlans) ReviseDinner(_ context.Context, householdID, night string, meal household.Meal) error {
	if d.blocked[meal.Title] {
		return fmt.Errorf("%w: %q on %s", household.ErrHardConstraint, meal.Title, night)
	}
	d.revised = append(d.revised, dinnerCall{HouseholdID: householdID, Night: night, Meal: meal})
	return nil
}
