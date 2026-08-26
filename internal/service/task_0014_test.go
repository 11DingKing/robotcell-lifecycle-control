package service_test

import (
	"context"
	"errors"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/service"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func TestRetirementRejectsActiveReservationWithoutReleasingIt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 11, 0, 0, 0, time.UTC)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "retire.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mk := func(name string, role identity.Role) identity.User {
		u, e := db.CreateUser(ctx, identity.User{Username: name, DisplayName: name, PasswordHash: "hash", Role: role, Active: true})
		if e != nil {
			t.Fatal(e)
		}
		return u
	}
	manager := mk("manager", identity.RoleLineManager)
	integrator := mk("integrator", identity.RoleIntegrator)
	operator := mk("operator", identity.RoleOperator)
	st, e := db.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-R", Name: "退役工位", Line: "L1", Active: true})
	if e != nil {
		t.Fatal(e)
	}
	tool, e := db.CreateTool(ctx, schedule.Tool{Code: "TL-R", Name: "工装", Active: true, CalibrationDue: now.Add(24 * time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	cell, e := db.CreateCell(ctx, lifecycle.RobotCell{Code: "CELL-R", Name: "退役单元", WorkstationID: st.ID, IntegratorID: integrator.ID, Status: lifecycle.CellProduction})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.CreateQualification(ctx, schedule.Qualification{UserID: operator.ID, Kind: "installation", ValidFrom: now.Add(-time.Hour), ValidTo: now.Add(24 * time.Hour)}); e != nil {
		t.Fatal(e)
	}
	w, e := db.CreateWindow(ctx, schedule.WorkWindow{CellID: cell.ID, WorkstationID: st.ID, ToolID: tool.ID, QualifiedUserID: operator.ID, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), Status: schedule.WindowRequested, Purpose: "退役前作业"})
	if e != nil {
		t.Fatal(e)
	}
	p := identity.Principal{UserID: manager.ID, Username: manager.Username, DisplayName: manager.DisplayName, Role: manager.Role, SessionID: "manager"}
	w, e = db.ApproveAndReserveWindow(ctx, p, w.ID, w.Version, "installation", "approve", now)
	if e != nil {
		t.Fatal(e)
	}
	_, e = service.NewLifecycle(db, clock.NewManual(now)).Retire(service.ContextWithPrincipal(ctx, p), cell.ID, cell.Version, "retire")
	if !errors.Is(e, apperr.ErrConflict) {
		t.Fatalf("retire error=%v", e)
	}
	got, e := db.CountActiveReservations(ctx, w.ID)
	if e != nil {
		t.Fatal(e)
	}
	stored, e := db.GetCell(ctx, cell.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got != 3 || stored.Status != lifecycle.CellProduction {
		t.Fatalf("retirement released state: reservations=%d status=%s", got, stored.Status)
	}
}
