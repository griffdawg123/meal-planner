# Meal Planner

An open-source, chat-first recipe book and weekly meal-planning application.

The application will create household dinner plans, collect feedback through chat, generate exact recipes and portions after approval, and produce a combined shopping list. It is intended to work as either a self-hosted application with bring-your-own LLM credentials or a managed subscription service.

See [the MVP product specification](docs/product-spec.md) for the agreed scope and product decisions.

Development can be delegated to Amp orbs without giving them GitHub credentials. See the
[local-to-orb development workflow](docs/orb-workflow.md).

Issues can also be picked up, implemented, reviewed, and merged automatically. See the
[automated dev loop](docs/devloop-workflow.md).

## Status

Product definition. Implementation has not started.

## Database initialization

Run the application to create a SQLite database and apply the current schema:

```bash
go run ./cmd/meal-planner
```

The database defaults to `meal-planner.db` in the current directory. Set a different file path
with the `-db` flag:

```bash
go run ./cmd/meal-planner -db /path/to/meal-planner.db
```

## Telegram bot

1. Register a bot with [@BotFather](https://t.me/BotFather) using `/newbot` and copy the token it
   gives you.
2. Provide the token as a secret, never in a committed file. Set exactly one of:
   - `TELEGRAM_BOT_TOKEN` to the token itself, or
   - `TELEGRAM_BOT_TOKEN_FILE` to the path of a file containing it, such as a Docker or systemd
     secret.
3. Run the application. It connects with long polling, so no public URL is needed:

   ```bash
   TELEGRAM_BOT_TOKEN_FILE=/run/secrets/telegram-bot-token go run ./cmd/meal-planner
   ```

   Long polling does not work while a webhook is set for the bot, so the application deletes any
   webhook when it starts. Only one process may poll with a token at a time; if Telegram reports
   a conflict, the application exits with an explanation instead of retrying.
4. Message the bot `/ping` and it replies `pong`; `/echo some text` replies with the same text.
   These confirm the connection works end to end. If Telegram rate limits a reply, the bot waits
   as long as Telegram asks and resends it, up to three times.

Without a token, the application initializes the database and exits without starting the bot.
