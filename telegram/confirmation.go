package telegram

import (
	"context"
	"errors"
	"fmt"
)

// PlanReadyMessage tells household members that a confirmed plan's exact recipes and shopping
// list have been generated.
const PlanReadyMessage = "The recipes and shopping list for the confirmed plan are ready."

// PlanReadyNotifier tells every linked member of a household when the planning workflow reports
// that a confirmed plan's recipes and shopping list are ready. It is the only source of
// PlanReadyMessage, so members are never told they were generated for a plan nobody confirmed.
type PlanReadyNotifier struct {
	directory MemberDirectory
	sender    Sender
}

// NewPlanReadyNotifier returns a notifier that resolves recipients through directory and messages
// them through sender.
func NewPlanReadyNotifier(directory MemberDirectory, sender Sender) *PlanReadyNotifier {
	return &PlanReadyNotifier{directory: directory, sender: sender}
}

// HandlePlanReady sends PlanReadyMessage to every linked member of the household, in order. If
// recipients cannot be resolved, nobody is messaged. A failed send does not stop delivery to the
// remaining members; the failures are returned together.
func (n *PlanReadyNotifier) HandlePlanReady(ctx context.Context, householdID string) error {
	recipients, err := HouseholdRecipients(ctx, n.directory, householdID)
	if err != nil {
		return err
	}
	var failures []error
	for _, recipient := range recipients {
		if recipient.TelegramUserID == 0 {
			continue
		}
		if err := n.sender.SendMessage(ctx, recipient.TelegramUserID, PlanReadyMessage, ParseMode); err != nil {
			failures = append(failures, fmt.Errorf("member %q: %w", recipient.MemberID, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("announce ready plan to household %q: %w", householdID, errors.Join(failures...))
	}
	return nil
}
