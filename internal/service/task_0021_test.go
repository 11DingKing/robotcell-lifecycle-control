package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/service"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

func TestCalibrationIdempotencyIsScopedToRobotCell(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "idempotency.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	integrator, err := db.CreateUser(ctx, identity.User{Username: "integrator", DisplayName: "集成方", PasswordHash: "hash", Role: identity.RoleIntegrator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	station, err := db.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-IDEM", Name: "标定工位", Line: "L1", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	createCell := func(code string) lifecycle.RobotCell {
		cell, e := db.CreateCell(ctx, lifecycle.RobotCell{Code: code, Name: code, WorkstationID: station.ID, IntegratorID: integrator.ID, Status: lifecycle.CellCalibrating})
		if e != nil {
			t.Fatal(e)
		}
		return cell
	}
	firstCell, secondCell := createCell("CELL-A"), createCell("CELL-B")
	principal := identity.Principal{UserID: integrator.ID, Username: integrator.Username, DisplayName: integrator.DisplayName, Role: integrator.Role, SessionID: "integrator"}
	lifecycleService := service.NewLifecycle(db, clock.NewManual(now))
	first, err := lifecycleService.ReportCalibrationFailure(service.ContextWithPrincipal(ctx, principal), firstCell.ID, "controller-timeout", "轴零点漂移", "req-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := lifecycleService.ReportCalibrationFailure(service.ContextWithPrincipal(ctx, principal), secondCell.ID, "controller-timeout", "视觉标定失败", "req-b")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || second.ObjectID != secondCell.ID || first.IdempotencyKey == second.IdempotencyKey {
		t.Fatalf("cross-cell report reused recovery job: first=%#v second=%#v", first, second)
	}
}
