package household

import (
	"context"
	"database/sql"
	"fmt"
)

// AwaySoftPreferenceFactor scales the strength of a soft preference belonging to a member who is
// away on a night. It is below one so the plan favors the members eating that dinner, but above
// zero so an away member's tastes can still break ties (for example, for leftovers).
const AwaySoftPreferenceFactor = 0.25

// WeightedPreference is a soft preference with the weight planning should give it on one night.
type WeightedPreference struct {
	Preference
	Weight float64
}

// NightPreferences is what planning must honor and should balance for one household dinner.
type NightPreferences struct {
	Night string
	// HardConstraints are the household-wide hard constraints and those of every member present
	// on Night. Hard constraints apply to everyone eating the meal, so an away member's are omitted.
	HardConstraints []Preference
	// SoftPreferences are every household-wide and member soft preference. Each weight is the
	// preference's strength, scaled by AwaySoftPreferenceFactor when its member is away on Night.
	SoftPreferences []WeightedPreference
}

// NightPreferences resolves a household's preferences for one night, applying the attendance
// recorded for that night at the time of the call. Planning calls it when generating a draft and
// again when revising one, so attendance changed after the draft is reflected.
func (s *Service) NightPreferences(ctx context.Context, householdID, night string) (NightPreferences, error) {
	if err := validateNight(night); err != nil {
		return NightPreferences{}, err
	}

	// A read transaction gives the household check and the preference query one consistent view of
	// attendance.
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return NightPreferences{}, fmt.Errorf("%w: begin night preferences: %v", ErrInternal, err)
	}
	defer tx.Rollback()

	if err := requireHousehold(ctx, tx, householdID); err != nil {
		return NightPreferences{}, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			preference.id, preference.household_id, preference.member_id, preference.strength,
			preference.value, preference.category, preference.kind,
			away_night.member_id IS NOT NULL
		FROM preference
		LEFT JOIN away_night
			ON away_night.household_id = preference.household_id
			AND away_night.member_id = preference.member_id
			AND away_night.night = ?
		WHERE preference.household_id = ?
		ORDER BY preference.created_at, preference.id
	`, night, householdID)
	if err != nil {
		return NightPreferences{}, fmt.Errorf("%w: list night preferences: %v", ErrInternal, err)
	}
	defer rows.Close()

	resolved := NightPreferences{
		Night:           night,
		HardConstraints: []Preference{},
		SoftPreferences: []WeightedPreference{},
	}
	for rows.Next() {
		var preference Preference
		var memberID sql.NullString
		var away bool
		if err := rows.Scan(&preference.ID, &preference.HouseholdID, &memberID, &preference.Strength, &preference.Value, &preference.Category, &preference.Kind, &away); err != nil {
			return NightPreferences{}, fmt.Errorf("%w: read night preference: %v", ErrInternal, err)
		}
		preference.MemberID = memberID.String

		switch preference.Kind {
		case Hard:
			if !away {
				resolved.HardConstraints = append(resolved.HardConstraints, preference)
			}
		case Soft:
			weight := float64(preference.Strength.Int16)
			if away {
				weight *= AwaySoftPreferenceFactor
			}
			resolved.SoftPreferences = append(resolved.SoftPreferences, WeightedPreference{Preference: preference, Weight: weight})
		}
	}
	if err := rows.Err(); err != nil {
		return NightPreferences{}, fmt.Errorf("%w: list night preferences: %v", ErrInternal, err)
	}

	return resolved, nil
}
