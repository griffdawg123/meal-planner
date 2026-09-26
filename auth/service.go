// Package auth authenticates web users with single-use magic links and bearer sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

const (
	magicLinkLifetime = 15 * time.Minute
	sessionLifetime   = 30 * 24 * time.Hour
)

var (
	// ErrInvalidInput identifies malformed input that callers can safely report.
	ErrInvalidInput = errors.New("invalid auth input")
	// ErrIdentityNotFound identifies an email that is not linked to a member.
	ErrIdentityNotFound = errors.New("web identity not found")
	// ErrMemberNotFound identifies a member that does not belong to the given household.
	ErrMemberNotFound = errors.New("auth member not found")
	// ErrInvalidMagicLink identifies an unknown, expired, or already-used magic link.
	ErrInvalidMagicLink = errors.New("invalid magic link")
	// ErrInvalidSession identifies an unknown or expired web session.
	ErrInvalidSession = errors.New("invalid web session")
	// ErrInternal identifies an auth storage or token-generation failure.
	ErrInternal = errors.New("auth internal error")
)

// Service provides web authentication operations backed by a database.
type Service struct {
	database *sql.DB
}

// Session is a newly issued bearer session.
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// Principal identifies the household member authenticated by a session.
type Principal struct {
	HouseholdID string
	MemberID    string
}

// NewService creates an authentication service backed by database.
func NewService(database *sql.DB) *Service {
	return &Service{database: database}
}

// LinkEmail associates one normalized email address with a household member.
func (s *Service) LinkEmail(ctx context.Context, householdID, memberID, email string) error {
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	if strings.TrimSpace(householdID) == "" || strings.TrimSpace(memberID) == "" {
		return fmt.Errorf("%w: household and member are required", ErrInvalidInput)
	}

	result, err := s.database.ExecContext(ctx, `
		INSERT INTO web_identity (email, member_id)
		SELECT ?, id FROM member WHERE household_id = ? AND id = ?
	`, email, householdID, memberID)
	if err != nil {
		return fmt.Errorf("%w: link email: %v", ErrInternal, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: inspect email link: %v", ErrInternal, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: %q", ErrMemberNotFound, memberID)
	}
	return nil
}

// RequestMagicLink creates a short-lived bearer token for the member linked to email. The caller
// is responsible for delivering the returned token to that email address.
func (s *Service) RequestMagicLink(ctx context.Context, email string) (string, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return "", err
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return "", fmt.Errorf("%w: create magic-link token: %v", ErrInternal, err)
	}
	now := time.Now().UTC()
	result, err := s.database.ExecContext(ctx, `
		INSERT INTO magic_link (token_hash, member_id, created_at, expires_at)
		SELECT ?, member_id, ?, ? FROM web_identity WHERE email = ?
	`, tokenHash, now.Unix(), now.Add(magicLinkLifetime).Unix(), email)
	if err != nil {
		return "", fmt.Errorf("%w: create magic link: %v", ErrInternal, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("%w: inspect magic-link creation: %v", ErrInternal, err)
	}
	if rowsAffected == 0 {
		return "", fmt.Errorf("%w: %q", ErrIdentityNotFound, email)
	}
	return token, nil
}

// VerifyMagicLink atomically consumes a magic link and returns a new authenticated session.
func (s *Service) VerifyMagicLink(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrInvalidMagicLink
	}
	sessionToken, sessionHash, err := newToken()
	if err != nil {
		return Session{}, fmt.Errorf("%w: create session token: %v", ErrInternal, err)
	}
	now := time.Now().UTC()
	expiresAt := time.Unix(now.Add(sessionLifetime).Unix(), 0).UTC()

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("%w: begin verification: %v", ErrInternal, err)
	}
	defer tx.Rollback()

	var memberID string
	err = tx.QueryRowContext(ctx, `
		UPDATE magic_link
		SET used_at = ?
		WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?
		RETURNING member_id
	`, now.Unix(), hashToken(token), now.Unix()).Scan(&memberID)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrInvalidMagicLink
	}
	if err != nil {
		return Session{}, fmt.Errorf("%w: consume magic link: %v", ErrInternal, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO web_session (token_hash, member_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, sessionHash, memberID, now.Unix(), expiresAt.Unix()); err != nil {
		return Session{}, fmt.Errorf("%w: create session: %v", ErrInternal, err)
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("%w: commit verification: %v", ErrInternal, err)
	}

	return Session{Token: sessionToken, ExpiresAt: expiresAt}, nil
}

// ResolveSession returns the household and member authenticated by an unexpired session token.
func (s *Service) ResolveSession(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrInvalidSession
	}
	var principal Principal
	err := s.database.QueryRowContext(ctx, `
		SELECT member.household_id, member.id
		FROM web_session
		JOIN member ON member.id = web_session.member_id
		WHERE web_session.token_hash = ? AND web_session.expires_at > ?
	`, hashToken(token), time.Now().UTC().Unix()).Scan(&principal.HouseholdID, &principal.MemberID)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrInvalidSession
	}
	if err != nil {
		return Principal{}, fmt.Errorf("%w: resolve session: %v", ErrInternal, err)
	}
	return principal, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	if err != nil || !strings.EqualFold(address.Address, value) {
		return "", fmt.Errorf("%w: valid email is required", ErrInvalidInput)
	}
	return strings.ToLower(address.Address), nil
}

func newToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}
