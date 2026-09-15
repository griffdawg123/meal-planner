# Meal Planner

An open-source, chat-first recipe book and weekly meal-planning application.

The application will create household dinner plans, collect feedback through chat, generate exact recipes and portions after approval, and produce a combined shopping list. It is intended to work as either a self-hosted application with bring-your-own LLM credentials or a managed subscription service.

See [the MVP product specification](docs/product-spec.md) for the agreed scope and product decisions.

Development can be delegated to Amp orbs without giving them GitHub credentials. See the
[local-to-orb development workflow](docs/orb-workflow.md).

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
