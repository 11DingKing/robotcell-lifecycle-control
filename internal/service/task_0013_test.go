package service_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/service"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
	_ "modernc.org/sqlite"
)

func TestRejectedInspectionLeavesNoEvidenceOrCellMutation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	databasePath := filepath.Join(t.TempDir(), "rejected-inspection.db")
	database, err := store.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	safety, err := database.CreateUser(ctx, identity.User{Username: "safety", DisplayName: "安全员", PasswordHash: "hash", Role: identity.RoleSafetyOfficer, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	integrator, err := database.CreateUser(ctx, identity.User{Username: "integrator", DisplayName: "集成方", PasswordHash: "hash", Role: identity.RoleIntegrator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	station, err := database.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-INSPECT", Name: "验收工位", Line: "L2", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	cell, err := database.CreateCell(ctx, lifecycle.RobotCell{Code: "CELL-EARLY", Name: "未到验收阶段单元", WorkstationID: station.ID, IntegratorID: integrator.ID, Status: lifecycle.CellSurveyed})
	if err != nil {
		t.Fatal(err)
	}
	principal := identity.Principal{UserID: safety.ID, Username: safety.Username, DisplayName: safety.DisplayName, Role: safety.Role, SessionID: "safety-session"}
	_, inspectErr := service.NewLifecycle(database, clock.NewManual(now)).RecordInspection(service.ContextWithPrincipal(ctx, principal), lifecycle.Inspection{CellID: cell.ID, Kind: lifecycle.InspectionSafety, Passed: true, Notes: "围栏联锁通过"}, "req-early-inspection")
	if !errors.Is(inspectErr, apperr.ErrInvalid) {
		t.Fatalf("early inspection error = %v, want invalid state", inspectErr)
	}
	raw, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var evidenceCount int
	if err = raw.QueryRowContext(ctx, `SELECT COUNT(*) FROM inspections WHERE cell_id=?`, cell.ID).Scan(&evidenceCount); err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetCell(ctx, cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := database.ListAudit(ctx, "robot_cell", "1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if evidenceCount != 0 || stored.Version != cell.Version || stored.SafetyPassed || len(events) != 0 {
		t.Fatalf("rejected inspection leaked state: evidence=%d version=%d safety=%v audits=%d", evidenceCount, stored.Version, stored.SafetyPassed, len(events))
	}
}
