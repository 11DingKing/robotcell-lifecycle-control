package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
)

func (s *Store) CreateUser(ctx context.Context, user identity.User) (identity.User, error) {
	if err := user.Validate(); err != nil {
		return identity.User{}, apperr.Wrap(apperr.ErrInvalid, "store.create_user", "invalid user", err)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO users(username,password_hash,display_name,role,active,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, user.Username, user.PasswordHash, user.DisplayName, user.Role, boolInt(user.Active), encodeTime(now), encodeTime(now))
	if err != nil {
		if isConflict(err) {
			return identity.User{}, apperr.Wrap(apperr.ErrConflict, "store.create_user", "username already exists", err)
		}
		return identity.User{}, fmt.Errorf("insert user: %w", err)
	}
	user.ID, err = result.LastInsertId()
	if err != nil {
		return identity.User{}, fmt.Errorf("read user id: %w", err)
	}
	user.CreatedAt, user.UpdatedAt = now, now
	return user, nil
}

func (s *Store) FindUserByUsername(ctx context.Context, username string) (identity.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,display_name,role,active,created_at,updated_at FROM users WHERE username=?`, username)
	return scanUser(row)
}

func (s *Store) FindUser(ctx context.Context, id int64) (identity.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,display_name,role,active,created_at,updated_at FROM users WHERE id=?`, id)
	return scanUser(row)
}

type rowScanner interface{ Scan(...any) error }

func scanUser(row rowScanner) (identity.User, error) {
	var user identity.User
	var active int
	var created, updated string
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role, &active, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return identity.User{}, apperr.Wrap(apperr.ErrNotFound, "store.scan_user", "user not found", err)
		}
		return identity.User{}, fmt.Errorf("scan user: %w", err)
	}
	var err error
	if user.CreatedAt, err = decodeTime(created); err != nil {
		return identity.User{}, err
	}
	if user.UpdatedAt, err = decodeTime(updated); err != nil {
		return identity.User{}, err
	}
	user.Active = active == 1
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, session identity.Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,expires_at,revoked_at,last_seen_at,created_at) VALUES(?,?,?,?,NULL,?,?)`, session.ID, session.UserID, session.TokenHash, encodeTime(session.ExpiresAt), encodeTime(session.LastSeenAt), encodeTime(session.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (s *Store) ResolveSession(ctx context.Context, tokenHash string, now time.Time) (identity.Principal, error) {
	var principal identity.Principal
	var expires, revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.display_name,u.role,s.id,s.expires_at,s.revoked_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND u.active=1`, tokenHash).Scan(&principal.UserID, &principal.Username, &principal.DisplayName, &principal.Role, &principal.SessionID, &expires, &revoked)
	if err == sql.ErrNoRows {
		return identity.Principal{}, apperr.New(apperr.ErrUnauthenticated, "store.resolve_session", "session is unknown")
	}
	if err != nil {
		return identity.Principal{}, fmt.Errorf("resolve session: %w", err)
	}
	if revoked.Valid {
		return identity.Principal{}, apperr.New(apperr.ErrUnauthenticated, "store.resolve_session", "session was revoked")
	}
	expiresAt, err := decodeTime(expires.String)
	if err != nil {
		return identity.Principal{}, err
	}
	if !now.Before(expiresAt) {
		return identity.Principal{}, apperr.New(apperr.ErrExpired, "store.resolve_session", "session expired")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=? AND revoked_at IS NULL`, encodeTime(now), principal.SessionID); err != nil {
		return identity.Principal{}, fmt.Errorf("touch session: %w", err)
	}
	return principal, nil
}

func (s *Store) RevokeSession(ctx context.Context, sessionID string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, encodeTime(now), sessionID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read revoke result: %w", err)
	}
	if count == 0 {
		return apperr.New(apperr.ErrNotFound, "store.revoke_session", "active session not found")
	}
	return nil
}

func (s *Store) PurgeExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<=? OR (revoked_at IS NOT NULL AND revoked_at<?)`, encodeTime(now), encodeTime(now.Add(-24*time.Hour)))
	if err != nil {
		return 0, fmt.Errorf("purge sessions: %w", err)
	}
	return result.RowsAffected()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
