package telegram_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffdawg123/meal-planner/telegram"
)

func TestBotTokenFromEnv(t *testing.T) {
	t.Run("reads the token from the token environment variable", func(t *testing.T) {
		t.Setenv(telegram.BotTokenEnv, testToken)
		t.Setenv(telegram.BotTokenFileEnv, "")

		got, err := telegram.BotTokenFromEnv()
		if err != nil {
			t.Fatalf("bot token: got error %v, want nil", err)
		}
		if got != testToken {
			t.Fatalf("bot token: got %q, want %q", got, testToken)
		}
	})

	t.Run("reads the token from the secret file named by the token file environment variable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "telegram-bot-token")
		if err := os.WriteFile(path, []byte(testToken+"\n"), 0o600); err != nil {
			t.Fatalf("write token file: %v", err)
		}
		t.Setenv(telegram.BotTokenEnv, "")
		t.Setenv(telegram.BotTokenFileEnv, path)

		got, err := telegram.BotTokenFromEnv()
		if err != nil {
			t.Fatalf("bot token: got error %v, want nil", err)
		}
		if got != testToken {
			t.Fatalf("bot token: got %q, want %q", got, testToken)
		}
	})

	t.Run("reports a missing token when neither variable is set", func(t *testing.T) {
		t.Setenv(telegram.BotTokenEnv, "")
		t.Setenv(telegram.BotTokenFileEnv, "")

		_, err := telegram.BotTokenFromEnv()
		if !errors.Is(err, telegram.ErrBotTokenMissing) {
			t.Fatalf("bot token: got error %v, want %v", err, telegram.ErrBotTokenMissing)
		}
	})

	t.Run("rejects an empty token file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "telegram-bot-token")
		if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
			t.Fatalf("write token file: %v", err)
		}
		t.Setenv(telegram.BotTokenEnv, "")
		t.Setenv(telegram.BotTokenFileEnv, path)

		_, err := telegram.BotTokenFromEnv()
		if !errors.Is(err, telegram.ErrBotTokenMissing) {
			t.Fatalf("bot token: got error %v, want %v", err, telegram.ErrBotTokenMissing)
		}
	})

	t.Run("reports an unreadable token file", func(t *testing.T) {
		t.Setenv(telegram.BotTokenEnv, "")
		t.Setenv(telegram.BotTokenFileEnv, filepath.Join(t.TempDir(), "missing"))

		_, err := telegram.BotTokenFromEnv()
		if err == nil || errors.Is(err, telegram.ErrBotTokenMissing) {
			t.Fatalf("bot token: got error %v, want a file read error", err)
		}
	})

	t.Run("rejects setting both variables because the intended source is ambiguous", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "telegram-bot-token")
		if err := os.WriteFile(path, []byte(testToken), 0o600); err != nil {
			t.Fatalf("write token file: %v", err)
		}
		t.Setenv(telegram.BotTokenEnv, testToken)
		t.Setenv(telegram.BotTokenFileEnv, path)

		_, err := telegram.BotTokenFromEnv()
		if !errors.Is(err, telegram.ErrBotTokenAmbiguous) {
			t.Fatalf("bot token: got error %v, want %v", err, telegram.ErrBotTokenAmbiguous)
		}
	})
}
