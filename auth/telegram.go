package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	telegramLinkCodeLifetime = 10 * time.Minute
	// telegramLinkCodeAlphabet is Crockford's base32 alphabet: 32 symbols, so each random byte maps
	// to one symbol without bias, and it omits the easily confused I, L, O, and U.
	telegramLinkCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	telegramLinkCodeLength   = 8

	// MaxFailedTelegramLinkAttempts is how many invalid link codes one Telegram user may send within
	// telegramLinkAttemptWindow before further attempts, even with a valid code, are rejected.
	MaxFailedTelegramLinkAttempts = 5
	telegramLinkAttemptWindow     = telegramLinkCodeLifetime
)

var (
	// ErrInvalidTelegramLinkCode identifies an unknown, expired, or already-used Telegram link code.
	ErrInvalidTelegramLinkCode = errors.New("invalid telegram link code")
	// ErrTelegramNotLinked identifies a Telegram user that is not linked to any household member.
	ErrTelegramNotLinked = errors.New("telegram user not linked")
	// ErrTelegramAlreadyLinked identifies a Telegram user that is already linked to another member.
	ErrTelegramAlreadyLinked = errors.New("telegram user already linked to another member")
	// ErrTooManyTelegramLinkAttempts identifies a Telegram user that has sent too many invalid link
	// codes and must wait before trying again.
	ErrTooManyTelegramLinkAttempts = errors.New("too many telegram link attempts")
)

// RequestTelegramLinkCode creates a short-lived, single-use code that links a Telegram account to
// the given household member. The caller must have authenticated that member and is responsible
// for showing them the returned code.
func (s *Service) RequestTelegramLinkCode(ctx context.Context, householdID, memberID string) (string, error) {
	if strings.TrimSpace(householdID) == "" || strings.TrimSpace(memberID) == "" {
		return "", fmt.Errorf("%w: household and member are required", ErrInvalidInput)
	}
	code, err := newTelegramLinkCode()
	if err != nil {
		return "", fmt.Errorf("%w: create telegram link code: %v", ErrInternal, err)
	}
	now := time.Now().UTC()
	result, err := s.database.ExecContext(ctx, `
		INSERT INTO telegram_link_code (code_hash, member_id, created_at, expires_at)
		SELECT ?, id, ?, ? FROM member WHERE household_id = ? AND id = ?
	`, hashToken(normalizeTelegramLinkCode(code)), now.Unix(), now.Add(telegramLinkCodeLifetime).Unix(), householdID, memberID)
	if err != nil {
		return "", fmt.Errorf("%w: create telegram link code: %v", ErrInternal, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("%w: inspect telegram link code creation: %v", ErrInternal, err)
	}
	if rowsAffected == 0 {
		return "", fmt.Errorf("%w: %q", ErrMemberNotFound, memberID)
	}
	return code, nil
}

// LinkTelegram atomically consumes a link code and links telegramUserID to the code's member,
// replacing any Telegram account previously linked to that member. It returns the linked member.
// After MaxFailedTelegramLinkAttempts invalid codes within telegramLinkAttemptWindow, it rejects
// every attempt from telegramUserID with ErrTooManyTelegramLinkAttempts until the window passes.
func (s *Service) LinkTelegram(ctx context.Context, telegramUserID int64, code string) (Principal, error) {
	if telegramUserID <= 0 {
		return Principal{}, fmt.Errorf("%w: telegram user ID must be positive", ErrInvalidInput)
	}
	code = normalizeTelegramLinkCode(code)
	if code == "" {
		return Principal{}, ErrInvalidTelegramLinkCode
	}
	now := time.Now().UTC()

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: begin telegram link: %v", ErrInternal, err)
	}
	defer tx.Rollback()

	// Count this attempt as a failure before checking the code, starting a new window if the last
	// one has passed. The count is committed only if the code turns out to be invalid; a lockout
	// check that rolls back leaves the stored count at its limit.
	windowCutoff := now.Add(-telegramLinkAttemptWindow).Unix()
	var failedAttempts int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO telegram_link_attempt (telegram_user_id, failed_attempts, window_started_at)
		VALUES (?, 1, ?)
		ON CONFLICT (telegram_user_id) DO UPDATE SET
			failed_attempts = CASE WHEN window_started_at <= ? THEN 1 ELSE failed_attempts + 1 END,
			window_started_at = CASE WHEN window_started_at <= ? THEN excluded.window_started_at ELSE window_started_at END
		RETURNING failed_attempts
	`, telegramUserID, now.Unix(), windowCutoff, windowCutoff).Scan(&failedAttempts)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: record telegram link attempt: %v", ErrInternal, err)
	}
	if failedAttempts > MaxFailedTelegramLinkAttempts {
		return Principal{}, ErrTooManyTelegramLinkAttempts
	}

	var memberID string
	err = tx.QueryRowContext(ctx, `
		UPDATE telegram_link_code
		SET used_at = ?
		WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?
		RETURNING member_id
	`, now.Unix(), hashToken(code), now.Unix()).Scan(&memberID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return Principal{}, fmt.Errorf("%w: commit failed telegram link attempt: %v", ErrInternal, err)
		}
		return Principal{}, ErrInvalidTelegramLinkCode
	}
	if err != nil {
		return Principal{}, fmt.Errorf("%w: consume telegram link code: %v", ErrInternal, err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM telegram_link_attempt WHERE telegram_user_id = ?
	`, telegramUserID); err != nil {
		return Principal{}, fmt.Errorf("%w: clear telegram link attempts: %v", ErrInternal, err)
	}

	var linkedMemberID string
	err = tx.QueryRowContext(ctx, `
		SELECT member_id FROM telegram_identity WHERE telegram_user_id = ?
	`, telegramUserID).Scan(&linkedMemberID)
	if err == nil && linkedMemberID != memberID {
		return Principal{}, fmt.Errorf("%w: %d", ErrTelegramAlreadyLinked, telegramUserID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Principal{}, fmt.Errorf("%w: inspect telegram identity: %v", ErrInternal, err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM telegram_identity WHERE member_id = ?
	`, memberID); err != nil {
		return Principal{}, fmt.Errorf("%w: unlink previous telegram identity: %v", ErrInternal, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO telegram_identity (telegram_user_id, member_id, linked_at)
		VALUES (?, ?, ?)
	`, telegramUserID, memberID, now.Unix()); err != nil {
		return Principal{}, fmt.Errorf("%w: link telegram identity: %v", ErrInternal, err)
	}
	principal := Principal{MemberID: memberID}
	if err := tx.QueryRowContext(ctx, `
		SELECT household_id FROM member WHERE id = ?
	`, memberID).Scan(&principal.HouseholdID); err != nil {
		return Principal{}, fmt.Errorf("%w: read linked member: %v", ErrInternal, err)
	}
	if err := tx.Commit(); err != nil {
		return Principal{}, fmt.Errorf("%w: commit telegram link: %v", ErrInternal, err)
	}
	return principal, nil
}

// ResolveTelegram returns the household and member linked to telegramUserID. An unlinked Telegram
// user yields ErrTelegramNotLinked and must not be allowed to act on any household's data.
func (s *Service) ResolveTelegram(ctx context.Context, telegramUserID int64) (Principal, error) {
	if telegramUserID <= 0 {
		return Principal{}, ErrTelegramNotLinked
	}
	var principal Principal
	err := s.database.QueryRowContext(ctx, `
		SELECT member.household_id, member.id
		FROM telegram_identity
		JOIN member ON member.id = telegram_identity.member_id
		WHERE telegram_identity.telegram_user_id = ?
	`, telegramUserID).Scan(&principal.HouseholdID, &principal.MemberID)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrTelegramNotLinked
	}
	if err != nil {
		return Principal{}, fmt.Errorf("%w: resolve telegram identity: %v", ErrInternal, err)
	}
	return principal, nil
}

// UnlinkTelegram removes the link for telegramUserID, as when a linked user asks the bot to unlink.
// Afterwards the Telegram user resolves to no member until they link again with a new code. A
// Telegram user that is not linked yields ErrTelegramNotLinked.
func (s *Service) UnlinkTelegram(ctx context.Context, telegramUserID int64) error {
	if telegramUserID <= 0 {
		return fmt.Errorf("%w: telegram user ID must be positive", ErrInvalidInput)
	}
	result, err := s.database.ExecContext(ctx, `
		DELETE FROM telegram_identity WHERE telegram_user_id = ?
	`, telegramUserID)
	if err != nil {
		return fmt.Errorf("%w: unlink telegram identity: %v", ErrInternal, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: inspect telegram unlink: %v", ErrInternal, err)
	}
	if rowsAffected == 0 {
		return ErrTelegramNotLinked
	}
	return nil
}

// UnlinkMemberTelegram removes the Telegram account linked to the given household member, so a
// member can disconnect an account they no longer control. The caller must have authenticated that
// member. A member outside the household yields ErrMemberNotFound, and a member with no linked
// Telegram account yields ErrTelegramNotLinked.
func (s *Service) UnlinkMemberTelegram(ctx context.Context, householdID, memberID string) error {
	if strings.TrimSpace(householdID) == "" || strings.TrimSpace(memberID) == "" {
		return fmt.Errorf("%w: household and member are required", ErrInvalidInput)
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin telegram unlink: %v", ErrInternal, err)
	}
	defer tx.Rollback()

	var memberExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM member WHERE household_id = ? AND id = ?)
	`, householdID, memberID).Scan(&memberExists); err != nil {
		return fmt.Errorf("%w: inspect member: %v", ErrInternal, err)
	}
	if !memberExists {
		return fmt.Errorf("%w: %q", ErrMemberNotFound, memberID)
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM telegram_identity WHERE member_id = ?
	`, memberID)
	if err != nil {
		return fmt.Errorf("%w: unlink member telegram identity: %v", ErrInternal, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: inspect telegram unlink: %v", ErrInternal, err)
	}
	if rowsAffected == 0 {
		return ErrTelegramNotLinked
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit telegram unlink: %v", ErrInternal, err)
	}
	return nil
}

// TelegramMember is a household member and the Telegram user linked to them. TelegramUserID is zero
// when the member has not linked a Telegram account.
type TelegramMember struct {
	MemberID       string
	TelegramUserID int64
}

// HouseholdTelegramMembers lists every member of the household, ordered by member ID, with the
// Telegram user linked to each, so household messages can reach every linked member. An unknown
// household has no members.
func (s *Service) HouseholdTelegramMembers(ctx context.Context, householdID string) ([]TelegramMember, error) {
	if strings.TrimSpace(householdID) == "" {
		return nil, fmt.Errorf("%w: household is required", ErrInvalidInput)
	}
	rows, err := s.database.QueryContext(ctx, `
		SELECT member.id, coalesce(telegram_identity.telegram_user_id, 0)
		FROM member
		LEFT JOIN telegram_identity ON telegram_identity.member_id = member.id
		WHERE member.household_id = ?
		ORDER BY member.id
	`, householdID)
	if err != nil {
		return nil, fmt.Errorf("%w: list household telegram members: %v", ErrInternal, err)
	}
	defer rows.Close()

	var members []TelegramMember
	for rows.Next() {
		var member TelegramMember
		if err := rows.Scan(&member.MemberID, &member.TelegramUserID); err != nil {
			return nil, fmt.Errorf("%w: read household telegram member: %v", ErrInternal, err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: list household telegram members: %v", ErrInternal, err)
	}
	return members, nil
}

// newTelegramLinkCode returns a code formatted for reading and typing, such as "7K3M-Q9XA".
func newTelegramLinkCode() (string, error) {
	raw := make([]byte, telegramLinkCodeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var code strings.Builder
	for i, b := range raw {
		if i == telegramLinkCodeLength/2 {
			code.WriteByte('-')
		}
		code.WriteByte(telegramLinkCodeAlphabet[int(b)%len(telegramLinkCodeAlphabet)])
	}
	return code.String(), nil
}

// normalizeTelegramLinkCode strips whitespace and separators and upper-cases a typed code so that
// equivalent spellings hash identically.
func normalizeTelegramLinkCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		default:
			return r
		}
	}, code)
}
