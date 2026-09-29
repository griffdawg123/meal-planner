// Package telegram formats household messages for delivery through the Telegram Bot API.
package telegram

import (
	"errors"
	"fmt"
	"html"
	"strings"
	"time"
)

// ParseMode is the Telegram Bot API parse_mode that messages from this package must be sent with.
const ParseMode = "HTML"

// nightLayout is the YYYY-MM-DD format of a household-local dinner date.
const nightLayout = "2006-01-02"

// ErrInvalidDraft identifies a draft plan that cannot be formatted as a message.
var ErrInvalidDraft = errors.New("invalid draft plan")

// DraftDinner is one night of a draft plan. Night is a YYYY-MM-DD date in the household's
// timezone. Description is a short summary of the dinner and may be empty.
type DraftDinner struct {
	Night       string
	Title       string
	Description string
}

// FormatDraftPlan renders a draft plan as a Telegram message, to be sent with ParseMode, listing
// each dinner's night, title, and short description in the order given. Titles and descriptions
// are escaped, so they may contain any text.
func FormatDraftPlan(dinners []DraftDinner) (string, error) {
	if len(dinners) == 0 {
		return "", fmt.Errorf("%w: at least one dinner is required", ErrInvalidDraft)
	}
	var message strings.Builder
	message.WriteString("<b>Draft dinner plan</b>")
	for _, dinner := range dinners {
		night, err := time.Parse(nightLayout, dinner.Night)
		if err != nil {
			return "", fmt.Errorf("%w: night %q must be a YYYY-MM-DD date", ErrInvalidDraft, dinner.Night)
		}
		title := strings.TrimSpace(dinner.Title)
		if title == "" {
			return "", fmt.Errorf("%w: dinner on %s has no title", ErrInvalidDraft, dinner.Night)
		}
		fmt.Fprintf(&message, "\n\n<b>%s: %s</b>", night.Format("Mon 2 Jan"), html.EscapeString(title))
		if description := strings.TrimSpace(dinner.Description); description != "" {
			message.WriteString("\n" + html.EscapeString(description))
		}
	}
	return message.String(), nil
}
