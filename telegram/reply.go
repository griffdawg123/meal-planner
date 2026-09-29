package telegram

import (
	"errors"

	"github.com/griffdawg123/meal-planner/auth"
	"github.com/griffdawg123/meal-planner/household"
)

// errorReplies maps errors a member's request can fail with to the reply explaining it, in
// priority order: when an error wraps several of them, the first listed wins, so a permission
// denial or hard constraint is never masked by a secondary failure.
var errorReplies = []struct {
	err   error
	reply string
}{
	{household.ErrPermissionDenied, "Sorry, you don't have permission to do that, so nothing was changed."},
	{household.ErrHardConstraint, "I can't do that because it would break a strict requirement, such as someone's allergy or diet, so nothing was changed."},
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

// genericErrorReply answers an internal or unrecognized error without exposing its detail.
const genericErrorReply = "Sorry, something went wrong on my end. Please try again later."

// ErrorReply returns a plain-language message, safe to send with ParseMode, telling a member why
// their request failed. It never includes the error's own text, which may hold internal detail. It
// returns an empty string for a nil error.
func ErrorReply(err error) string {
	if err == nil {
		return ""
	}
	for _, known := range errorReplies {
		if errors.Is(err, known.err) {
			return known.reply
		}
	}
	return genericErrorReply
}
