package household

import (
	"context"
	"fmt"
	"time"
)

// nightLayout is the YYYY-MM-DD format of a household-local dinner date, matching the away_night
// table's `night IS date(night)` check.
const nightLayout = "2006-01-02"

// AwayNight records that one member will not be eating dinner at home on one night. Night is a
// YYYY-MM-DD date in the household's timezone. Members are attending by default, so any night
// without an AwayNight means the member is present.
type AwayNight struct {
	HouseholdID string
	MemberID    string
	Night       string
}

// RecordAwayNights marks a member of the household as away on each of nights. Recording a night
// the member is already away is a no-op, so callers can resend a member's stated absences without
// first checking what is stored. Either every night is recorded or, on error, none is.
func (s *Service) RecordAwayNights(ctx context.Context, householdID, memberID string, nights ...string) error {
	return s.updateAwayNights(ctx, householdID, memberID, nights, "record", `
		INSERT INTO away_night (household_id, member_id, night) VALUES (?, ?, ?)
		ON CONFLICT (member_id, night) DO NOTHING
	`)
}

// ClearAwayNights marks a member of the household as present again on each of nights, for example
// when their plans change after a draft plan exists. Clearing a night the member was not away is
// a no-op. Either every night is cleared or, on error, none is.
func (s *Service) ClearAwayNights(ctx context.Context, householdID, memberID string, nights ...string) error {
	return s.updateAwayNights(ctx, householdID, memberID, nights, "clear", `
		DELETE FROM away_night WHERE household_id = ? AND member_id = ? AND night = ?
	`)
}

// updateAwayNights runs statement, which takes household ID, member ID, and night arguments, once
// per night in a single transaction after validating the nights and the member.
func (s *Service) updateAwayNights(ctx context.Context, householdID, memberID string, nights []string, action, statement string) error {
	if len(nights) == 0 {
		return fmt.Errorf("%w: at least one night is required", ErrInvalidInput)
	}
	for _, night := range nights {
		if err := validateNight(night); err != nil {
			return err
		}
	}

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin away night %s: %v", ErrInternal, action, err)
	}
	defer tx.Rollback()

	if err := requireHousehold(ctx, tx, householdID); err != nil {
		return err
	}
	if err := requireMember(ctx, tx, householdID, memberID); err != nil {
		return err
	}

	for _, night := range nights {
		if _, err := tx.ExecContext(ctx, statement, householdID, memberID, night); err != nil {
			return fmt.Errorf("%w: %s away night: %v", ErrInternal, action, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit away night %s: %v", ErrInternal, action, err)
	}
	return nil
}

// ListAwayNights returns every member's away nights in a household from one night to another,
// inclusive, ordered by night. An unknown household is not an error: it naturally has no rows (the
// same convention ListMembers uses).
func (s *Service) ListAwayNights(ctx context.Context, householdID, from, to string) ([]AwayNight, error) {
	if err := validateNight(from); err != nil {
		return nil, err
	}
	if err := validateNight(to); err != nil {
		return nil, err
	}
	// YYYY-MM-DD dates order the same lexically and chronologically.
	if to < from {
		return nil, fmt.Errorf("%w: night range ends %q before it starts %q", ErrInvalidInput, to, from)
	}

	awayNights := []AwayNight{}
	rows, err := s.database.QueryContext(ctx, `
		SELECT away_night.household_id, away_night.member_id, away_night.night
		FROM away_night
		JOIN member ON member.household_id = away_night.household_id AND member.id = away_night.member_id
		WHERE away_night.household_id = ? AND away_night.night BETWEEN ? AND ?
		ORDER BY away_night.night, member.created_at, member.id
	`, householdID, from, to)
	if err != nil {
		return nil, fmt.Errorf("%w: list away nights: %v", ErrInternal, err)
	}
	defer rows.Close()

	for rows.Next() {
		var awayNight AwayNight
		if err := rows.Scan(&awayNight.HouseholdID, &awayNight.MemberID, &awayNight.Night); err != nil {
			return nil, fmt.Errorf("%w: read away night: %v", ErrInternal, err)
		}
		awayNights = append(awayNights, awayNight)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list away nights: %v", ErrInternal, err)
	}

	return awayNights, nil
}

func validateNight(night string) error {
	if _, err := time.Parse(nightLayout, night); err != nil {
		return fmt.Errorf("%w: night %q must be a YYYY-MM-DD date", ErrInvalidInput, night)
	}
	return nil
}
