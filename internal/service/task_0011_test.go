package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/service"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

func TestMaintenanceStartReservesPartsOnceAndAdvancesAllState(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "maintenance-start.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	maintainer, err := database.CreateUser(ctx, identity.User{Username: "maintainer", DisplayName: "维护人员", PasswordHash: "hash", Role: identity.RoleMaintenance, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	integrator, err := database.CreateUser(ctx, identity.User{Username: "integrator", DisplayName: "集成方", PasswordHash: "hash", Role: identity.RoleIntegrator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	station, err := database.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-MAINT", Name: "维护工位", Line: "L1", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	cell, err := database.CreateCell(ctx, lifecycle.RobotCell{Code: "CELL-MAINT", Name: "机器人单元", WorkstationID: station.ID, IntegratorID: integrator.ID, Status: lifecycle.CellProduction})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.CreateQualification(ctx, schedule.Qualification{UserID: maintainer.ID, Kind: "maintenance", ValidFrom: now.Add(-time.Hour), ValidTo: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	part, err := database.CreateSparePart(ctx, maintenance.SparePart{Code: "SP-EXACT", Name: "减速器", Available: 2})
	if err != nil {
		t.Fatal(err)
	}
	partID := part.ID
	order, err := database.CreateMaintenanceOrder(ctx, maintenance.Order{Code: "MO-START", CellID: cell.ID, AssigneeID: maintainer.ID, SparePartID: &partID, SpareQuantity: 2, Priority: 1, Summary: "更换减速器", Status: maintenance.Approved, DueAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	principal := identity.Principal{UserID: maintainer.ID, Username: maintainer.Username, DisplayName: maintainer.DisplayName, Role: maintainer.Role, SessionID: "session-maintainer"}
	updated, startErr := service.NewMaintenance(database, clock.NewManual(now)).Advance(service.ContextWithPrincipal(ctx, principal), order.ID, order.Version, maintenance.Executing, "req-start-maintenance")
	storedPart, err := database.GetSparePart(ctx, part.ID)
	if err != nil {
		t.Fatal(err)
	}
	if startErr != nil {
		t.Fatalf("start maintenance returned %v and left reserved=%d", startErr, storedPart.Reserved)
	}
	storedCell, err := database.GetCell(ctx, cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != maintenance.Executing || storedPart.Reserved != 2 || storedCell.Status != lifecycle.CellMaintenance {
		t.Fatalf("inconsistent maintenance start: order=%s reserved=%d cell=%s", updated.Status, storedPart.Reserved, storedCell.Status)
	}
}
