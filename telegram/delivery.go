package telegram

import (
	"context"
	"errors"
	"fmt"
)

// ErrDeliveryFailed identifies a draft plan that could not be sent to one or more linked members.
var ErrDeliveryFailed = errors.New("draft plan delivery failed")

// Recipient is a household member who may receive a draft plan. TelegramUserID is zero when the
// member has not linked a Telegram account.
type Recipient struct {
	MemberID       string
	TelegramUserID int64
}

// Sender sends a text message to a Telegram chat through the Bot API.
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text, parseMode string) error
}

// DeliverDraftPlan formats the draft plan and sends it to the private chat of every linked
// recipient, in order. Unlinked recipients are skipped. A failed send does not stop delivery to the
// remaining recipients; the failures are returned together, wrapped in ErrDeliveryFailed. A
// malformed draft is rejected, as by FormatDraftPlan, before anyone is messaged.
func DeliverDraftPlan(ctx context.Context, sender Sender, recipients []Recipient, dinners []DraftDinner) error {
	message, err := FormatDraftPlan(dinners)
	if err != nil {
		return err
	}
	var failures []error
	for _, recipient := range recipients {
		if recipient.TelegramUserID == 0 {
			continue
		}
		if err := sender.SendMessage(ctx, recipient.TelegramUserID, message, ParseMode); err != nil {
			failures = append(failures, fmt.Errorf("member %q: %w", recipient.MemberID, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%w: %w", ErrDeliveryFailed, errors.Join(failures...))
	}
	return nil
}
