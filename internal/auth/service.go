package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

type Service struct {
	store *store.Store
	clock clock.Clock
	ttl   time.Duration
}

func New(serviceStore *store.Store, serviceClock clock.Clock, ttl time.Duration) *Service {
	return &Service{store: serviceStore, clock: serviceClock, ttl: ttl}
}

type LoginResult struct {
	Token     string             `json:"token"`
	ExpiresAt time.Time          `json:"expires_at"`
	Principal identity.Principal `json:"principal"`
}

func PasswordHash(password string) (string, error) {
	if len(password) < 12 || len(password) > 128 {
		return "", apperr.New(apperr.ErrInvalid, "auth.password_hash", "password must contain 12 to 128 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func (s *Service) Login(ctx context.Context, username, password string) (LoginResult, error) {
	user, err := s.store.FindUserByUsername(ctx, username)
	if err != nil {
		return LoginResult{}, apperr.New(apperr.ErrUnauthenticated, "auth.login", "invalid credentials")
	}
	if !user.Active || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return LoginResult{}, apperr.New(apperr.ErrUnauthenticated, "auth.login", "invalid credentials")
	}
	token, err := randomToken(32)
	if err != nil {
		return LoginResult{}, err
	}
	sessionID, err := randomToken(18)
	if err != nil {
		return LoginResult{}, err
	}
	now := s.clock.Now()
	session := identity.Session{ID: sessionID, UserID: user.ID, TokenHash: HashToken(token), CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.ttl)}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return LoginResult{}, fmt.Errorf("persist session: %w", err)
	}
	principal := identity.Principal{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Role: user.Role, SessionID: sessionID}
	return LoginResult{Token: token, ExpiresAt: session.ExpiresAt, Principal: principal}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (identity.Principal, error) {
	if token == "" {
		return identity.Principal{}, apperr.New(apperr.ErrUnauthenticated, "auth.authenticate", "missing bearer token")
	}
	principal, err := s.store.ResolveSession(ctx, HashToken(token), s.clock.Now())
	if err != nil {
		return identity.Principal{}, err
	}
	return principal, nil
}

func (s *Service) Logout(ctx context.Context, principal identity.Principal) error {
	return s.store.RevokeSession(ctx, principal.SessionID, s.clock.Now())
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
