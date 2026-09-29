package telegram

import (
	"context"
	"fmt"

	"github.com/griffdawg123/meal-planner/auth"
	"github.com/griffdawg123/meal-planner/household"
)

// MemberDirectory lists a household's members with the Telegram user linked to each. auth.Service
// implements it.
type MemberDirectory interface {
	HouseholdTelegramMembers(ctx context.Context, householdID string) ([]auth.TelegramMember, error)
}

// HouseholdRecipients resolves every member of the household, with their linked Telegram user or
// zero if unlinked, as draft plan recipients.
func HouseholdRecipients(ctx context.Context, directory MemberDirectory, householdID string) ([]Recipient, error) {
	members, err := directory.HouseholdTelegramMembers(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("resolve recipients for household %q: %w", householdID, err)
	}
	recipients := make([]Recipient, 0, len(members))
	for _, member := range members {
		recipients = append(recipients, Recipient{MemberID: member.MemberID, TelegramUserID: member.TelegramUserID})
	}
	return recipients, nil
}

// DraftPlanNotifier sends each draft plan the planning workflow creates to every linked member of
// the draft's household.
type DraftPlanNotifier struct {
	directory MemberDirectory
	sender    Sender
}

// NewDraftPlanNotifier returns a notifier that resolves recipients through directory and messages
// them through sender.
func NewDraftPlanNotifier(directory MemberDirectory, sender Sender) *DraftPlanNotifier {
	return &DraftPlanNotifier{directory: directory, sender: sender}
}

// Subscribe registers the notifier for every draft plan published to events.
func (n *DraftPlanNotifier) Subscribe(events *household.DraftPlanEvents) {
	events.Subscribe(n.HandleDraftPlanCreated)
}

// HandleDraftPlanCreated resolves the draft's household members and delivers the draft to each
// linked one, as DeliverDraftPlan does. If recipients cannot be resolved, nobody is messaged.
func (n *DraftPlanNotifier) HandleDraftPlanCreated(ctx context.Context, event household.DraftPlanCreated) error {
	recipients, err := HouseholdRecipients(ctx, n.directory, event.HouseholdID)
	if err != nil {
		return err
	}
	dinners := make([]DraftDinner, 0, len(event.Dinners))
	for _, dinner := range event.Dinners {
		dinners = append(dinners, DraftDinner(dinner))
	}
	if err := DeliverDraftPlan(ctx, n.sender, recipients, dinners); err != nil {
		return fmt.Errorf("deliver draft plan to household %q: %w", event.HouseholdID, err)
	}
	return nil
}
