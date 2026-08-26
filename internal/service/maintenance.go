package service

import (
	"context"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

type Maintenance struct {
	store *store.Store
	clock clock.Clock
}

func NewMaintenance(s *store.Store, c clock.Clock) *Maintenance {
	return &Maintenance{store: s, clock: c}
}

func (s *Maintenance) Open(ctx context.Context, order maintenance.Order) (maintenance.Order, error) {
	if _, err := RequireRole(ctx, identity.RoleOperator, identity.RoleMaintenance, identity.RoleLineManager); err != nil {
		return maintenance.Order{}, err
	}
	order.Status = maintenance.Opened
	return s.store.CreateMaintenanceOrder(ctx, order)
}

func (s *Maintenance) Advance(ctx context.Context, id, expected int64, next maintenance.Status, requestID string) (maintenance.Order, error) {
	principal, err := RequireRole(ctx, identity.RoleMaintenance, identity.RoleLineManager, identity.RoleQualityEngineer)
	if err != nil {
		return maintenance.Order{}, err
	}
	return s.store.AdvanceMaintenance(ctx, principal, id, expected, next, requestID, s.clock.Now())
}
