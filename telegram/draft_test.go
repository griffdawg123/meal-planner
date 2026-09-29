package telegram_test

import (
	"errors"
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

	t.Run("omits the description line for a dinner without one", func(t *testing.T) {
		got, err := telegram.FormatDraftPlan([]telegram.DraftDinner{
			{Night: "2026-10-05", Title: "Plain risotto"},
			{Night: "2026-10-06", Title: "Lasagne", Description: "Baked beef and tomato layers."},
		})
		if err != nil {
			t.Fatalf("format draft plan: %v", err)
		}

		want := "<b>Draft dinner plan</b>\n" +
			"\n" +
			"<b>Mon 5 Oct: Plain risotto</b>\n" +
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
			{Night: "2026-10-05", Title: "  Pad thai\n", Description: " \t "},
		})
		if err != nil {
			t.Fatalf("format draft plan: %v", err)
		}

		want := "<b>Draft dinner plan</b>\n\n<b>Mon 5 Oct: Pad thai</b>"
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
			{name: "night not a YYYY-MM-DD date", dinners: []telegram.DraftDinner{{Night: "Monday", Title: "Pad thai"}}},
			{name: "blank title", dinners: []telegram.DraftDinner{{Night: "2026-10-05", Title: "  "}}},
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
}
