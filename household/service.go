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
