package service_test

import (
	"context"
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
)

func TestStaleWindowCancellationKeepsEveryReservation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "window-cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	manager, err := database.CreateUser(ctx, identity.User{Username: "manager", DisplayName: "产线负责人", PasswordHash: "hash", Role: identity.RoleLineManager, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	integrator, err := database.CreateUser(ctx, identity.User{Username: "integrator", DisplayName: "集成方", PasswordHash: "hash", Role: identity.RoleIntegrator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := database.CreateUser(ctx, identity.User{Username: "operator", DisplayName: "操作员", PasswordHash: "hash", Role: identity.RoleOperator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	station, err := database.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-CANCEL", Name: "取消测试工位", Line: "L1", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := database.CreateTool(ctx, schedule.Tool{Code: "TL-CANCEL", Name: "标定工装", Active: true, CalibrationDue: now.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	cell, err := database.CreateCell(ctx, lifecycle.RobotCell{Code: "CELL-CANCEL", Name: "机器人单元", WorkstationID: station.ID, IntegratorID: integrator.ID, Status: lifecycle.CellApproved})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.CreateQualification(ctx, schedule.Qualification{UserID: operator.ID, Kind: "installation", ValidFrom: now.Add(-time.Hour), ValidTo: now.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	window, err := database.CreateWindow(ctx, schedule.WorkWindow{CellID: cell.ID, WorkstationID: station.ID, ToolID: tool.ID, QualifiedUserID: operator.ID, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), Status: schedule.WindowRequested, Purpose: "安装标定"})
	if err != nil {
		t.Fatal(err)
	}
	managerPrincipal := identity.Principal{UserID: manager.ID, Username: manager.Username, DisplayName: manager.DisplayName, Role: manager.Role, SessionID: "manager-session"}
	reserved, err := database.ApproveAndReserveWindow(ctx, managerPrincipal, window.ID, window.Version, "installation", "req-approve", now)
	if err != nil {
		t.Fatal(err)
	}
	before, err := database.CountActiveReservations(ctx, window.ID)
	if err != nil || before != 3 {
		t.Fatalf("active reservations before stale cancellation = %d, err=%v", before, err)
	}
	_, cancelErr := service.NewScheduling(database, clock.NewManual(now)).Cancel(service.ContextWithPrincipal(ctx, managerPrincipal), reserved.ID, window.Version, "现场计划变更", "req-stale-cancel")
	if !errors.Is(cancelErr, apperr.ErrVersion) {
		t.Fatalf("stale cancellation error = %v, want version conflict", cancelErr)
	}
	after, err := database.CountActiveReservations(ctx, window.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetWindow(ctx, window.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after != 3 || stored.Status != schedule.WindowReserved || stored.Version != reserved.Version {
		t.Fatalf("stale cancellation changed ownership: reservations=%d status=%s version=%d", after, stored.Status, stored.Version)
	}
}
