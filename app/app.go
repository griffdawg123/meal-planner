// Package app composes the meal planner's services into one application.
package app

import (
	"database/sql"

	"github.com/griffdawg123/meal-planner/auth"
	"github.com/griffdawg123/meal-planner/household"
	"github.com/griffdawg123/meal-planner/telegram"
)

// App is the composed application. Every draft Planner creates is delivered to the household's
// linked members through the Telegram sender.
type App struct {
	Households      *household.Service
	Auth            *auth.Service
	DraftPlanEvents *household.DraftPlanEvents
	Planner         *household.Planner
	// Commands applies changes Telegram users ask for, after permission and constraint checks.
	Commands *telegram.Commands
}

// New composes the application's services over database, sending Telegram messages through sender.
func New(database *sql.DB, sender telegram.Sender) *App {
	households := household.NewService(database)
	authentication := auth.NewService(database)
	events := &household.DraftPlanEvents{}
	telegram.NewDraftPlanNotifier(authentication, sender).Subscribe(events)
	planner := household.NewPlanner(households, events)
	return &App{
		Households:      households,
		Auth:            authentication,
		DraftPlanEvents: events,
		Planner:         planner,
		Commands:        telegram.NewCommands(authentication, households, planner, sender),
	}
}
