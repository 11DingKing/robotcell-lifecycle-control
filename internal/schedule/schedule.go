package schedule

import (
	"fmt"
	"strings"
	"time"
)

type WindowStatus string

const (
	WindowRequested WindowStatus = "requested"
	WindowApproved  WindowStatus = "approved"
	WindowReserved  WindowStatus = "reserved"
	WindowActive    WindowStatus = "active"
	WindowCompleted WindowStatus = "completed"
	WindowCancelled WindowStatus = "cancelled"
	WindowExpired   WindowStatus = "expired"
)

var transitions = map[WindowStatus]map[WindowStatus]bool{
	WindowRequested: {WindowApproved: true, WindowCancelled: true},
	WindowApproved:  {WindowReserved: true, WindowCancelled: true},
	WindowReserved:  {WindowActive: true, WindowCancelled: true, WindowExpired: true},
	WindowActive:    {WindowCompleted: true, WindowCancelled: true},
}

type Workstation struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Line      string    `json:"line"`
	Active    bool      `json:"active"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

type Tool struct {
	ID             int64     `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	CalibrationDue time.Time `json:"calibration_due"`
	Active         bool      `json:"active"`
	Version        int64     `json:"version"`
}

type Qualification struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Kind      string     `json:"kind"`
	ValidFrom time.Time  `json:"valid_from"`
	ValidTo   time.Time  `json:"valid_to"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func (q Qualification) ValidAt(at time.Time) bool {
	return q.RevokedAt == nil && !at.Before(q.ValidFrom) && at.Before(q.ValidTo)
}

type WorkWindow struct {
	ID              int64        `json:"id"`
	CellID          int64        `json:"cell_id"`
	WorkstationID   int64        `json:"workstation_id"`
	ToolID          int64        `json:"tool_id"`
	QualifiedUserID int64        `json:"qualified_user_id"`
	StartsAt        time.Time    `json:"starts_at"`
	EndsAt          time.Time    `json:"ends_at"`
	Status          WindowStatus `json:"status"`
	Purpose         string       `json:"purpose"`
	Version         int64        `json:"version"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

func (w WorkWindow) Validate() error {
	if w.CellID <= 0 || w.WorkstationID <= 0 || w.ToolID <= 0 || w.QualifiedUserID <= 0 {
		return fmt.Errorf("window resources are required")
	}
	if !w.StartsAt.Before(w.EndsAt) {
		return fmt.Errorf("window start must precede end")
	}
	if w.EndsAt.Sub(w.StartsAt) > 24*time.Hour {
		return fmt.Errorf("window cannot exceed 24 hours")
	}
	if strings.TrimSpace(w.Purpose) == "" {
		return fmt.Errorf("window purpose is required")
	}
	return nil
}

func (w WorkWindow) CanTransition(next WindowStatus) bool {
	return transitions[w.Status][next]
}

func (w WorkWindow) Overlaps(other WorkWindow) bool {
	return w.StartsAt.Before(other.EndsAt) && other.StartsAt.Before(w.EndsAt)
}

type ResourceKind string

const (
	ResourceWorkstation ResourceKind = "workstation"
	ResourceTool        ResourceKind = "tool"
	ResourcePerson      ResourceKind = "person"
)

type Reservation struct {
	ID           int64        `json:"id"`
	WindowID     int64        `json:"window_id"`
	ResourceKind ResourceKind `json:"resource_kind"`
	ResourceID   int64        `json:"resource_id"`
	StartsAt     time.Time    `json:"starts_at"`
	EndsAt       time.Time    `json:"ends_at"`
	ReleasedAt   *time.Time   `json:"released_at,omitempty"`
}
