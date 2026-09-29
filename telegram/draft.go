// Package telegram formats household messages for delivery through the Telegram Bot API.
package telegram

import (
	"errors"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"
)

// ParseMode is the Telegram Bot API parse_mode that messages from this package must be sent with.
const ParseMode = "HTML"

// nightLayout is the YYYY-MM-DD format of a household-local dinner date.
const nightLayout = "2006-01-02"

// MaxMessageLength is the Telegram Bot API limit on a message's text after entity parsing, in
// Unicode code points.
const MaxMessageLength = 4096

// ErrInvalidDraft identifies a draft plan that cannot be formatted as a message.
var ErrInvalidDraft = errors.New("invalid draft plan")

// ErrMessageTooLong identifies a draft plan whose message would exceed MaxMessageLength.
var ErrMessageTooLong = errors.New("message exceeds Telegram's length limit")

// DraftDinner is one night of a draft plan. Night is a YYYY-MM-DD date in the household's
// timezone. Title and Description, a short summary of the dinner, are both required.
type DraftDinner struct {
	Night       string
	Title       string
	Description string
}

// FormatDraftPlan renders a draft plan as a Telegram message, to be sent with ParseMode, listing
// each dinner's night, title, and short description in the order given. Titles and descriptions
// are escaped, so they may contain any text. It returns ErrInvalidDraft for a malformed draft and
// ErrMessageTooLong if the message would exceed MaxMessageLength.
func FormatDraftPlan(dinners []DraftDinner) (string, error) {
	if len(dinners) == 0 {
		return "", fmt.Errorf("%w: at least one dinner is required", ErrInvalidDraft)
	}
	// message is the HTML sent to Telegram; visible is the text Telegram measures once it has
	// parsed the HTML into entities.
	var message, visible strings.Builder
	message.WriteString("<b>Draft dinner plan</b>")
	visible.WriteString("Draft dinner plan")
	for _, dinner := range dinners {
		night, err := time.Parse(nightLayout, dinner.Night)
		if err != nil {
			return "", fmt.Errorf("%w: night %q must be a YYYY-MM-DD date", ErrInvalidDraft, dinner.Night)
		}
		title := strings.TrimSpace(dinner.Title)
		if title == "" {
			return "", fmt.Errorf("%w: dinner on %s has no title", ErrInvalidDraft, dinner.Night)
		}
		description := strings.TrimSpace(dinner.Description)
		if description == "" {
			return "", fmt.Errorf("%w: dinner on %s has no description", ErrInvalidDraft, dinner.Night)
		}
		heading := night.Format("Mon 2 Jan") + ": "
		fmt.Fprintf(&message, "\n\n<b>%s%s</b>\n%s", heading, html.EscapeString(title), html.EscapeString(description))
		fmt.Fprintf(&visible, "\n\n%s%s\n%s", heading, title, description)
	}
	if length := utf8.RuneCountInString(visible.String()); length > MaxMessageLength {
		return "", fmt.Errorf("%w: %d characters, limit %d", ErrMessageTooLong, length, MaxMessageLength)
	}
	return message.String(), nil
}
