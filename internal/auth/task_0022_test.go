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

func TestFailedLogoutDoesNotRecordSuccessfulRevocation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "logout.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(ctx, identity.User{Username: "operator", DisplayName: "操作员", PasswordHash: "hash", Role: identity.RoleOperator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	principal := identity.Principal{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Role: user.Role, SessionID: "missing-session"}
	err = auth.New(db, clock.NewManual(now), time.Hour).Logout(ctx, principal)
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("logout error=%v, want missing active session", err)
	}
	events, err := db.ListAudit(ctx, "session", principal.SessionID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("failed logout recorded success audit: %#v", events)
	}
}
