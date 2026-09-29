package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/griffdawg123/meal-planner/auth"
	"github.com/griffdawg123/meal-planner/household"
)

// Identities resolves the household member a Telegram user acts as. auth.Service implements it.
type Identities interface {
	ResolveTelegram(ctx context.Context, telegramUserID int64) (auth.Principal, error)
}

// Households authorizes and applies changes to household data. household.Service implements it.
type Households interface {
	AuthorizeMember(ctx context.Context, householdID, memberID string) error
	RecordAwayNights(ctx context.Context, householdID, memberID string, nights ...string) error
}

// DraftPlans revises a household's draft plan, enforcing its hard constraints. household.Planner
// implements it.
type DraftPlans interface {
	ReviseDinner(ctx context.Context, householdID, night string, meal household.Meal) error
}

// Commands applies the changes a Telegram user asks for. Every change is made on behalf of the
// household member the user is linked to, and only after the deterministic permission and
// constraint checks pass. The user is always replied to: with a confirmation, or with a
// plain-language explanation of why nothing was changed.
type Commands struct {
	identities Identities
	households Households
	drafts     DraftPlans
	sender     Sender
}

// NewCommands returns commands that resolve users through identities, change household data
// through households and drafts, and reply through sender.
func NewCommands(identities Identities, households Households, drafts DraftPlans, sender Sender) *Commands {
	return &Commands{identities: identities, households: households, drafts: drafts, sender: sender}
}

// MarkAway records that memberID, who must belong to the user's household, is away on nights.
func (c *Commands) MarkAway(ctx context.Context, telegramUserID int64, memberID string, nights ...string) error {
	err := c.markAway(ctx, telegramUserID, memberID, nights)
	return c.reply(ctx, telegramUserID, "Got it, I've noted who's away.", err)
}

func (c *Commands) markAway(ctx context.Context, telegramUserID int64, memberID string, nights []string) error {
	principal, err := c.identities.ResolveTelegram(ctx, telegramUserID)
	if err != nil {
		return err
	}
	if err := c.households.AuthorizeMember(ctx, principal.HouseholdID, memberID); err != nil {
		return err
	}
	return c.households.RecordAwayNights(ctx, principal.HouseholdID, memberID, nights...)
}

// ReplaceDinner replaces the user's household's dinner on night with meal, which must not break the
// hard constraints of anyone eating it.
func (c *Commands) ReplaceDinner(ctx context.Context, telegramUserID int64, night string, meal household.Meal) error {
	err := c.replaceDinner(ctx, telegramUserID, night, meal)
	return c.reply(ctx, telegramUserID, "Done, I've updated the plan and sent it to everyone.", err)
}

func (c *Commands) replaceDinner(ctx context.Context, telegramUserID int64, night string, meal household.Meal) error {
	principal, err := c.identities.ResolveTelegram(ctx, telegramUserID)
	if err != nil {
		return err
	}
	return c.drafts.ReviseDinner(ctx, principal.HouseholdID, night, meal)
}

// reply sends the user confirmation when err is nil and ErrorReply(err) otherwise, returning err
// together with any failure to send.
func (c *Commands) reply(ctx context.Context, telegramUserID int64, confirmation string, err error) error {
	text := confirmation
	if err != nil {
		text = ErrorReply(err)
	}
	if sendErr := c.sender.SendMessage(ctx, telegramUserID, text, ParseMode); sendErr != nil {
		return errors.Join(err, fmt.Errorf("reply to telegram user %d: %w", telegramUserID, sendErr))
	}
	return err
}
