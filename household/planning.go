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
	// Satisfies lists preference values the meal fulfills, such as a cuisine or "under 30 minutes".
	Satisfies []string
	// Conflicts lists preference values the meal violates, such as an allergen it contains, a diet
	// it breaks, or a disliked ingredient.
	Conflicts []string
}

// PlanDinners chooses one candidate meal for each night, in order. A meal that conflicts with any of
// a night's hard constraints is never chosen for that night.
//
// Soft preferences are balanced over time rather than letting the strongest always win: a
// preference's weight is divided by one plus the number of dinners, among recent and those already
// chosen, that satisfied its owner (a member, or the household for household-wide preferences).
// Owners therefore share dinners roughly in proportion to their preferences' weights. Recent is the
// household's previously planned dinners, so balancing carries across separate planning runs. Ties
// go to the earlier candidate.
func PlanDinners(nights []NightPreferences, candidates, recent []Meal) ([]Meal, error) {
	history := append([]Meal(nil), recent...)
	dinners := make([]Meal, 0, len(nights))
	for _, night := range nights {
		satisfied := map[string]int{}
		for _, dinner := range history {
			owners := map[string]bool{}
			for _, preference := range night.SoftPreferences {
				if relates(dinner.Satisfies, preference.Value) {
					owners[preference.MemberID] = true
				}
			}
			for owner := range owners {
				satisfied[owner]++
			}
		}

		best, bestScore, found := Meal{}, 0.0, false
		for _, meal := range candidates {
			if blocked(meal, night.HardConstraints) {
				continue
			}
			score := 0.0
			for _, preference := range night.SoftPreferences {
				if relates(meal.Satisfies, preference.Value) {
					score += preference.Weight / float64(1+satisfied[preference.MemberID])
				}
				if relates(meal.Conflicts, preference.Value) {
					score -= preference.Weight
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

func blocked(meal Meal, constraints []Preference) bool {
	for _, constraint := range constraints {
		if relates(meal.Conflicts, constraint.Value) {
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
