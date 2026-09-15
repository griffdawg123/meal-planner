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
