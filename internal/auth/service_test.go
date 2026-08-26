package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/auth"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

func authFixture(t *testing.T) (*store.Store, *clock.Manual, *auth.Service, identity.User) {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	password := "correct-password-2026"
	hash, err := auth.PasswordHash(password)
	if err != nil {
		t.Fatal(err)
	}
	user, err := database.CreateUser(ctx, identity.User{Username: "operator", DisplayName: "现场操作员", Role: identity.RoleOperator, Active: true, PasswordHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	manual := clock.NewManual(time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))
	return database, manual, auth.New(database, manual, time.Hour), user
}

func TestPasswordHashValidationAndBcryptStorage(t *testing.T) {
	invalid := []string{"", "short", "12345678901"}
	for _, password := range invalid {
		if _, err := auth.PasswordHash(password); !errors.Is(err, apperr.ErrInvalid) {
			t.Errorf("PasswordHash(%q) error = %v", password, err)
		}
	}
	tooLong := make([]byte, 129)
	for index := range tooLong {
		tooLong[index] = 'a'
	}
	if _, err := auth.PasswordHash(string(tooLong)); !errors.Is(err, apperr.ErrInvalid) {
		t.Errorf("long password error = %v", err)
	}
	hash, err := auth.PasswordHash("valid-password-2026")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "valid-password-2026" || len(hash) < 40 {
		t.Fatalf("password was not stored as a strong hash: %q", hash)
	}
}

func TestLoginAuthenticateLogoutLifecycle(t *testing.T) {
	_, manual, service, user := authFixture(t)
	ctx := context.Background()
	result, err := service.Login(ctx, user.Username, "correct-password-2026")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.Token == "" || result.Principal.UserID != user.ID || result.Principal.Role != identity.RoleOperator {
		t.Fatalf("unexpected login result: %#v", result)
	}
	if !result.ExpiresAt.Equal(manual.Now().Add(time.Hour)) {
		t.Fatalf("expiry = %v", result.ExpiresAt)
	}
	resolved, err := service.Authenticate(ctx, result.Token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if resolved.SessionID != result.Principal.SessionID || resolved.UserID != user.ID {
		t.Fatalf("resolved principal = %#v", resolved)
	}
	if err = service.Logout(ctx, resolved); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err = service.Authenticate(ctx, result.Token); !errors.Is(err, apperr.ErrUnauthenticated) {
		t.Fatalf("revoked token error = %v", err)
	}
}

func TestLoginRejectsUnknownUserAndWrongPassword(t *testing.T) {
	_, _, service, user := authFixture(t)
	ctx := context.Background()
	for _, input := range []struct{ username, password string }{
		{"missing", "correct-password-2026"},
		{user.Username, "wrong-password-2026"},
		{"", ""},
	} {
		if _, err := service.Login(ctx, input.username, input.password); !errors.Is(err, apperr.ErrUnauthenticated) {
			t.Errorf("login(%q) error = %v", input.username, err)
		}
	}
}

func TestSessionExpiresAtConfiguredBoundary(t *testing.T) {
	_, manual, service, user := authFixture(t)
	ctx := context.Background()
	result, err := service.Login(ctx, user.Username, "correct-password-2026")
	if err != nil {
		t.Fatal(err)
	}
	manual.Advance(time.Hour - time.Nanosecond)
	if _, err = service.Authenticate(ctx, result.Token); err != nil {
		t.Fatalf("session should remain valid before deadline: %v", err)
	}
	manual.Advance(time.Nanosecond)
	if _, err = service.Authenticate(ctx, result.Token); !errors.Is(err, apperr.ErrExpired) {
		t.Fatalf("expired session error = %v", err)
	}
}

func TestTokenHashIsDeterministicAndDoesNotExposeToken(t *testing.T) {
	first := auth.HashToken("secret-token")
	second := auth.HashToken("secret-token")
	different := auth.HashToken("another-token")
	if first != second {
		t.Fatal("same token must have stable hash")
	}
	if first == different {
		t.Fatal("different tokens must not have same hash")
	}
	if first == "secret-token" || len(first) != 64 {
		t.Fatalf("unexpected token hash %q", first)
	}
}
