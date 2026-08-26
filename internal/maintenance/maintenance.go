package maintenance

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	Opened    Status = "opened"
	Triaged   Status = "triaged"
	Approved  Status = "approved"
	Executing Status = "executing"
	Verifying Status = "verifying"
	Closed    Status = "closed"
	Cancelled Status = "cancelled"
)

var transitions = map[Status]map[Status]bool{
	Opened:    {Triaged: true, Cancelled: true},
	Triaged:   {Approved: true, Cancelled: true},
	Approved:  {Executing: true, Cancelled: true},
	Executing: {Verifying: true, Cancelled: true},
	Verifying: {Closed: true, Executing: true},
}

type Order struct {
	ID            int64     `json:"id"`
	Code          string    `json:"code"`
	CellID        int64     `json:"cell_id"`
	AssigneeID    int64     `json:"assignee_id"`
	SparePartID   *int64    `json:"spare_part_id,omitempty"`
	SpareQuantity int       `json:"spare_quantity"`
	Priority      int       `json:"priority"`
	Summary       string    `json:"summary"`
	Status        Status    `json:"status"`
	Version       int64     `json:"version"`
	DueAt         time.Time `json:"due_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (o Order) Validate() error {
	if strings.TrimSpace(o.Code) == "" || strings.TrimSpace(o.Summary) == "" {
		return fmt.Errorf("maintenance code and summary are required")
	}
	if o.CellID <= 0 || o.AssigneeID <= 0 {
		return fmt.Errorf("cell and assignee are required")
	}
	if o.Priority < 1 || o.Priority > 5 {
		return fmt.Errorf("priority must be between 1 and 5")
	}
	if o.SparePartID == nil && o.SpareQuantity != 0 {
		return fmt.Errorf("spare quantity requires a spare part")
	}
	if o.SparePartID != nil && o.SpareQuantity <= 0 {
		return fmt.Errorf("spare quantity must be positive")
	}
	return nil
}

func (o Order) CanTransition(next Status) bool { return transitions[o.Status][next] }

type SparePart struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Available int       `json:"available"`
	Reserved  int       `json:"reserved"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s SparePart) CanReserve(quantity int) bool {
	return quantity > 0 && s.Available-s.Reserved >= quantity
}

type PartMovement struct {
	ID        int64     `json:"id"`
	PartID    int64     `json:"part_id"`
	OrderID   int64     `json:"order_id"`
	Quantity  int       `json:"quantity"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}
