package telegram

import (
	"context"
	"html"
	"strings"
)

const echoUsage = "Send /echo followed by some text and I'll send it back."

// HealthCheck answers the /ping and /echo commands, confirming end to end that the bot receives
// messages and can reply to them. It ignores every other update.
type HealthCheck struct {
	sender Sender
}

// NewHealthCheck returns a health check that replies through sender.
func NewHealthCheck(sender Sender) *HealthCheck {
	return &HealthCheck{sender: sender}
}

// HandleUpdate replies "pong" to /ping and repeats the text after /echo, in the same chat.
func (h *HealthCheck) HandleUpdate(ctx context.Context, update Update) error {
	if update.Message == nil {
		return nil
	}
	command, argument := parseCommand(update.Message.Text)
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

// parseCommand splits a message into its command, without any "@botname" suffix, and the trimmed
// text after it.
func parseCommand(text string) (command, argument string) {
	command, argument, _ = strings.Cut(strings.TrimSpace(text), " ")
	command, _, _ = strings.Cut(command, "@")
	return command, strings.TrimSpace(argument)
}
