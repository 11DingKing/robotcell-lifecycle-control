package store

import (
	"context"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
)

func TestMaintenanceExecutionAllowsExactSpareCapacity(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	partID := fx.part.ID
	order, err := fx.store.CreateMaintenanceOrder(ctx, maintenance.Order{Code:"MO-EXACT", CellID:fx.cell.ID, AssigneeID:fx.maintainer.ID, SparePartID:&partID, SpareQuantity:3, Priority:1, Summary:"exact stock", Status:maintenance.Approved, DueAt:fx.now.Add(time.Hour)})
	if err != nil { t.Fatal(err) }
	if _, err = fx.store.AdvanceMaintenance(ctx, principal(fx.maintainer), order.ID, order.Version, maintenance.Executing, "exact", fx.now); err != nil { t.Fatalf("exact capacity rejected: %v", err) }
}
