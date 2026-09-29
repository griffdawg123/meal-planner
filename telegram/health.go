package telegram

import (
	"context"
	"html"
	"strings"
)

const echoUsage = "Send /echo followed by some text and I'll send it back."

// HealthCheck answers the /ping and /echo commands, confirming end to end that the bot receives
// messages and can reply to them. It ignores every other update, including commands addressed to
// other bots.
type HealthCheck struct {
	sender   Sender
	username string
}

// NewHealthCheck returns a health check for the bot named username, as reported by GetMe, that
// replies through sender.
func NewHealthCheck(sender Sender, username string) *HealthCheck {
	return &HealthCheck{sender: sender, username: username}
}

// HandleUpdate replies "pong" to /ping and repeats the text after /echo, in the same chat.
func (h *HealthCheck) HandleUpdate(ctx context.Context, update Update) error {
	if update.Message == nil {
		return nil
	}
	command, addressee, addressed, argument := parseCommand(update.Message.Text)
	if addressed && !strings.EqualFold(addressee, h.username) {
		return nil
	}
	var reply string
	switch command {
	case "/ping":
		reply = "pong"
	case "/echo":
		reply = echoUsage
		if argument != "" {
			reply = html.EscapeString(argument)
		}
	default:
		return nil
	}
	return h.sender.SendMessage(ctx, update.Message.Chat.ID, reply, ParseMode)
}

// parseCommand splits a message into its command, the bot it is addressed to by an "@botname"
// suffix, whether it has such a suffix, and the trimmed text after it.
func parseCommand(text string) (command, addressee string, addressed bool, argument string) {
	command, argument, _ = strings.Cut(strings.TrimSpace(text), " ")
	command, addressee, addressed = strings.Cut(command, "@")
	return command, addressee, addressed, strings.TrimSpace(argument)
}
