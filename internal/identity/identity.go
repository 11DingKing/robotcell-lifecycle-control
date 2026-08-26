package identity

import (
	"fmt"
	"strings"
	"time"
)

type Role string

const (
	RoleLineManager     Role = "line_manager"
	RoleOperator        Role = "operator"
	RoleSafetyOfficer   Role = "safety_officer"
	RoleQualityEngineer Role = "quality_engineer"
	RoleMaintenance     Role = "maintenance_engineer"
	RoleIntegrator      Role = "integrator"
)

var roles = map[Role]bool{
	RoleLineManager: true, RoleOperator: true, RoleSafetyOfficer: true,
	RoleQualityEngineer: true, RoleMaintenance: true, RoleIntegrator: true,
}

func ParseRole(value string) (Role, error) {
	r := Role(strings.TrimSpace(value))
	if !roles[r] {
		return "", fmt.Errorf("unknown role %q", value)
	}
	return r, nil
}

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"display_name"`
	Role         Role      `json:"role"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (u User) Validate() error {
	if strings.TrimSpace(u.Username) == "" || strings.ContainsAny(u.Username, " \t\r\n") {
		return fmt.Errorf("username is required and cannot contain whitespace")
	}
	if strings.TrimSpace(u.DisplayName) == "" {
		return fmt.Errorf("display name is required")
	}
	if !roles[u.Role] {
		return fmt.Errorf("role is invalid")
	}
	return nil
}

type Session struct {
	ID         string     `json:"id"`
	UserID     int64      `json:"user_id"`
	TokenHash  string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (s Session) UsableAt(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

type Principal struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        Role   `json:"role"`
	SessionID   string `json:"session_id"`
}

func (p Principal) HasAny(allowed ...Role) bool {
	for _, role := range allowed {
		if p.Role == role {
			return true
		}
	}
	return false
}
