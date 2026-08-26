package identity_test

import (
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
)

func TestParseRole(t *testing.T) {
	valid := []identity.Role{
		identity.RoleLineManager,
		identity.RoleOperator,
		identity.RoleSafetyOfficer,
		identity.RoleQualityEngineer,
		identity.RoleMaintenance,
		identity.RoleIntegrator,
	}
	for _, role := range valid {
		got, err := identity.ParseRole(" " + string(role) + " ")
		if err != nil {
			t.Errorf("ParseRole(%q): %v", role, err)
		}
		if got != role {
			t.Errorf("ParseRole(%q)=%q", role, got)
		}
	}
	invalid := []string{"", "admin", "manager", "root", "LINE_MANAGER", "operator " + string(identity.RoleIntegrator)}
	for _, value := range invalid {
		if _, err := identity.ParseRole(value); err == nil {
			t.Errorf("ParseRole(%q) should fail", value)
		}
	}
}

func TestUserValidation(t *testing.T) {
	base := identity.User{Username: "line.manager", DisplayName: "产线负责人", Role: identity.RoleLineManager, Active: true}
	tests := []struct {
		name   string
		change func(*identity.User)
		valid  bool
	}{
		{"valid", func(*identity.User) {}, true},
		{"empty username", func(u *identity.User) { u.Username = "" }, false},
		{"blank username", func(u *identity.User) { u.Username = " " }, false},
		{"username space", func(u *identity.User) { u.Username = "line manager" }, false},
		{"username tab", func(u *identity.User) { u.Username = "line\tmanager" }, false},
		{"username newline", func(u *identity.User) { u.Username = "line\nmanager" }, false},
		{"empty display name", func(u *identity.User) { u.DisplayName = "" }, false},
		{"blank display name", func(u *identity.User) { u.DisplayName = "  " }, false},
		{"unknown role", func(u *identity.User) { u.Role = "admin" }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user := base
			test.change(&user)
			err := user.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected valid user: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected invalid user")
			}
		})
	}
}

func TestSessionUsableAtHonorsRevocationAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	session := identity.Session{ExpiresAt: now.Add(time.Hour)}
	if !session.UsableAt(now) {
		t.Fatal("fresh session should be usable")
	}
	if !session.UsableAt(now.Add(time.Hour - time.Nanosecond)) {
		t.Fatal("session should be usable before expiration")
	}
	if session.UsableAt(now.Add(time.Hour)) {
		t.Fatal("expiration is exclusive")
	}
	revoked := now.Add(time.Minute)
	session.RevokedAt = &revoked
	if session.UsableAt(now) {
		t.Fatal("revoked session should not be usable")
	}
}

func TestPrincipalRoleMembership(t *testing.T) {
	principal := identity.Principal{Role: identity.RoleSafetyOfficer}
	if !principal.HasAny(identity.RoleSafetyOfficer) {
		t.Fatal("principal should match its role")
	}
	if !principal.HasAny(identity.RoleOperator, identity.RoleSafetyOfficer, identity.RoleLineManager) {
		t.Fatal("principal should match one role in list")
	}
	if principal.HasAny(identity.RoleOperator, identity.RoleLineManager) {
		t.Fatal("principal should not match unrelated roles")
	}
	if principal.HasAny() {
		t.Fatal("empty allowed set should reject")
	}
}
