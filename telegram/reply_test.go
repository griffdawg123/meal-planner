package telegram_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/griffdawg123/meal-planner/auth"
	"github.com/griffdawg123/meal-planner/household"
	"github.com/griffdawg123/meal-planner/telegram"
)

func TestErrorReply(t *testing.T) {
	t.Run("explains a permission denial without revealing the error's internal detail", func(t *testing.T) {
		err := fmt.Errorf("remove member %q from household %q: %w", "member-2", "household-9", household.ErrPermissionDenied)

		got := telegram.ErrorReply(err)

		want := "Sorry, you don't have permission to do that, so nothing was changed."
		if got != want {
			t.Fatalf("reply: got %q, want %q", got, want)
		}
	})

	t.Run("explains that a hard constraint blocked the change", func(t *testing.T) {
		err := fmt.Errorf("%w: meal %q contains allergen %q", household.ErrHardConstraint, "Satay", "peanut")

		got := telegram.ErrorReply(err)

		want := "I can't do that because it would break a strict requirement, such as someone's allergy or diet, so nothing was changed."
		if got != want {
			t.Fatalf("reply: got %q, want %q", got, want)
		}
	})

	t.Run("prefers the permission denial when an error wraps both a denial and another failure", func(t *testing.T) {
		err := errors.Join(household.ErrMemberNotFound, household.ErrPermissionDenied)

		got := telegram.ErrorReply(err)

		want := telegram.ErrorReply(household.ErrPermissionDenied)
		if got != want {
			t.Fatalf("reply: got %q, want %q", got, want)
		}
	})

	t.Run("explains each known failure in plain language", func(t *testing.T) {
		cases := []struct {
			err  error
			want string
		}{
			{household.ErrNoSuitableMeal, "I couldn't find a meal for every night that meets everyone's strict requirements, such as allergies and diets. Try adding more meal ideas or relaxing a requirement."},
			{auth.ErrTelegramNotLinked, "Your Telegram account isn't linked to a household member yet. Get a link code from the web app and send it to me first."},
			{household.ErrFoundingMember, "The person who set up the household can't be removed."},
			{household.ErrMemberNotFound, "I couldn't find that person in your household."},
			{auth.ErrMemberNotFound, "I couldn't find that person in your household."},
			{household.ErrPreferenceNotFound, "I couldn't find that preference in your household."},
			{household.ErrHouseholdNotFound, "I couldn't find your household."},
			{household.ErrInvalidInput, "Some details were missing or not valid, so nothing was changed."},
			{auth.ErrInvalidInput, "Some details were missing or not valid, so nothing was changed."},
		}
		for _, c := range cases {
			got := telegram.ErrorReply(fmt.Errorf("handle message: %w", c.err))
			if got != c.want {
				t.Errorf("reply for %v: got %q, want %q", c.err, got, c.want)
			}
		}
	})

	t.Run("gives a generic apology for an internal or unrecognized error", func(t *testing.T) {
		want := "Sorry, something went wrong on my end. Please try again later."
		for _, err := range []error{
			fmt.Errorf("%w: insert member: database is locked", household.ErrInternal),
			errors.New("connection reset by peer"),
		} {
			got := telegram.ErrorReply(err)
			if got != want {
				t.Errorf("reply for %v: got %q, want %q", err, got, want)
			}
		}
	})

	t.Run("returns no reply for a nil error", func(t *testing.T) {
		if got := telegram.ErrorReply(nil); got != "" {
			t.Fatalf("reply: got %q, want empty", got)
		}
	})

	t.Run("every reply is safe to send with the package's HTML parse mode", func(t *testing.T) {
		for _, err := range []error{
			household.ErrPermissionDenied, household.ErrHardConstraint, household.ErrNoSuitableMeal,
			auth.ErrTelegramNotLinked, household.ErrFoundingMember, household.ErrInvalidInput,
			errors.New("<b>&unexpected</b>"),
		} {
			if reply := telegram.ErrorReply(err); strings.ContainsAny(reply, "<>&") {
				t.Errorf("reply for %v: got %q, want no HTML-special characters", err, reply)
			}
		}
	})
}
