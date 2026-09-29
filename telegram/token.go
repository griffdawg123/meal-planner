package telegram

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// The bot token is a secret: it is read from the environment at startup and never stored in the
// repository or the database. Set exactly one of these variables.
const (
	// BotTokenEnv holds the bot token itself.
	BotTokenEnv = "TELEGRAM_BOT_TOKEN"
	// BotTokenFileEnv holds the path to a file containing the bot token, such as a Docker or
	// systemd secret.
	BotTokenFileEnv = "TELEGRAM_BOT_TOKEN_FILE"
)

// ErrBotTokenMissing identifies that no bot token was configured.
var ErrBotTokenMissing = errors.New("telegram bot token is not set")

// ErrBotTokenAmbiguous identifies that the bot token was configured in more than one place.
var ErrBotTokenAmbiguous = errors.New("telegram bot token is set in both " + BotTokenEnv + " and " + BotTokenFileEnv)

// BotTokenFromEnv returns the bot token from BotTokenEnv, or from the file named by
// BotTokenFileEnv with surrounding whitespace removed.
func BotTokenFromEnv() (string, error) {
	token := strings.TrimSpace(os.Getenv(BotTokenEnv))
	path := os.Getenv(BotTokenFileEnv)
	switch {
	case token != "" && path != "":
		return "", ErrBotTokenAmbiguous
	case token != "":
		return token, nil
	case path == "":
		return "", fmt.Errorf("%w: set %s or %s", ErrBotTokenMissing, BotTokenEnv, BotTokenFileEnv)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", BotTokenFileEnv, err)
	}
	token = strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("%w: %s names an empty file", ErrBotTokenMissing, BotTokenFileEnv)
	}
	return token, nil
}
