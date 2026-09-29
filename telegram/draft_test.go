package telegram_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/griffdawg123/meal-planner/telegram"
)

func TestFormatDraftPlan(t *testing.T) {
	t.Run("lists each night's dinner title and short description in order", func(t *testing.T) {
		got, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
			{Night: "2026-10-05", Title: "Pad thai", Description: "Rice noodles with tofu and lime."},
			{Night: "2026-10-06", Title: "Lasagne", Description: "Baked beef and tomato layers."},
		})
		if err != nil {
			t.Fatalf("format draft plan: %v", err)
		}

		want := "<b>Draft dinner plan</b>\n" +
			"\n" +
			"<b>Mon 5 Oct: Pad thai</b>\n" +
			"Rice noodles with tofu and lime.\n" +
			"\n" +
			"<b>Tue 6 Oct: Lasagne</b>\n" +
			"Baked beef and tomato layers."
		if got != want {
			t.Fatalf("message: got %q, want %q", got, want)
		}
	})

	t.Run("escapes HTML special characters in titles and descriptions", func(t *testing.T) {
		got, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
			{Night: "2026-10-05", Title: "Mac & <cheese>", Description: "Kids > adults & \"everyone\"."},
		})
		if err != nil {
			t.Fatalf("format draft plan: %v", err)
		}

		want := "<b>Draft dinner plan</b>\n" +
			"\n" +
			"<b>Mon 5 Oct: Mac &amp; &lt;cheese&gt;</b>\n" +
			"Kids &gt; adults &amp; &#34;everyone&#34;."
		if got != want {
			t.Fatalf("message: got %q, want %q", got, want)
		}
	})

	t.Run("trims surrounding whitespace from titles and descriptions", func(t *testing.T) {
		got, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
			{Night: "2026-10-05", Title: "  Pad thai\n", Description: " \tRice noodles.\n"},
		})
		if err != nil {
			t.Fatalf("format draft plan: %v", err)
		}

		want := "<b>Draft dinner plan</b>\n\n<b>Mon 5 Oct: Pad thai</b>\nRice noodles."
		if got != want {
			t.Fatalf("message: got %q, want %q", got, want)
		}
	})

	t.Run("rejects invalid drafts", func(t *testing.T) {
		tests := []struct {
			name    string
			dinners []telegram.DraftDinner
		}{
			{name: "no dinners", dinners: nil},
			{name: "night not a YYYY-MM-DD date", dinners: []telegram.DraftDinner{{Night: "Monday", Title: "Pad thai", Description: "Rice noodles."}}},
			{name: "blank title", dinners: []telegram.DraftDinner{{Night: "2026-10-05", Title: "  ", Description: "Rice noodles."}}},
			{name: "blank description", dinners: []telegram.DraftDinner{{Night: "2026-10-05", Title: "Pad thai", Description: " \t\n"}}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				got, err := telegram.FormatDraftPlan(test.dinners)
				if !errors.Is(err, telegram.ErrInvalidDraft) {
					t.Fatalf("error: got %v, want %v", err, telegram.ErrInvalidDraft)
				}
				if got != "" {
					t.Errorf("message: got %q, want empty", got)
				}
			})
		}
	})

	// The visible text of a one-dinner plan titled "Pad thai" on Mon 5 Oct is this prefix followed
	// by the description, so a description of descriptionBudget characters reaches the limit exactly.
	const visiblePrefix = "Draft dinner plan\n\nMon 5 Oct: Pad thai\n"
	descriptionBudget := telegram.MaxMessageLength - len(visiblePrefix)

	t.Run("accepts a message whose visible text is exactly Telegram's limit", func(t *testing.T) {
		description := strings.Repeat("a", descriptionBudget)
		got, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
			{Night: "2026-10-05", Title: "Pad thai", Description: description},
		})
		if err != nil {
			t.Fatalf("format draft plan: %v", err)
		}

		want := "<b>Draft dinner plan</b>\n\n<b>Mon 5 Oct: Pad thai</b>\n" + description
		if got != want {
			t.Fatalf("message: got %q, want %q", got, want)
		}
	})

	t.Run("does not count HTML markup or escapes against the limit", func(t *testing.T) {
		_, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
			{Night: "2026-10-05", Title: "Pad thai", Description: strings.Repeat("&", descriptionBudget)},
		})
		if err != nil {
			t.Fatalf("format draft plan: got %v, want nil", err)
		}
	})

	t.Run("rejects messages whose visible text exceeds Telegram's limit", func(t *testing.T) {
		tests := []struct {
			name        string
			description string
		}{
			{name: "one character over", description: strings.Repeat("a", descriptionBudget+1)},
			{name: "description alone at the limit", description: strings.Repeat("a", telegram.MaxMessageLength)},
			// Telegram measures text in UTF-16 code units, where an emoji outside the Basic
			// Multilingual Plane counts as two.
			{name: "over only when counted in UTF-16 code units", description: strings.Repeat("a", descriptionBudget-1) + "🍜"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				got, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
					{Night: "2026-10-05", Title: "Pad thai", Description: test.description},
				})
				if !errors.Is(err, telegram.ErrMessageTooLong) {
					t.Fatalf("error: got %v, want %v", err, telegram.ErrMessageTooLong)
				}
				if got != "" {
					t.Errorf("message: got %q, want empty", got)
				}
			})
		}
	})
}
