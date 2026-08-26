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

func TestConflictingWindowApprovalDoesNotScheduleRobotCell(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 2, 0, 0, 0, time.UTC)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "scheduling.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	createUser := func(name string, role identity.Role) identity.User {
		user, createErr := db.CreateUser(ctx, identity.User{Username: name, DisplayName: name, PasswordHash: "hash", Role: role, Active: true})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return user
	}
	manager := createUser("manager", identity.RoleLineManager)
	operator := createUser("operator", identity.RoleOperator)
	integrator := createUser("integrator", identity.RoleIntegrator)
	station, err := db.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-23", Name: "总装工位", Line: "L2", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := db.CreateTool(ctx, schedule.Tool{Code: "TL-23", Name: "标定工装", Active: true, CalibrationDue: now.Add(30 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.CreateQualification(ctx, schedule.Qualification{UserID: operator.ID, Kind: "robot_installation", ValidFrom: now.Add(-time.Hour), ValidTo: now.Add(30 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	createCell := func(code string) lifecycle.RobotCell {
		cell, createErr := db.CreateCell(ctx, lifecycle.RobotCell{Code: code, Name: code, WorkstationID: station.ID, IntegratorID: integrator.ID, Status: lifecycle.CellApproved})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return cell
	}
	firstCell, secondCell := createCell("RC-23-A"), createCell("RC-23-B")
	start := now.Add(24 * time.Hour)
	createWindow := func(cell lifecycle.RobotCell, purpose string) schedule.WorkWindow {
		window, createErr := db.CreateWindow(ctx, schedule.WorkWindow{CellID: cell.ID, WorkstationID: station.ID, ToolID: tool.ID, QualifiedUserID: operator.ID, StartsAt: start, EndsAt: start.Add(3 * time.Hour), Status: schedule.WindowRequested, Purpose: purpose})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return window
	}
	first, second := createWindow(firstCell, "first installation"), createWindow(secondCell, "conflicting installation")
	principal := identity.Principal{UserID: manager.ID, Username: manager.Username, Role: manager.Role, SessionID: "session-manager"}
	scheduler := service.NewScheduling(db, clock.NewManual(now))
	if _, err = scheduler.Approve(service.ContextWithPrincipal(ctx, principal), first.ID, first.Version, "robot_installation", "req-23-first"); err != nil {
		t.Fatalf("approve first window: %v", err)
	}
	_, err = scheduler.Approve(service.ContextWithPrincipal(ctx, principal), second.ID, second.Version, "robot_installation", "req-23-second")
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("conflicting approval error=%v, want conflict", err)
	}
	currentCell, err := db.GetCell(ctx, secondCell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentCell.Status != lifecycle.CellApproved || currentCell.Version != secondCell.Version {
		t.Fatalf("failed approval changed robot cell: %#v", currentCell)
	}
	currentWindow, err := db.GetWindow(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	reservations, err := db.CountActiveReservations(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentWindow.Status != schedule.WindowRequested || reservations != 0 {
		t.Fatalf("failed approval leaked window state: window=%#v reservations=%d", currentWindow, reservations)
	}
}
