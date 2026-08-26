package lifecycle_test

import (
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
)

func TestProductionBatchValidation(t *testing.T) {
	now := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		batch lifecycle.ProductionBatch
		valid bool
	}{
		{
			name: "valid planned batch",
			batch: lifecycle.ProductionBatch{
				Code: "PB-2026-001", Name: "一号产线改造批次", Status: lifecycle.BatchPlanned,
				StartsAt: now.Add(time.Hour), EndsAt: now.Add(8 * time.Hour),
			},
			valid: true,
		},
		{
			name: "missing code",
			batch: lifecycle.ProductionBatch{
				Name: "一号产线改造批次", Status: lifecycle.BatchDraft,
				StartsAt: now, EndsAt: now.Add(time.Hour),
			},
		},
		{
			name: "missing name",
			batch: lifecycle.ProductionBatch{
				Code: "PB-2026-001", Status: lifecycle.BatchDraft,
				StartsAt: now, EndsAt: now.Add(time.Hour),
			},
		},
		{
			name: "zero duration",
			batch: lifecycle.ProductionBatch{
				Code: "PB-2026-001", Name: "一号产线改造批次", Status: lifecycle.BatchDraft,
				StartsAt: now, EndsAt: now,
			},
		},
		{
			name: "reverse duration",
			batch: lifecycle.ProductionBatch{
				Code: "PB-2026-001", Name: "一号产线改造批次", Status: lifecycle.BatchDraft,
				StartsAt: now.Add(time.Hour), EndsAt: now,
			},
		},
		{
			name: "missing status",
			batch: lifecycle.ProductionBatch{
				Code: "PB-2026-001", Name: "一号产线改造批次",
				StartsAt: now, EndsAt: now.Add(time.Hour),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.batch.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected valid batch, got %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestProductionBatchTransitions(t *testing.T) {
	tests := []struct {
		from lifecycle.BatchStatus
		to   lifecycle.BatchStatus
		want bool
	}{
		{lifecycle.BatchDraft, lifecycle.BatchPlanned, true},
		{lifecycle.BatchDraft, lifecycle.BatchCancelled, true},
		{lifecycle.BatchDraft, lifecycle.BatchActive, false},
		{lifecycle.BatchPlanned, lifecycle.BatchActive, true},
		{lifecycle.BatchPlanned, lifecycle.BatchCancelled, true},
		{lifecycle.BatchPlanned, lifecycle.BatchCompleted, false},
		{lifecycle.BatchActive, lifecycle.BatchCompleted, true},
		{lifecycle.BatchActive, lifecycle.BatchCancelled, true},
		{lifecycle.BatchCompleted, lifecycle.BatchActive, false},
		{lifecycle.BatchCancelled, lifecycle.BatchDraft, false},
	}
	for _, test := range tests {
		batch := lifecycle.ProductionBatch{Status: test.from}
		if got := batch.CanTransition(test.to); got != test.want {
			t.Errorf("transition %s -> %s = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestRobotCellValidation(t *testing.T) {
	base := lifecycle.RobotCell{
		Code: "RC-01", Name: "焊接机器人单元", WorkstationID: 4,
		IntegratorID: 9, Status: lifecycle.CellSurveyed,
	}
	tests := []struct {
		name   string
		change func(*lifecycle.RobotCell)
		valid  bool
	}{
		{"valid", func(*lifecycle.RobotCell) {}, true},
		{"blank code", func(cell *lifecycle.RobotCell) { cell.Code = " " }, false},
		{"blank name", func(cell *lifecycle.RobotCell) { cell.Name = "" }, false},
		{"missing workstation", func(cell *lifecycle.RobotCell) { cell.WorkstationID = 0 }, false},
		{"missing integrator", func(cell *lifecycle.RobotCell) { cell.IntegratorID = 0 }, false},
		{"missing status", func(cell *lifecycle.RobotCell) { cell.Status = "" }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cell := base
			test.change(&cell)
			err := cell.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected valid cell: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected invalid cell")
			}
		})
	}
}

func TestRobotCellLifecycleTransitions(t *testing.T) {
	tests := []struct {
		name   string
		cell   lifecycle.RobotCell
		next   lifecycle.CellStatus
		wanted bool
	}{
		{"survey to proposal", lifecycle.RobotCell{Status: lifecycle.CellSurveyed}, lifecycle.CellProposed, true},
		{"proposal approved", lifecycle.RobotCell{Status: lifecycle.CellProposed}, lifecycle.CellApproved, true},
		{"proposal sent back", lifecycle.RobotCell{Status: lifecycle.CellProposed}, lifecycle.CellSurveyed, true},
		{"approved scheduled", lifecycle.RobotCell{Status: lifecycle.CellApproved}, lifecycle.CellScheduled, true},
		{"scheduled installing", lifecycle.RobotCell{Status: lifecycle.CellScheduled}, lifecycle.CellInstalling, true},
		{"installing calibrating", lifecycle.RobotCell{Status: lifecycle.CellInstalling}, lifecycle.CellCalibrating, true},
		{"calibration review", lifecycle.RobotCell{Status: lifecycle.CellCalibrating}, lifecycle.CellSafetyReview, true},
		{"review without evidence", lifecycle.RobotCell{Status: lifecycle.CellSafetyReview}, lifecycle.CellProduction, false},
		{"review missing quality", lifecycle.RobotCell{Status: lifecycle.CellSafetyReview, SafetyPassed: true, CalibrationRef: "CAL-1"}, lifecycle.CellProduction, false},
		{"review missing safety", lifecycle.RobotCell{Status: lifecycle.CellSafetyReview, QualityPassed: true, CalibrationRef: "CAL-1"}, lifecycle.CellProduction, false},
		{"review missing calibration", lifecycle.RobotCell{Status: lifecycle.CellSafetyReview, QualityPassed: true, SafetyPassed: true}, lifecycle.CellProduction, false},
		{"review completed", lifecycle.RobotCell{Status: lifecycle.CellSafetyReview, QualityPassed: true, SafetyPassed: true, CalibrationRef: "CAL-1"}, lifecycle.CellProduction, true},
		{"production maintenance", lifecycle.RobotCell{Status: lifecycle.CellProduction}, lifecycle.CellMaintenance, true},
		{"maintenance returns", lifecycle.RobotCell{Status: lifecycle.CellMaintenance}, lifecycle.CellProduction, true},
		{"production retires", lifecycle.RobotCell{Status: lifecycle.CellProduction}, lifecycle.CellRetired, true},
		{"retired immutable", lifecycle.RobotCell{Status: lifecycle.CellRetired}, lifecycle.CellProduction, false},
		{"skip survey", lifecycle.RobotCell{Status: lifecycle.CellSurveyed}, lifecycle.CellApproved, false},
		{"skip installation", lifecycle.RobotCell{Status: lifecycle.CellScheduled}, lifecycle.CellCalibrating, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.cell.CanTransition(test.next); got != test.wanted {
				t.Fatalf("CanTransition(%s, %s) = %v, want %v", test.cell.Status, test.next, got, test.wanted)
			}
		})
	}
}

func TestNormalizePage(t *testing.T) {
	tests := []struct {
		page, size int
		wantPage   int
		wantSize   int
	}{
		{1, 20, 1, 20},
		{0, 0, 1, 20},
		{-5, -1, 1, 20},
		{2, 1, 2, 1},
		{3, 100, 3, 100},
		{3, 101, 3, 100},
		{999, 5000, 999, 100},
	}
	for _, test := range tests {
		page, size := lifecycle.NormalizePage(test.page, test.size)
		if page != test.wantPage || size != test.wantSize {
			t.Errorf("NormalizePage(%d,%d)=(%d,%d), want (%d,%d)", test.page, test.size, page, size, test.wantPage, test.wantSize)
		}
	}
}
