package service

import (
	"context"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

type Scheduling struct {
	store *store.Store
	clock clock.Clock
}

func NewScheduling(s *store.Store, c clock.Clock) *Scheduling { return &Scheduling{store: s, clock: c} }

func (s *Scheduling) Request(ctx context.Context, window schedule.WorkWindow, requestID string) (schedule.WorkWindow, error) {
	if _, err := requireRoleWithAudit(ctx, s.store, s.clock, "window.request", "work_window", 0, requestID, identity.RoleLineManager, identity.RoleIntegrator); err != nil {
		return schedule.WorkWindow{}, err
	}
	if !window.StartsAt.After(s.clock.Now()) {
		return schedule.WorkWindow{}, apperr.New(apperr.ErrInvalid, "service.request_window", "work window must start in the future")
	}
	window.Status = schedule.WindowRequested
	return s.store.CreateWindow(ctx, window)
}

func (s *Scheduling) Approve(ctx context.Context, id, expected int64, qualification, requestID string) (schedule.WorkWindow, error) {
	principal, err := requireRoleWithAudit(ctx, s.store, s.clock, "window.reserve", "work_window", id, requestID, identity.RoleLineManager, identity.RoleSafetyOfficer)
	if err != nil {
		return schedule.WorkWindow{}, err
	}
	if err = s.store.StageCellForWindow(ctx, principal, id, requestID, s.clock.Now()); err != nil {
		return schedule.WorkWindow{}, err
	}
	return s.store.ApproveAndReserveWindow(ctx, principal, id, expected, qualification, requestID, s.clock.Now())
}

func (s *Scheduling) Cancel(ctx context.Context, id, expected int64, reason, requestID string) (schedule.WorkWindow, error) {
	principal, err := requireRoleWithAudit(ctx, s.store, s.clock, "window.cancel", "work_window", id, requestID, identity.RoleLineManager, identity.RoleOperator, identity.RoleIntegrator)
	if err != nil {
		return schedule.WorkWindow{}, err
	}
	if reason == "" {
		return schedule.WorkWindow{}, apperr.New(apperr.ErrInvalid, "service.cancel_window", "cancellation reason is required")
	}
	return s.store.CancelWindow(ctx, principal, id, expected, reason, requestID, s.clock.Now())
}
