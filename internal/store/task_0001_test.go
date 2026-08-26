package store_test

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

func TestRejectedWindowCancellationKeepsResourceOwnership(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "window-cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	manager, err := database.CreateUser(ctx, identity.User{Username: "manager", DisplayName: "产线负责人", PasswordHash: "hash", Role: identity.RoleLineManager, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := database.CreateUser(ctx, identity.User{Username: "operator", DisplayName: "安装操作员", PasswordHash: "hash", Role: identity.RoleOperator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	integrator, err := database.CreateUser(ctx, identity.User{Username: "integrator", DisplayName: "外部集成方", PasswordHash: "hash", Role: identity.RoleIntegrator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	station, err := database.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-CANCEL", Name: "焊装工位", Line: "L1", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := database.CreateTool(ctx, schedule.Tool{Code: "TOOL-CANCEL", Name: "安装工装", Active: true, CalibrationDue: now.Add(30 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	cell, err := database.CreateCell(ctx, lifecycle.RobotCell{Code: "CELL-CANCEL", Name: "机器人单元", WorkstationID: station.ID, IntegratorID: integrator.ID, Status: lifecycle.CellSurveyed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.CreateQualification(ctx, schedule.Qualification{UserID: operator.ID, Kind: "robot_installation", ValidFrom: now.Add(-time.Hour), ValidTo: now.Add(30 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	start := now.Add(24 * time.Hour)
	reserved, err := database.CreateWindow(ctx, schedule.WorkWindow{CellID: cell.ID, WorkstationID: station.ID, ToolID: tool.ID, QualifiedUserID: operator.ID, StartsAt: start, EndsAt: start.Add(2 * time.Hour), Status: schedule.WindowRequested, Purpose: "安装机器人单元"})
	if err != nil {
		t.Fatal(err)
	}
	principal := identity.Principal{UserID: manager.ID, Username: manager.Username, DisplayName: manager.DisplayName, Role: manager.Role, SessionID: "session-manager"}
	reserved, err = database.ApproveAndReserveWindow(ctx, principal, reserved.ID, reserved.Version, "robot_installation", "reserve-first", now)
	if err != nil {
		t.Fatal(err)
	}

	scheduler := service.NewScheduling(database, clock.NewManual(now))
	requestCtx := service.ContextWithPrincipal(ctx, principal)
	_, err = scheduler.Cancel(requestCtx, reserved.ID, reserved.Version-1, "现场计划变更", "cancel-stale")
	if !errors.Is(err, apperr.ErrVersion) {
		t.Fatalf("stale cancellation error = %v", err)
	}
	active, err := database.CountActiveReservations(ctx, reserved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active != 3 {
		t.Fatalf("rejected cancellation released resource ownership: active=%d", active)
	}
	current, err := database.GetWindow(ctx, reserved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != schedule.WindowReserved || current.Version != reserved.Version {
		t.Fatalf("rejected cancellation mutated window: %#v", current)
	}

	overlap, err := database.CreateWindow(ctx, schedule.WorkWindow{CellID: cell.ID, WorkstationID: station.ID, ToolID: tool.ID, QualifiedUserID: operator.ID, StartsAt: start.Add(30 * time.Minute), EndsAt: start.Add(90 * time.Minute), Status: schedule.WindowRequested, Purpose: "冲突停机作业"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.ApproveAndReserveWindow(ctx, principal, overlap.ID, overlap.Version, "robot_installation", "reserve-overlap", now); !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("overlapping window should remain blocked, got %v", err)
	}
}
