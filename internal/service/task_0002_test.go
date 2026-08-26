package service_test

import (
	"context"
	"errors"
	"fmt"
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

func TestCancelledCalibrationReportDoesNotCreateRecoveryWork(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "cancel-report.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	integrator, err := database.CreateUser(ctx, identity.User{Username: "integrator", DisplayName: "集成方", PasswordHash: "hash", Role: identity.RoleIntegrator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := database.CreateUser(ctx, identity.User{Username: "manager", DisplayName: "负责人", PasswordHash: "hash", Role: identity.RoleLineManager, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	station, err := database.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-CTX", Name: "标定工位", Line: "L1", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	cell, err := database.CreateCell(ctx, lifecycle.RobotCell{Code: "CELL-CTX", Name: "机器人单元", WorkstationID: station.ID, IntegratorID: integrator.ID, Status: lifecycle.CellSurveyed})
	if err != nil {
		t.Fatal(err)
	}
	actor := func(user identity.User) identity.Principal {
		return identity.Principal{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Role: user.Role, SessionID: "session-" + user.Username}
	}
	for _, step := range []struct {
		next lifecycle.CellStatus
		user identity.User
	}{{lifecycle.CellProposed, integrator}, {lifecycle.CellApproved, manager}, {lifecycle.CellScheduled, manager}, {lifecycle.CellInstalling, integrator}, {lifecycle.CellCalibrating, integrator}} {
		cell, err = database.TransitionCell(ctx, actor(step.user), cell.ID, cell.Version, step.next, "prepare", "prepare-report", now)
		if err != nil {
			t.Fatalf("prepare %s: %v", step.next, err)
		}
	}
	requestCtx, cancel := context.WithCancel(service.ContextWithPrincipal(ctx, actor(integrator)))
	cancel()
	_, err = service.NewLifecycle(database, clock.NewManual(now)).ReportCalibrationFailure(requestCtx, cell.ID, "cancelled-report", "target lost", "report-cancelled")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled report error = %v", err)
	}
	_, err = database.FindRecoveryByKey(ctx, fmt.Sprintf("calibration:%d:cancelled-report", cell.ID))
	if err == nil || !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("cancelled report persisted recovery work: %v", err)
	}
}
