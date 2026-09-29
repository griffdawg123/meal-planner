// Package household creates and manages households.
package household

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrInvalidInput identifies an error that callers can safely present to a user.
	ErrInvalidInput = errors.New("invalid household input")
	// ErrInternal identifies a database error that callers should not expose to a user.
	ErrInternal = errors.New("household internal error")
	// ErrHouseholdNotFound identifies an operation on a household that does not exist.
	ErrHouseholdNotFound = errors.New("household not found")
	// ErrMemberNotFound identifies an operation on a member that does not exist in the household.
	ErrMemberNotFound = errors.New("household member not found")
	// ErrFoundingMember identifies an attempt to remove the member who founded the household.
	ErrFoundingMember = errors.New("founding household member cannot be removed")
	// ErrPreferenceNotFound identifies an operation on a preference that does not exist in the household.
	ErrPreferenceNotFound = errors.New("household preference not found")
	// ErrPermissionDenied identifies an action the acting member is not allowed to perform, such as
	// changing another household's data. Nothing is changed.
	ErrPermissionDenied = errors.New("household permission denied")
	// ErrHardConstraint identifies a change rejected because it would break a hard constraint, such
	// as an allergy or diet. Nothing is changed.
	ErrHardConstraint = errors.New("blocked by household hard constraint")
)

// Service provides household operations backed by a database.
type Service struct {
	database *sql.DB
}

// Household is a newly created household and its first member.
type Household struct {
	ID        string
	Name      string
	Timezone  string
	CreatorID string
}

// Member belongs to a household.
type Member struct {
	ID          string
	HouseholdID string
	Name        string
}

// PreferenceKind distinguishes a hard constraint from a soft preference.
type PreferenceKind string

const (
	Hard PreferenceKind = "hard"
	Soft PreferenceKind = "soft"
)

// PreferenceCategory classifies what a preference is about.
type PreferenceCategory string

const (
	Allergy            PreferenceCategory = "allergy"
	DietaryRestriction PreferenceCategory = "dietary_restriction"
	Dislike            PreferenceCategory = "dislike"
	Cuisine            PreferenceCategory = "cuisine"
	Budget             PreferenceCategory = "budget"
	CookingTime        PreferenceCategory = "cooking_time"
)

// Preference is a hard constraint or soft preference. It belongs to a household, or, when
// MemberID is set, to one member of that household.
type Preference struct {
	ID          string
	HouseholdID string
	MemberID    string
	Strength    sql.NullInt16
	Value       string
	Category    PreferenceCategory
	Kind        PreferenceKind
}

// NewService creates a household service backed by database.
func NewService(database *sql.DB) *Service {
	return &Service{database: database}
}

// CreateHousehold creates a household and makes creatorName its first member.
func (s *Service) CreateHousehold(ctx context.Context, name, timezone, creatorName string) (Household, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Household{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}

	creatorName = strings.TrimSpace(creatorName)
	if creatorName == "" {
		return Household{}, fmt.Errorf("%w: creator name is required", ErrInvalidInput)
	}

	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return Household{}, fmt.Errorf("%w: timezone is required", ErrInvalidInput)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Household{}, fmt.Errorf("%w: unrecognized timezone %q", ErrInvalidInput, timezone)
	}

	household := Household{
		ID:        uuid.NewString(),
		Name:      name,
		Timezone:  timezone,
		CreatorID: uuid.NewString(),
	}

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Household{}, fmt.Errorf("%w: begin creation: %v", ErrInternal, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO household (id, name, timezone, created_by_member_id) VALUES (?, ?, ?, ?)`,
		household.ID, household.Name, household.Timezone, household.CreatorID,
	); err != nil {
		return Household{}, fmt.Errorf("%w: insert household: %v", ErrInternal, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO member (id, household_id, name) VALUES (?, ?, ?)`,
		household.CreatorID, household.ID, creatorName,
	); err != nil {
		return Household{}, fmt.Errorf("%w: insert creator: %v", ErrInternal, err)
	}
	if err := tx.Commit(); err != nil {
		return Household{}, fmt.Errorf("%w: commit creation: %v", ErrInternal, err)
	}

	return household, nil
}

// AddMember adds a member to an existing household.
func (s *Service) AddMember(ctx context.Context, householdID, name string) (Member, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Member{}, fmt.Errorf("%w: member name is required", ErrInvalidInput)
	}

	member := Member{
		ID:          uuid.NewString(),
		HouseholdID: householdID,
		Name:        name,
	}
	result, err := s.database.ExecContext(ctx, `
		INSERT INTO member (id, household_id, name)
		SELECT ?, id, ? FROM household WHERE id = ?
	`, member.ID, member.Name, member.HouseholdID)
	if err != nil {
		return Member{}, fmt.Errorf("%w: insert member: %v", ErrInternal, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Member{}, fmt.Errorf("%w: inspect member insertion: %v", ErrInternal, err)
	}
	if rowsAffected == 0 {
		return Member{}, fmt.Errorf("%w: %q", ErrHouseholdNotFound, householdID)
	}

	return member, nil
}

// ListMembers returns all members in a household.
func (s *Service) ListMembers(ctx context.Context, householdID string) ([]Member, error) {
	// An unknown household returns an empty slice because the household filter naturally has no rows
	// and callers can treat known-empty and unknown households identically when displaying members.
	members := []Member{}
	rows, err := s.database.QueryContext(ctx, `
		SELECT id, household_id, name
		FROM member
		WHERE household_id = ?
		ORDER BY created_at, id
	`, householdID)
	if err != nil {
		return nil, fmt.Errorf("%w: list members: %v", ErrInternal, err)
	}
	defer rows.Close()

	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.ID, &member.HouseholdID, &member.Name); err != nil {
			return nil, fmt.Errorf("%w: read member: %v", ErrInternal, err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list members: %v", ErrInternal, err)
	}

	return members, nil
}

// RemoveMember removes a non-founding member from a household.
func (s *Service) RemoveMember(ctx context.Context, householdID, memberID string) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin member removal: %v", ErrInternal, err)
	}
	defer tx.Rollback()

	var foundingMemberID string
	if err := tx.QueryRowContext(ctx,
		`SELECT created_by_member_id FROM household WHERE id = ?`, householdID,
	).Scan(&foundingMemberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %q", ErrHouseholdNotFound, householdID)
		}
		return fmt.Errorf("%w: read household founder: %v", ErrInternal, err)
	}
	if memberID == foundingMemberID {
		return fmt.Errorf("%w: %q", ErrFoundingMember, memberID)
	}

	result, err := tx.ExecContext(ctx,
		`DELETE FROM member WHERE household_id = ? AND id = ?`, householdID, memberID,
	)
	if err != nil {
		return fmt.Errorf("%w: delete member: %v", ErrInternal, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: inspect member removal: %v", ErrInternal, err)
	}
	// Removing an absent member is an error so callers can distinguish a successful removal from a
	// stale or incorrect member ID; this also makes repeated removal attempts observable.
	if rowsAffected == 0 {
		return fmt.Errorf("%w: %q", ErrMemberNotFound, memberID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit member removal: %v", ErrInternal, err)
	}
	return nil
}

// AddPreference records a hard constraint or soft preference for a household, or for one of
// its members when memberID is set. A hard preference must not carry a strength; a soft
// preference requires one from 1 (weakest) to 5 (strongest).
func (s *Service) AddPreference(ctx context.Context, householdID, memberID string, strength sql.NullInt16, value string, category PreferenceCategory, kind PreferenceKind) (Preference, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Preference{}, fmt.Errorf("%w: value is required", ErrInvalidInput)
	}
	switch kind {
	case Hard:
		if strength.Valid {
			return Preference{}, fmt.Errorf("%w: a hard preference cannot have a strength", ErrInvalidInput)
		}
	case Soft:
		if !strength.Valid || strength.Int16 < 1 || strength.Int16 > 5 {
			return Preference{}, fmt.Errorf("%w: a soft preference requires a strength between 1 and 5", ErrInvalidInput)
		}
	default:
		return Preference{}, fmt.Errorf("%w: unrecognized preference kind %q", ErrInvalidInput, kind)
	}

	preference := Preference{
		ID:          uuid.NewString(),
		HouseholdID: householdID,
		MemberID:    memberID,
		Strength:    strength,
		Value:       value,
		Category:    category,
		Kind:        kind,
	}

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Preference{}, fmt.Errorf("%w: begin preference creation: %v", ErrInternal, err)
	}
	defer tx.Rollback()

	if err := requireHousehold(ctx, tx, householdID); err != nil {
		return Preference{}, err
	}
	if memberID != "" {
		if err := requireMember(ctx, tx, householdID, memberID); err != nil {
			return Preference{}, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO preference (id, household_id, member_id, strength, value, category, kind)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, preference.ID, preference.HouseholdID, nullableMemberID(memberID), preference.Strength, preference.Value, preference.Category, preference.Kind); err != nil {
		return Preference{}, fmt.Errorf("%w: insert preference: %v", ErrInternal, err)
	}
	if err := tx.Commit(); err != nil {
		return Preference{}, fmt.Errorf("%w: commit preference creation: %v", ErrInternal, err)
	}

	return preference, nil
}

// PreferenceFilter narrows a ListPreferences call. The zero value ("", "") applies no filter;
// a non-empty Category or Kind restricts results to that value, and both can be set together.
type PreferenceFilter struct {
	Category PreferenceCategory
	Kind     PreferenceKind
}

// ListPreferences returns a household's preferences, optionally narrowed by filter. When
// memberID is set, the result also includes that member's own preferences alongside the
// household-wide ones; when it is empty, only household-wide preferences are returned. The member
// must belong to the household. An unknown household is not an error: it naturally has no rows, so
// callers can treat unknown and known-empty identically (the same convention ListMembers uses).
func (s *Service) ListPreferences(ctx context.Context, householdID, memberID string, filter PreferenceFilter) ([]Preference, error) {
	preferences := []Preference{}
	if memberID != "" {
		var exists int
		err := s.database.QueryRowContext(ctx,
			`SELECT 1 FROM member WHERE household_id = ? AND id = ?`, householdID, memberID,
		).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %q", ErrMemberNotFound, memberID)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: check member: %v", ErrInternal, err)
		}
	}

	query := strings.Builder{}
	query.WriteString(`
		SELECT id, household_id, member_id, strength, value, category, kind
		FROM preference
		WHERE household_id = ? AND (member_id IS NULL OR member_id = ?)
	`)
	args := []any{householdID, memberID}
	if filter.Category != "" {
		query.WriteString(` AND category = ?`)
		args = append(args, filter.Category)
	}
	if filter.Kind != "" {
		query.WriteString(` AND kind = ?`)
		args = append(args, filter.Kind)
	}
	query.WriteString(` ORDER BY created_at, id`)

	rows, err := s.database.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("%w: list preferences: %v", ErrInternal, err)
	}
	defer rows.Close()

	for rows.Next() {
		var preference Preference
		var memberID sql.NullString
		if err := rows.Scan(&preference.ID, &preference.HouseholdID, &memberID, &preference.Strength, &preference.Value, &preference.Category, &preference.Kind); err != nil {
			return nil, fmt.Errorf("%w: read preference: %v", ErrInternal, err)
		}
		preference.MemberID = memberID.String
		preferences = append(preferences, preference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list preferences: %v", ErrInternal, err)
	}

	return preferences, nil
}

// RemovePreference deletes one preference. It must belong to the household, and be either a
// household-wide preference (member_id NULL, removable via any member) or one belonging to the
// given member specifically.
func (s *Service) RemovePreference(ctx context.Context, householdID, memberID, preferenceID string) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin preference removal: %v", ErrInternal, err)
	}
	defer tx.Rollback()

	if err := requireHousehold(ctx, tx, householdID); err != nil {
		return err
	}
	if memberID != "" {
		if err := requireMember(ctx, tx, householdID, memberID); err != nil {
			return err
		}
	}

	result, err := tx.ExecContext(ctx, `
		DELETE FROM preference
		WHERE household_id = ? AND (member_id IS NULL OR member_id = ?) AND id = ?
	`, householdID, memberID, preferenceID)
	if err != nil {
		return fmt.Errorf("%w: delete preference: %v", ErrInternal, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: inspect preference removal: %v", ErrInternal, err)
	}
	// Removing an absent preference is an error so callers can distinguish a successful removal from a
	// stale or incorrect preference ID; this also makes repeated removal attempts observable.
	if rowsAffected == 0 {
		return fmt.Errorf("%w: %q", ErrPreferenceNotFound, preferenceID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit preference removal: %v", ErrInternal, err)
	}
	return nil
}

// nullableMemberID converts the empty-string "household-wide" sentinel used throughout this
// file's Preference methods into a SQL NULL; the preference table's CHECK constraint rejects an
// empty-string member_id outright.
func nullableMemberID(memberID string) sql.NullString {
	return sql.NullString{String: memberID, Valid: memberID != ""}
}

func requireHousehold(ctx context.Context, tx *sql.Tx, householdID string) error {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM household WHERE id = ?`, householdID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", ErrHouseholdNotFound, householdID)
	}
	if err != nil {
		return fmt.Errorf("%w: check household: %v", ErrInternal, err)
	}
	return nil
}

func requireMember(ctx context.Context, tx *sql.Tx, householdID, memberID string) error {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM member WHERE household_id = ? AND id = ?`, householdID, memberID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", ErrMemberNotFound, memberID)
	}
	if err != nil {
		return fmt.Errorf("%w: check member: %v", ErrInternal, err)
	}
	return nil
}
