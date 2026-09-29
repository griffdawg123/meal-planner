package household

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AuthorizeMember checks, before anything is changed, that an actor belonging to householdID may
// act on memberID. A member of another household yields ErrPermissionDenied; a member that does not
// exist at all yields ErrMemberNotFound.
func (s *Service) AuthorizeMember(ctx context.Context, householdID, memberID string) error {
	var memberHouseholdID string
	err := s.database.QueryRowContext(ctx,
		`SELECT household_id FROM member WHERE id = ?`, memberID,
	).Scan(&memberHouseholdID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q", ErrMemberNotFound, memberID)
	}
	if err != nil {
		return fmt.Errorf("%w: check member household: %v", ErrInternal, err)
	}
	if memberHouseholdID != householdID {
		return fmt.Errorf("%w: member %q belongs to another household", ErrPermissionDenied, memberID)
	}
	return nil
}
