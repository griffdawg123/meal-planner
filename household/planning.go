package household

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoSuitableMeal identifies a night on which every candidate meal breaks a hard constraint.
var ErrNoSuitableMeal = errors.New("no suitable meal")

// Meal is a candidate dinner described by the preference values it relates to.
type Meal struct {
	Title string
	// Description is a short summary of the meal shown to members alongside its title.
	Description string
	// Satisfies lists preference values the meal fulfills, such as a cuisine or "under 30 minutes".
	Satisfies []string
	// Conflicts lists preference values the meal violates, such as an allergen it contains, a diet
	// it breaks, or a disliked ingredient.
	Conflicts []string
}

// PlanDinners chooses one candidate meal for each night, in order. A meal that breaks any of a
// night's hard constraints is never chosen for that night: it must not conflict with a hard allergy,
// diet, or dislike, and must satisfy a hard cuisine, budget, or cooking time.
//
// A soft preference is favorable (a cuisine, budget, or cooking time its owner wants a meal to
// satisfy) or adverse (an allergy, diet, or dislike its owner wants a meal not to conflict with). A
// meal gains the weight of each favorable preference it satisfies and loses the weight of each
// adverse preference it conflicts with, so opposing preferences for the same value stay distinct.
//
// Soft preferences are balanced over time rather than letting the strongest always win: a
// preference's weight is divided by one plus the number of dinners, among recent and those already
// chosen, that went its owner's way (a member, or the household for household-wide preferences).
// A dinner goes an owner's way on favorable preferences when it satisfies any of them, and on
// adverse preferences when it conflicts with none of them; the two are tallied separately. Owners
// therefore share dinners roughly in proportion to their preferences' weights, including when one
// member's preference is opposed by another's dislike. Recent is the household's previously
// planned dinners, so balancing carries across separate planning runs. Ties go to the earlier
// candidate.
func PlanDinners(nights []NightPreferences, candidates, recent []Meal) ([]Meal, error) {
	history := append([]Meal(nil), recent...)
	dinners := make([]Meal, 0, len(nights))
	for _, night := range nights {
		wins := map[ledger]int{}
		for _, dinner := range history {
			for key, won := range outcomes(dinner, night.SoftPreferences) {
				if won {
					wins[key]++
				}
			}
		}

		best, bestScore, found := Meal{}, 0.0, false
		for _, meal := range candidates {
			if blocked(meal, night.HardConstraints) {
				continue
			}
			score := 0.0
			for _, preference := range night.SoftPreferences {
				key := ledgerOf(preference)
				weight := preference.Weight / float64(1+wins[key])
				if key.adverse && relates(meal.Conflicts, preference.Value) {
					score -= weight
				}
				if !key.adverse && relates(meal.Satisfies, preference.Value) {
					score += weight
				}
			}
			if !found || score > bestScore {
				best, bestScore, found = meal, score, true
			}
		}
		if !found {
			return nil, fmt.Errorf("%w on %s", ErrNoSuitableMeal, night.Night)
		}
		dinners = append(dinners, best)
		history = append(history, best)
	}
	return dinners, nil
}

// ledger identifies an owner's favorable or adverse preferences, whose wins are tallied separately.
type ledger struct {
	owner   string
	adverse bool
}

func ledgerOf(preference WeightedPreference) ledger {
	return ledger{owner: preference.MemberID, adverse: adverse(preference.Category)}
}

// adverse reports whether a category names something its owner wants a meal not to conflict with,
// rather than something they want a meal to satisfy.
func adverse(category PreferenceCategory) bool {
	switch category {
	case Allergy, DietaryRestriction, Dislike:
		return true
	default:
		return false
	}
}

// outcomes reports, for each ledger with a preference, whether dinner went that ledger's way.
func outcomes(dinner Meal, preferences []WeightedPreference) map[ledger]bool {
	won := map[ledger]bool{}
	for _, preference := range preferences {
		key := ledgerOf(preference)
		if key.adverse {
			if _, seen := won[key]; !seen {
				won[key] = true
			}
			if relates(dinner.Conflicts, preference.Value) {
				won[key] = false
			}
		} else if relates(dinner.Satisfies, preference.Value) {
			won[key] = true
		}
	}
	return won
}

// blocked reports whether meal breaks any constraint: by conflicting with an adverse one, or by
// failing to satisfy a favorable one such as a required cooking time.
func blocked(meal Meal, constraints []Preference) bool {
	for _, constraint := range constraints {
		if adverse(constraint.Category) {
			if relates(meal.Conflicts, constraint.Value) {
				return true
			}
		} else if !relates(meal.Satisfies, constraint.Value) {
			return true
		}
	}
	return false
}

func relates(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}
