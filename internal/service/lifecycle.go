package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

type Lifecycle struct {
	store *store.Store
	clock clock.Clock
}

func NewLifecycle(s *store.Store, c clock.Clock) *Lifecycle { return &Lifecycle{store: s, clock: c} }

func (s *Lifecycle) CreateCell(ctx context.Context, cell lifecycle.RobotCell) (lifecycle.RobotCell, error) {
	principal, err := RequireRole(ctx, identity.RoleLineManager, identity.RoleIntegrator)
	if err != nil {
		return lifecycle.RobotCell{}, err
	}
	if principal.Role == identity.RoleIntegrator && cell.IntegratorID != principal.UserID {
		return lifecycle.RobotCell{}, apperr.New(apperr.ErrForbidden, "service.create_cell", "integrator can only register its own cell")
	}
	cell.Status = lifecycle.CellSurveyed
	return s.store.CreateCell(ctx, cell)
}

func (s *Lifecycle) Transition(ctx context.Context, id, expected int64, next lifecycle.CellStatus, reason, requestID string) (lifecycle.RobotCell, error) {
	principal, err := PrincipalFromContext(ctx)
	if err != nil {
		return lifecycle.RobotCell{}, err
	}
	allowed := map[lifecycle.CellStatus][]identity.Role{lifecycle.CellProposed: {identity.RoleIntegrator}, lifecycle.CellApproved: {identity.RoleLineManager}, lifecycle.CellScheduled: {identity.RoleLineManager}, lifecycle.CellInstalling: {identity.RoleIntegrator, identity.RoleOperator}, lifecycle.CellCalibrating: {identity.RoleIntegrator}, lifecycle.CellSafetyReview: {identity.RoleIntegrator}, lifecycle.CellProduction: {identity.RoleLineManager}, lifecycle.CellMaintenance: {identity.RoleMaintenance}, lifecycle.CellRetired: {identity.RoleLineManager}}
	roles, ok := allowed[next]
	if !ok || !principal.HasAny(roles...) {
		return lifecycle.RobotCell{}, apperr.New(apperr.ErrForbidden, "service.transition_cell", "role cannot perform requested transition")
	}
	return s.store.TransitionCell(ctx, principal, id, expected, next, reason, requestID, s.clock.Now())
}

func (s *Lifecycle) RecordInspection(ctx context.Context, item lifecycle.Inspection, requestID string) (lifecycle.RobotCell, error) {
	required := identity.RoleSafetyOfficer
	if item.Kind == lifecycle.InspectionQuality {
		required = identity.RoleQualityEngineer
	}
	principal, err := RequireRole(ctx, required)
	if err != nil {
		return lifecycle.RobotCell{}, err
	}
	item.InspectorID = principal.UserID
	item.RecordedAt = s.clock.Now()
	return s.store.RecordInspection(ctx, principal, item, requestID)
}

func (s *Lifecycle) ReportCalibrationFailure(ctx context.Context, cellID int64, idempotencyKey, reason string) (recovery.Job, error) {
	principal, err := RequireRole(ctx, identity.RoleIntegrator, identity.RoleOperator)
	if err != nil {
		return recovery.Job{}, err
	}
	if idempotencyKey == "" || reason == "" {
		return recovery.Job{}, apperr.New(apperr.ErrInvalid, "service.calibration_failure", "idempotency key and reason are required")
	}
	cell, err := s.store.GetCell(ctx, cellID)
	if err != nil {
		return recovery.Job{}, err
	}
	if cell.Status != lifecycle.CellCalibrating {
		return recovery.Job{}, apperr.New(apperr.ErrInvalid, "service.calibration_failure", "cell is not calibrating")
	}
	payload, _ := json.Marshal(map[string]any{"cell_id": cellID, "reason": reason, "actor_id": principal.UserID, "version": cell.Version})
	now := s.clock.Now()
	job := recovery.Job{Kind: "calibration_compensation", ObjectType: "robot_cell", ObjectID: cellID, IdempotencyKey: fmt.Sprintf("calibration:%d:%s", cellID, idempotencyKey), Payload: payload, MaxAttempts: 5, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
	return s.store.CreateRecoveryJob(ctx, job)
}

func (s *Lifecycle) List(ctx context.Context, status lifecycle.CellStatus, page, size int) (lifecycle.Page, error) {
	if _, err := PrincipalFromContext(ctx); err != nil {
		return lifecycle.Page{}, err
	}
	return s.store.ListCells(ctx, status, page, size)
}

func (s *Lifecycle) Retire(ctx context.Context, id, expected int64, requestID string) (lifecycle.RobotCell, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.Transition(ctx, id, expected, lifecycle.CellRetired, "lifecycle completed", requestID)
}
