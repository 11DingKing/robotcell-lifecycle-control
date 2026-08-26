package lifecycle

import (
	"fmt"
	"strings"
	"time"
)

type BatchStatus string

const (
	BatchDraft     BatchStatus = "draft"
	BatchPlanned   BatchStatus = "planned"
	BatchActive    BatchStatus = "active"
	BatchCompleted BatchStatus = "completed"
	BatchCancelled BatchStatus = "cancelled"
)

var batchTransitions = map[BatchStatus]map[BatchStatus]bool{
	BatchDraft:   {BatchPlanned: true, BatchCancelled: true},
	BatchPlanned: {BatchActive: true, BatchCancelled: true},
	BatchActive:  {BatchCompleted: true, BatchCancelled: true},
}

type ProductionBatch struct {
	ID        int64       `json:"id"`
	Code      string      `json:"code"`
	Name      string      `json:"name"`
	Status    BatchStatus `json:"status"`
	StartsAt  time.Time   `json:"starts_at"`
	EndsAt    time.Time   `json:"ends_at"`
	Version   int64       `json:"version"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

func (b ProductionBatch) Validate() error {
	if strings.TrimSpace(b.Code) == "" || strings.TrimSpace(b.Name) == "" {
		return fmt.Errorf("batch code and name are required")
	}
	if !b.StartsAt.Before(b.EndsAt) {
		return fmt.Errorf("batch start must precede end")
	}
	if b.Status == "" {
		return fmt.Errorf("batch status is required")
	}
	return nil
}

func (b ProductionBatch) CanTransition(next BatchStatus) bool {
	return batchTransitions[b.Status][next]
}

type CellStatus string

const (
	CellSurveyed     CellStatus = "surveyed"
	CellProposed     CellStatus = "proposed"
	CellApproved     CellStatus = "approved"
	CellScheduled    CellStatus = "scheduled"
	CellInstalling   CellStatus = "installing"
	CellCalibrating  CellStatus = "calibrating"
	CellSafetyReview CellStatus = "safety_review"
	CellProduction   CellStatus = "production"
	CellMaintenance  CellStatus = "maintenance"
	CellRetired      CellStatus = "retired"
)

var cellTransitions = map[CellStatus]map[CellStatus]bool{
	CellSurveyed:     {CellProposed: true},
	CellProposed:     {CellApproved: true, CellSurveyed: true},
	CellApproved:     {CellScheduled: true},
	CellScheduled:    {CellInstalling: true, CellApproved: true},
	CellInstalling:   {CellCalibrating: true, CellScheduled: true},
	CellCalibrating:  {CellSafetyReview: true, CellInstalling: true},
	CellSafetyReview: {CellProduction: true, CellCalibrating: true},
	CellProduction:   {CellMaintenance: true, CellRetired: true},
	CellMaintenance:  {CellProduction: true, CellRetired: true},
}

type RobotCell struct {
	ID             int64      `json:"id"`
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	BatchID        *int64     `json:"batch_id,omitempty"`
	WorkstationID  int64      `json:"workstation_id"`
	IntegratorID   int64      `json:"integrator_id"`
	Status         CellStatus `json:"status"`
	SafetyPassed   bool       `json:"safety_passed"`
	QualityPassed  bool       `json:"quality_passed"`
	CalibrationRef string     `json:"calibration_ref"`
	Version        int64      `json:"version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (c RobotCell) Validate() error {
	if strings.TrimSpace(c.Code) == "" || strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("cell code and name are required")
	}
	if c.WorkstationID <= 0 || c.IntegratorID <= 0 {
		return fmt.Errorf("cell workstation and integrator are required")
	}
	if c.Status == "" {
		return fmt.Errorf("cell status is required")
	}
	return nil
}

func (c RobotCell) CanTransition(next CellStatus) bool {
	if !cellTransitions[c.Status][next] {
		return false
	}
	if c.Status == CellSafetyReview && next == CellProduction {
		return c.SafetyPassed && c.QualityPassed && c.CalibrationRef != ""
	}
	return true
}

type InspectionKind string

const (
	InspectionSafety  InspectionKind = "safety"
	InspectionQuality InspectionKind = "quality"
)

type Inspection struct {
	ID          int64          `json:"id"`
	CellID      int64          `json:"cell_id"`
	Kind        InspectionKind `json:"kind"`
	Passed      bool           `json:"passed"`
	InspectorID int64          `json:"inspector_id"`
	Notes       string         `json:"notes"`
	RecordedAt  time.Time      `json:"recorded_at"`
}

type Transition struct {
	CellID     int64      `json:"cell_id"`
	From       CellStatus `json:"from"`
	To         CellStatus `json:"to"`
	ActorID    int64      `json:"actor_id"`
	Reason     string     `json:"reason"`
	RequestID  string     `json:"request_id"`
	OccurredAt time.Time  `json:"occurred_at"`
}

type Page struct {
	Items    []RobotCell `json:"items"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

func NormalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
