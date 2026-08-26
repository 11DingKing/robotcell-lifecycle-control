package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/audit"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
)

type fixture struct {
	store         *Store
	manager       identity.User
	operator      identity.User
	safety        identity.User
	quality       identity.User
	maintainer    identity.User
	integrator    identity.User
	station       schedule.Workstation
	tool          schedule.Tool
	cell          lifecycle.RobotCell
	qualification schedule.Qualification
	part          maintenance.SparePart
	now           time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fixture.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	createUser := func(username string, role identity.Role) identity.User {
		user, createErr := database.CreateUser(ctx, identity.User{
			Username: username, PasswordHash: "test-password-hash", DisplayName: username,
			Role: role, Active: true,
		})
		if createErr != nil {
			t.Fatalf("create user %s: %v", username, createErr)
		}
		return user
	}
	result := fixture{store: database, now: now}
	result.manager = createUser("line.manager", identity.RoleLineManager)
	result.operator = createUser("operator", identity.RoleOperator)
	result.safety = createUser("safety", identity.RoleSafetyOfficer)
	result.quality = createUser("quality", identity.RoleQualityEngineer)
	result.maintainer = createUser("maintenance", identity.RoleMaintenance)
	result.integrator = createUser("integrator", identity.RoleIntegrator)
	result.station, err = database.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-01", Name: "一号焊装工位", Line: "L1", Active: true})
	if err != nil {
		t.Fatalf("create workstation: %v", err)
	}
	result.tool, err = database.CreateTool(ctx, schedule.Tool{Code: "TL-01", Name: "激光标定工装", Active: true, CalibrationDue: now.Add(365 * 24 * time.Hour)})
	if err != nil {
		t.Fatalf("create tool: %v", err)
	}
	result.cell, err = database.CreateCell(ctx, lifecycle.RobotCell{Code: "RC-01", Name: "焊接机器人单元", WorkstationID: result.station.ID, IntegratorID: result.integrator.ID, Status: lifecycle.CellSurveyed})
	if err != nil {
		t.Fatalf("create cell: %v", err)
	}
	result.qualification, err = database.CreateQualification(ctx, schedule.Qualification{UserID: result.operator.ID, Kind: "robot_installation", ValidFrom: now.Add(-24 * time.Hour), ValidTo: now.Add(365 * 24 * time.Hour)})
	if err != nil {
		t.Fatalf("create qualification: %v", err)
	}
	if _, err = database.CreateQualification(ctx, schedule.Qualification{UserID: result.maintainer.ID, Kind: "maintenance", ValidFrom: now.Add(-24 * time.Hour), ValidTo: now.Add(365 * 24 * time.Hour)}); err != nil {
		t.Fatalf("create maintenance qualification: %v", err)
	}
	result.part, err = database.CreateSparePart(ctx, maintenance.SparePart{Code: "SP-01", Name: "腕部减速器", Available: 3})
	if err != nil {
		t.Fatalf("create spare part: %v", err)
	}
	return result
}

func principal(user identity.User) identity.Principal {
	return identity.Principal{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Role: user.Role, SessionID: "session-" + user.Username}
}

func TestMigrationsAreIdempotentAndEnableForeignKeys(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "migrations.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err = database.Migrate(ctx); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var versions int
	if err = database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatalf("read versions: %v", err)
	}
	if versions != len(migrations) {
		t.Fatalf("migration versions = %d, want %d", versions, len(migrations))
	}
	if err = database.Ready(ctx); err != nil {
		t.Fatalf("database not ready: %v", err)
	}
	if err = database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer reopened.Close()
	if err = reopened.Ready(ctx); err != nil {
		t.Fatalf("reopened database not ready: %v", err)
	}
}

func TestDatabaseRestartRestoresPersistedCell(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := database.CreateUser(ctx, identity.User{Username: "integrator", DisplayName: "集成方", PasswordHash: "hash", Role: identity.RoleIntegrator, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	station, err := database.CreateWorkstation(ctx, schedule.Workstation{Code: "WS-R", Name: "恢复工位", Line: "L2", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	created, err := database.CreateCell(ctx, lifecycle.RobotCell{Code: "RC-R", Name: "恢复机器人", WorkstationID: station.ID, IntegratorID: user.ID, Status: lifecycle.CellSurveyed})
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.GetCell(ctx, created.ID)
	if err != nil {
		t.Fatalf("restore cell: %v", err)
	}
	if restored.Code != created.Code || restored.Status != created.Status || restored.Version != created.Version {
		t.Fatalf("restored cell mismatch: %#v", restored)
	}
}

func TestUserUniquenessAndLookup(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	found, err := fx.store.FindUserByUsername(ctx, fx.manager.Username)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found.ID != fx.manager.ID || found.Role != identity.RoleLineManager || !found.Active {
		t.Fatalf("unexpected user: %#v", found)
	}
	_, err = fx.store.CreateUser(ctx, identity.User{Username: fx.manager.Username, DisplayName: "duplicate", PasswordHash: "hash", Role: identity.RoleOperator, Active: true})
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("duplicate username error = %v", err)
	}
	_, err = fx.store.FindUser(ctx, 999999)
	if !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("missing user error = %v", err)
	}
}

func TestSessionLifecycleIncludesExpiryAndRevocation(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	session := identity.Session{ID: "session-1", UserID: fx.operator.ID, TokenHash: "token-hash-1", CreatedAt: fx.now, LastSeenAt: fx.now, ExpiresAt: fx.now.Add(time.Hour)}
	if err := fx.store.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	resolved, err := fx.store.ResolveSession(ctx, session.TokenHash, fx.now.Add(time.Minute))
	if err != nil {
		t.Fatalf("resolve active session: %v", err)
	}
	if resolved.UserID != fx.operator.ID || resolved.SessionID != session.ID {
		t.Fatalf("unexpected principal: %#v", resolved)
	}
	if err = fx.store.RevokeSession(ctx, session.ID, fx.now.Add(2*time.Minute)); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err = fx.store.ResolveSession(ctx, session.TokenHash, fx.now.Add(3*time.Minute)); !errors.Is(err, apperr.ErrUnauthenticated) {
		t.Fatalf("revoked session error = %v", err)
	}
	expired := identity.Session{ID: "session-expired", UserID: fx.operator.ID, TokenHash: "token-hash-expired", CreatedAt: fx.now, LastSeenAt: fx.now, ExpiresAt: fx.now.Add(time.Minute)}
	if err = fx.store.CreateSession(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err = fx.store.ResolveSession(ctx, expired.TokenHash, fx.now.Add(time.Minute)); !errors.Is(err, apperr.ErrExpired) {
		t.Fatalf("expired session error = %v", err)
	}
	deleted, err := fx.store.PurgeExpiredSessions(ctx, fx.now.Add(25*time.Hour))
	if err != nil || deleted < 2 {
		t.Fatalf("purge expired sessions deleted=%d error=%v", deleted, err)
	}
}

func TestCellTransitionUsesOptimisticVersionAndAuditChain(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	updated, err := fx.store.TransitionCell(ctx, principal(fx.integrator), fx.cell.ID, fx.cell.Version, lifecycle.CellProposed, "勘测完成", "req-1", fx.now)
	if err != nil {
		t.Fatalf("transition: %v", err)
	}
	if updated.Status != lifecycle.CellProposed || updated.Version != fx.cell.Version+1 {
		t.Fatalf("unexpected transition result: %#v", updated)
	}
	_, err = fx.store.TransitionCell(ctx, principal(fx.manager), fx.cell.ID, fx.cell.Version, lifecycle.CellApproved, "批准", "req-stale", fx.now)
	if !errors.Is(err, apperr.ErrVersion) {
		t.Fatalf("stale transition error = %v", err)
	}
	events, err := fx.store.ListAudit(ctx, "robot_cell", fmt.Sprint(fx.cell.ID), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "cell.transition" || events[0].RequestID != "req-1" {
		t.Fatalf("unexpected audit events: %#v", events)
	}
	if events[0].EventHash == "" {
		t.Fatal("audit event hash is required")
	}
}

func TestInvalidCellTransitionRollsBackHistoryAndAudit(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	_, err := fx.store.TransitionCell(ctx, principal(fx.manager), fx.cell.ID, fx.cell.Version, lifecycle.CellProduction, "跳过阶段", "req-invalid", fx.now)
	if !errors.Is(err, apperr.ErrInvalid) {
		t.Fatalf("invalid transition error = %v", err)
	}
	current, err := fx.store.GetCell(ctx, fx.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != lifecycle.CellSurveyed || current.Version != fx.cell.Version {
		t.Fatalf("invalid transition mutated cell: %#v", current)
	}
	var histories int
	if err = fx.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cell_transitions WHERE cell_id=?", fx.cell.ID).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if histories != 0 {
		t.Fatalf("invalid transition wrote %d history rows", histories)
	}
}

func TestInspectionUpdatesOneConclusionAndKeepsEvidence(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	if _, err := fx.store.db.ExecContext(ctx, `UPDATE robot_cells SET status=?, calibration_ref='CAL-001' WHERE id=?`, lifecycle.CellSafetyReview, fx.cell.ID); err != nil {
		t.Fatal(err)
	}
	cell, err := fx.store.RecordInspection(ctx, principal(fx.safety), lifecycle.Inspection{CellID: fx.cell.ID, Kind: lifecycle.InspectionSafety, Passed: true, InspectorID: fx.safety.ID, Notes: "防护距离符合要求", RecordedAt: fx.now}, "req-safety")
	if err != nil {
		t.Fatalf("safety inspection: %v", err)
	}
	if !cell.SafetyPassed || cell.QualityPassed {
		t.Fatalf("inspection flags = safety:%v quality:%v", cell.SafetyPassed, cell.QualityPassed)
	}
	cell, err = fx.store.RecordInspection(ctx, principal(fx.quality), lifecycle.Inspection{CellID: fx.cell.ID, Kind: lifecycle.InspectionQuality, Passed: true, InspectorID: fx.quality.ID, Notes: "重复定位精度合格", RecordedAt: fx.now.Add(time.Minute)}, "req-quality")
	if err != nil {
		t.Fatal(err)
	}
	if !cell.SafetyPassed || !cell.QualityPassed || !cell.CanTransition(lifecycle.CellProduction) {
		t.Fatalf("completed inspection did not unlock production: %#v", cell)
	}
}

func TestListCellsUsesSameFilterForTotalAndItems(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	for index := 2; index <= 6; index++ {
		status := lifecycle.CellSurveyed
		if index%2 == 0 {
			status = lifecycle.CellProposed
		}
		_, err := fx.store.CreateCell(ctx, lifecycle.RobotCell{Code: fmt.Sprintf("RC-%02d", index), Name: fmt.Sprintf("机器人 %d", index), WorkstationID: fx.station.ID, IntegratorID: fx.integrator.ID, Status: status})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := fx.store.ListCells(ctx, lifecycle.CellProposed, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Page != 1 || page.PageSize != 2 {
		t.Fatalf("unexpected page: %#v", page)
	}
	for _, cell := range page.Items {
		if cell.Status != lifecycle.CellProposed {
			t.Fatalf("filter leaked status %s", cell.Status)
		}
	}
}

func TestWindowApprovalAtomicallyReservesAllResources(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	start := fx.now.Add(24 * time.Hour)
	window, err := fx.store.CreateWindow(ctx, schedule.WorkWindow{CellID: fx.cell.ID, WorkstationID: fx.station.ID, ToolID: fx.tool.ID, QualifiedUserID: fx.operator.ID, StartsAt: start, EndsAt: start.Add(4 * time.Hour), Status: schedule.WindowRequested, Purpose: "机器人安装"})
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := fx.store.ApproveAndReserveWindow(ctx, principal(fx.manager), window.ID, window.Version, "robot_installation", "req-window", fx.now)
	if err != nil {
		t.Fatalf("approve window: %v", err)
	}
	if reserved.Status != schedule.WindowReserved || reserved.Version != 2 {
		t.Fatalf("unexpected reserved window: %#v", reserved)
	}
	count, err := fx.store.CountActiveReservations(ctx, window.ID)
	if err != nil || count != 3 {
		t.Fatalf("active reservations=%d error=%v", count, err)
	}
}

func TestWindowConflictRollsBackEveryReservation(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	start := fx.now.Add(24 * time.Hour)
	first, err := fx.store.CreateWindow(ctx, schedule.WorkWindow{CellID: fx.cell.ID, WorkstationID: fx.station.ID, ToolID: fx.tool.ID, QualifiedUserID: fx.operator.ID, StartsAt: start, EndsAt: start.Add(4 * time.Hour), Status: schedule.WindowRequested, Purpose: "首次安装"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fx.store.ApproveAndReserveWindow(ctx, principal(fx.manager), first.ID, first.Version, "robot_installation", "req-first", fx.now); err != nil {
		t.Fatal(err)
	}
	second, err := fx.store.CreateWindow(ctx, schedule.WorkWindow{CellID: fx.cell.ID, WorkstationID: fx.station.ID, ToolID: fx.tool.ID, QualifiedUserID: fx.operator.ID, StartsAt: start.Add(time.Hour), EndsAt: start.Add(5 * time.Hour), Status: schedule.WindowRequested, Purpose: "冲突安装"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fx.store.ApproveAndReserveWindow(ctx, principal(fx.manager), second.ID, second.Version, "robot_installation", "req-second", fx.now)
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("conflicting reservation error = %v", err)
	}
	count, err := fx.store.CountActiveReservations(ctx, second.ID)
	if err != nil || count != 0 {
		t.Fatalf("failed window leaked %d reservations: %v", count, err)
	}
	current, err := fx.store.GetWindow(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != schedule.WindowRequested || current.Version != 1 {
		t.Fatalf("failed approval mutated window: %#v", current)
	}
}

func TestConcurrentOverlappingWindowApprovalHasSingleWinner(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	start := fx.now.Add(48 * time.Hour)
	windows := make([]schedule.WorkWindow, 2)
	for index := range windows {
		created, err := fx.store.CreateWindow(ctx, schedule.WorkWindow{CellID: fx.cell.ID, WorkstationID: fx.station.ID, ToolID: fx.tool.ID, QualifiedUserID: fx.operator.ID, StartsAt: start.Add(time.Duration(index) * time.Minute), EndsAt: start.Add(4 * time.Hour), Status: schedule.WindowRequested, Purpose: fmt.Sprintf("并发安装 %d", index)})
		if err != nil {
			t.Fatal(err)
		}
		windows[index] = created
	}
	startGate := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, window := range windows {
		window := window
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-startGate
			_, err := fx.store.ApproveAndReserveWindow(ctx, principal(fx.manager), window.ID, window.Version, "robot_installation", fmt.Sprintf("req-%d", window.ID), fx.now)
			results <- err
		}()
	}
	close(startGate)
	wait.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, apperr.ErrConflict) {
			conflicted++
		} else {
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent outcomes success=%d conflict=%d", succeeded, conflicted)
	}
}

func TestWindowCancellationReleasesReservations(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	start := fx.now.Add(24 * time.Hour)
	window, err := fx.store.CreateWindow(ctx, schedule.WorkWindow{CellID: fx.cell.ID, WorkstationID: fx.station.ID, ToolID: fx.tool.ID, QualifiedUserID: fx.operator.ID, StartsAt: start, EndsAt: start.Add(time.Hour), Status: schedule.WindowRequested, Purpose: "短时安装"})
	if err != nil {
		t.Fatal(err)
	}
	window, err = fx.store.ApproveAndReserveWindow(ctx, principal(fx.manager), window.ID, window.Version, "robot_installation", "req-reserve", fx.now)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := fx.store.CancelWindow(ctx, principal(fx.manager), window.ID, window.Version, "产线计划调整", "req-cancel", fx.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != schedule.WindowCancelled {
		t.Fatalf("window status = %s", cancelled.Status)
	}
	count, err := fx.store.CountActiveReservations(ctx, window.ID)
	if err != nil || count != 0 {
		t.Fatalf("active reservations after cancel=%d err=%v", count, err)
	}
}

func TestCancelledContextPreventsTransactionMutation(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fx.store.TransitionCell(ctx, principal(fx.integrator), fx.cell.ID, fx.cell.Version, lifecycle.CellProposed, "cancelled", "req-cancelled", fx.now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled transaction error = %v", err)
	}
	current, readErr := fx.store.GetCell(context.Background(), fx.cell.ID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if current.Status != lifecycle.CellSurveyed || current.Version != fx.cell.Version {
		t.Fatalf("cancelled transaction mutated cell: %#v", current)
	}
}

func TestMaintenanceReservationAndConsumptionAreAtomic(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	if _, err := fx.store.db.ExecContext(ctx, `UPDATE robot_cells SET status=? WHERE id=?`, lifecycle.CellProduction, fx.cell.ID); err != nil {
		t.Fatal(err)
	}
	partID := fx.part.ID
	order, err := fx.store.CreateMaintenanceOrder(ctx, maintenance.Order{Code: "MO-001", CellID: fx.cell.ID, AssigneeID: fx.maintainer.ID, SparePartID: &partID, SpareQuantity: 2, Priority: 2, Summary: "更换减速器", Status: maintenance.Opened, DueAt: fx.now.Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	for _, next := range []maintenance.Status{maintenance.Triaged, maintenance.Approved, maintenance.Executing} {
		order, err = fx.store.AdvanceMaintenance(ctx, principal(fx.maintainer), order.ID, order.Version, next, "req-maintenance", fx.now)
		if err != nil {
			t.Fatalf("advance to %s: %v", next, err)
		}
	}
	part, err := fx.store.GetSparePart(ctx, partID)
	if err != nil {
		t.Fatal(err)
	}
	if part.Available != 3 || part.Reserved != 2 {
		t.Fatalf("reserved part = %#v", part)
	}
	for _, next := range []maintenance.Status{maintenance.Verifying, maintenance.Closed} {
		order, err = fx.store.AdvanceMaintenance(ctx, principal(fx.maintainer), order.ID, order.Version, next, "req-maintenance-close", fx.now.Add(time.Hour))
		if err != nil {
			t.Fatalf("advance to %s: %v", next, err)
		}
	}
	part, err = fx.store.GetSparePart(ctx, partID)
	if err != nil {
		t.Fatal(err)
	}
	if part.Available != 1 || part.Reserved != 0 {
		t.Fatalf("consumed part = %#v", part)
	}
	cell, err := fx.store.GetCell(ctx, fx.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cell.Status != lifecycle.CellProduction {
		t.Fatalf("cell did not return to production: %s", cell.Status)
	}
}

func TestInsufficientSparePartRollsBackMaintenanceExecution(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	if _, err := fx.store.db.ExecContext(ctx, `UPDATE robot_cells SET status=? WHERE id=?`, lifecycle.CellProduction, fx.cell.ID); err != nil {
		t.Fatal(err)
	}
	partID := fx.part.ID
	order, err := fx.store.CreateMaintenanceOrder(ctx, maintenance.Order{Code: "MO-TOO-MANY", CellID: fx.cell.ID, AssigneeID: fx.maintainer.ID, SparePartID: &partID, SpareQuantity: 4, Priority: 1, Summary: "需要超量备件", Status: maintenance.Approved, DueAt: fx.now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fx.store.AdvanceMaintenance(ctx, principal(fx.maintainer), order.ID, order.Version, maintenance.Executing, "req-insufficient", fx.now)
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("insufficient part error = %v", err)
	}
	current, err := fx.store.GetMaintenanceOrder(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != maintenance.Approved || current.Version != order.Version {
		t.Fatalf("failed execution mutated order: %#v", current)
	}
	part, err := fx.store.GetSparePart(ctx, partID)
	if err != nil {
		t.Fatal(err)
	}
	if part.Reserved != 0 || part.Available != 3 {
		t.Fatalf("failed execution mutated part: %#v", part)
	}
}

func TestRecoveryIdempotencyClaimRetryAndCompletion(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	job := recovery.Job{Kind: "calibration_compensation", ObjectType: "robot_cell", ObjectID: fx.cell.ID, IdempotencyKey: "calibration:1:report-1", Payload: []byte(`{"reason":"sensor drift"}`), MaxAttempts: 3, NextAttemptAt: fx.now, CreatedAt: fx.now, UpdatedAt: fx.now}
	created, err := fx.store.CreateRecoveryJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := fx.store.CreateRecoveryJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ID != created.ID {
		t.Fatalf("idempotent create returned new id: first=%d second=%d", created.ID, repeated.ID)
	}
	claimed, err := fx.store.ClaimRecoveryJob(ctx, "worker-a", fx.now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != recovery.Running || claimed.Attempts != 1 || claimed.LeaseOwner != "worker-a" {
		t.Fatalf("unexpected claim: %#v", claimed)
	}
	if err = fx.store.FailRecoveryJob(ctx, claimed, "worker-a", errors.New("temporary controller outage"), fx.now); err != nil {
		t.Fatal(err)
	}
	stored, err := fx.store.FindRecoveryByKey(ctx, job.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != recovery.RetryWait || stored.LastError == "" || !stored.NextAttemptAt.Equal(fx.now.Add(time.Second)) {
		t.Fatalf("unexpected retry state: %#v", stored)
	}
	claimed, err = fx.store.ClaimRecoveryJob(ctx, "worker-b", stored.NextAttemptAt, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Attempts != 2 || claimed.LeaseOwner != "worker-b" {
		t.Fatalf("unexpected second claim: %#v", claimed)
	}
	if err = fx.store.CompleteRecoveryJob(ctx, claimed.ID, "worker-b", stored.NextAttemptAt); err != nil {
		t.Fatal(err)
	}
	stored, err = fx.store.FindRecoveryByKey(ctx, job.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != recovery.Succeeded || stored.LeaseOwner != "" || stored.LeaseUntil != nil {
		t.Fatalf("unexpected completed state: %#v", stored)
	}
}

func TestExpiredRecoveryLeaseCanBeReclaimedAfterRestart(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	job, err := fx.store.CreateRecoveryJob(ctx, recovery.Job{Kind: "calibration_compensation", ObjectType: "robot_cell", ObjectID: fx.cell.ID, IdempotencyKey: "lease-restart", Payload: []byte(`{}`), MaxAttempts: 3, NextAttemptAt: fx.now, CreatedAt: fx.now, UpdatedAt: fx.now})
	if err != nil {
		t.Fatal(err)
	}
	first, err := fx.store.ClaimRecoveryJob(ctx, "dead-worker", fx.now, time.Second)
	if err != nil || first.ID != job.ID {
		t.Fatalf("first claim=%#v err=%v", first, err)
	}
	second, err := fx.store.ClaimRecoveryJob(ctx, "replacement-worker", fx.now.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatalf("reclaim expired lease: %v", err)
	}
	if second.ID != job.ID || second.Attempts != 2 || second.LeaseOwner != "replacement-worker" {
		t.Fatalf("unexpected reclaimed job: %#v", second)
	}
}

func TestAuditHashChainLinksEvents(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	first, err := audit.New(fx.manager.ID, string(fx.manager.Role), "batch.plan", "production_batch", "PB-1", "req-a", audit.ResultSucceeded, map[string]any{"status": "planned"}, fx.now)
	if err != nil {
		t.Fatal(err)
	}
	first, err = fx.store.AppendAudit(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := audit.New(fx.manager.ID, string(fx.manager.Role), "batch.activate", "production_batch", "PB-1", "req-b", audit.ResultSucceeded, map[string]any{"status": "active"}, fx.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	second, err = fx.store.AppendAudit(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventHash == "" || second.EventHash == "" || first.EventHash == second.EventHash {
		t.Fatal("audit event hashes must be populated and unique")
	}
	if second.PreviousHash != first.EventHash {
		t.Fatalf("audit chain previous=%s want=%s", second.PreviousHash, first.EventHash)
	}
}

func TestCalibrationCompensationRestoresInstallStateOnce(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	if _, err := fx.store.db.ExecContext(ctx, `UPDATE robot_cells SET status=?,calibration_ref='FAILED-CAL',version=7 WHERE id=?`, lifecycle.CellCalibrating, fx.cell.ID); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"cell_id": fx.cell.ID, "reason": "laser target lost", "actor_id": fx.integrator.ID, "version": 7})
	if err != nil {
		t.Fatal(err)
	}
	job, err := fx.store.CreateRecoveryJob(ctx, recovery.Job{Kind: "calibration_compensation", ObjectType: "robot_cell", ObjectID: fx.cell.ID, IdempotencyKey: "compensate-once", Payload: payload, MaxAttempts: 3, NextAttemptAt: fx.now, CreatedAt: fx.now, UpdatedAt: fx.now})
	if err != nil {
		t.Fatal(err)
	}
	if err = fx.store.CompensateCalibration(ctx, job, fx.now); err != nil {
		t.Fatalf("compensate calibration: %v", err)
	}
	cell, err := fx.store.GetCell(ctx, fx.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cell.Status != lifecycle.CellInstalling || cell.CalibrationRef != "" || cell.Version != 8 {
		t.Fatalf("unexpected compensated cell: %#v", cell)
	}
	events, err := fx.store.ListAudit(ctx, "robot_cell", fmt.Sprint(fx.cell.ID), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "recovery.calibration_compensate" {
		t.Fatalf("unexpected recovery audit: %#v", events)
	}
	if err = fx.store.CompensateCalibration(ctx, job, fx.now.Add(time.Minute)); err != nil {
		t.Fatalf("repeat compensation must be idempotent: %v", err)
	}
	events, err = fx.store.ListAudit(ctx, "robot_cell", fmt.Sprint(fx.cell.ID), 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("repeat compensation duplicated audit: count=%d error=%v", len(events), err)
	}
}

func TestRetirementRejectsActiveMaintenanceAndPreservesCell(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	if _, err := fx.store.db.ExecContext(ctx, `UPDATE robot_cells SET status=?,version=4 WHERE id=?`, lifecycle.CellProduction, fx.cell.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.store.CreateMaintenanceOrder(ctx, maintenance.Order{Code: "MO-RETIRE", CellID: fx.cell.ID, AssigneeID: fx.maintainer.ID, Priority: 2, Summary: "待处理保养", Status: maintenance.Opened, DueAt: fx.now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	_, err := fx.store.RetireCell(ctx, principal(fx.manager), fx.cell.ID, 4, "退役", "req-retire-blocked", fx.now)
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("active maintenance retirement error = %v", err)
	}
	cell, err := fx.store.GetCell(ctx, fx.cell.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cell.Status != lifecycle.CellProduction || cell.Version != 4 {
		t.Fatalf("blocked retirement mutated cell: %#v", cell)
	}
}

func TestRetirementRejectsActiveResourceReservation(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	start := fx.now.Add(24 * time.Hour)
	window, err := fx.store.CreateWindow(ctx, schedule.WorkWindow{CellID: fx.cell.ID, WorkstationID: fx.station.ID, ToolID: fx.tool.ID, QualifiedUserID: fx.operator.ID, StartsAt: start, EndsAt: start.Add(time.Hour), Status: schedule.WindowRequested, Purpose: "退役前安装冲突"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fx.store.ApproveAndReserveWindow(ctx, principal(fx.manager), window.ID, window.Version, "robot_installation", "req-reserve-retire", fx.now); err != nil {
		t.Fatal(err)
	}
	if _, err = fx.store.db.ExecContext(ctx, `UPDATE robot_cells SET status=?,version=5 WHERE id=?`, lifecycle.CellProduction, fx.cell.ID); err != nil {
		t.Fatal(err)
	}
	_, err = fx.store.RetireCell(ctx, principal(fx.manager), fx.cell.ID, 5, "退役", "req-retire-reservation", fx.now)
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("active reservation retirement error = %v", err)
	}
}

func TestRetirementSucceedsAfterCrossObjectClosure(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	if _, err := fx.store.db.ExecContext(ctx, `UPDATE robot_cells SET status=?,version=6 WHERE id=?`, lifecycle.CellProduction, fx.cell.ID); err != nil {
		t.Fatal(err)
	}
	retired, err := fx.store.RetireCell(ctx, principal(fx.manager), fx.cell.ID, 6, "生命周期结束", "req-retire", fx.now)
	if err != nil {
		t.Fatalf("retire closed cell: %v", err)
	}
	if retired.Status != lifecycle.CellRetired || retired.Version != 7 {
		t.Fatalf("unexpected retired cell: %#v", retired)
	}
	events, err := fx.store.ListAudit(ctx, "robot_cell", fmt.Sprint(fx.cell.ID), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "cell.retire" {
		t.Fatalf("retirement audit missing: %#v", events)
	}
}
