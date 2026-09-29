package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	mealdb "github.com/griffdawg123/meal-planner/db"
	"github.com/griffdawg123/meal-planner/telegram"
	_ "modernc.org/sqlite"
)

// pollTimeout is how long each Telegram long poll waits for an update.
const pollTimeout = 50 * time.Second

func main() {
	databasePath := flag.String("db", "meal-planner.db", "path to the SQLite database file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := initializeDatabase(ctx, *databasePath); err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	log.Printf("database ready: %s", *databasePath)

	token, err := telegram.BotTokenFromEnv()
	if errors.Is(err, telegram.ErrBotTokenMissing) {
		log.Printf("telegram bot not started: %v", err)
		return
	}
	if err != nil {
		log.Fatalf("load telegram bot token: %v", err)
	}
	if err := runBot(ctx, token); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("run telegram bot: %v", err)
	}
}

// runBot connects to Telegram with token and answers health checks until ctx is done.
func runBot(ctx context.Context, token string) error {
	client := telegram.NewClient(telegram.DefaultAPIURL, token, &http.Client{Timeout: pollTimeout + 10*time.Second})
	bot, err := client.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if err := client.DeleteWebhook(ctx); err != nil {
		return fmt.Errorf("switch to long polling: %w", err)
	}
	log.Printf("telegram bot connected: @%s; send it /ping to check the connection", bot.Username)

	poller := &telegram.Poller{
		Updates:    client,
		Handler:    telegram.NewHealthCheck(client, bot.Username),
		Timeout:    pollTimeout,
		RetryDelay: 5 * time.Second,
	}
	return poller.Run(ctx)
}

func initializeDatabase(ctx context.Context, databasePath string) error {
	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		return fmt.Errorf("resolve database path: %w", err)
	}

	dsn := (&url.URL{
		Scheme:   "file",
		Path:     absolutePath,
		RawQuery: "_foreign_keys=on",
	}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if err := mealdb.ApplySchema(ctx, database); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	return nil
}
