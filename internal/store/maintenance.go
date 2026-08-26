package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/audit"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
)

func (s *Store) CreateMaintenanceOrder(ctx context.Context, order maintenance.Order) (maintenance.Order, error) {
	if err := order.Validate(); err != nil {
		return maintenance.Order{}, apperr.Wrap(apperr.ErrInvalid, "store.create_maintenance", "invalid maintenance order", err)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO maintenance_orders(code,cell_id,assignee_id,spare_part_id,spare_quantity,priority,summary,status,version,due_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,?,?,?)`, order.Code, order.CellID, order.AssigneeID, order.SparePartID, order.SpareQuantity, order.Priority, order.Summary, order.Status, encodeTime(order.DueAt), encodeTime(now), encodeTime(now))
	if err != nil {
		return maintenance.Order{}, fmt.Errorf("insert maintenance order: %w", err)
	}
	order.ID, err = result.LastInsertId()
	order.Version = 1
	order.CreatedAt = now
	order.UpdatedAt = now
	return order, err
}

func getOrder(ctx context.Context, q DBTX, id int64) (maintenance.Order, error) {
	var o maintenance.Order
	var part sql.NullInt64
	var due, created, updated string
	err := q.QueryRowContext(ctx, `SELECT id,code,cell_id,assignee_id,spare_part_id,spare_quantity,priority,summary,status,version,due_at,created_at,updated_at FROM maintenance_orders WHERE id=?`, id).Scan(&o.ID, &o.Code, &o.CellID, &o.AssigneeID, &part, &o.SpareQuantity, &o.Priority, &o.Summary, &o.Status, &o.Version, &due, &created, &updated)
	if err == sql.ErrNoRows {
		return maintenance.Order{}, apperr.Wrap(apperr.ErrNotFound, "store.get_maintenance", "maintenance order not found", err)
	}
	if err != nil {
		return maintenance.Order{}, err
	}
	if part.Valid {
		o.SparePartID = &part.Int64
	}
	var parseErr error
	if o.DueAt, parseErr = decodeTime(due); parseErr != nil {
		return maintenance.Order{}, parseErr
	}
	if o.CreatedAt, parseErr = decodeTime(created); parseErr != nil {
		return maintenance.Order{}, parseErr
	}
	if o.UpdatedAt, parseErr = decodeTime(updated); parseErr != nil {
		return maintenance.Order{}, parseErr
	}
	return o, nil
}

func (s *Store) GetMaintenanceOrder(ctx context.Context, id int64) (maintenance.Order, error) {
	return getOrder(ctx, s.db, id)
}

func (s *Store) AdvanceMaintenance(ctx context.Context, principal identity.Principal, id, expected int64, next maintenance.Status, requestID string, now time.Time) (maintenance.Order, error) {
	var updated maintenance.Order
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		order, err := getOrder(ctx, tx, id)
		if err != nil {
			return err
		}
		if order.Version != expected {
			return apperr.New(apperr.ErrVersion, "store.advance_maintenance", "maintenance order changed concurrently")
		}
		if !order.CanTransition(next) {
			return apperr.New(apperr.ErrInvalid, "store.advance_maintenance", "maintenance transition is not allowed")
		}
		if next == maintenance.Executing {
			var valid int
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM qualifications WHERE user_id=? AND kind='maintenance' AND revoked_at IS NULL AND valid_from<=? AND valid_to>?`, order.AssigneeID, encodeTime(now), encodeTime(now)).Scan(&valid); err != nil {
				return err
			}
			if valid == 0 {
				return apperr.New(apperr.ErrForbidden, "store.advance_maintenance", "assignee lacks maintenance qualification")
			}
			if order.SparePartID != nil {
				result, err := tx.ExecContext(ctx, `UPDATE spare_parts SET reserved=reserved+?,version=version+1,updated_at=? WHERE id=? AND available-reserved>=?`, order.SpareQuantity, encodeTime(now), *order.SparePartID, order.SpareQuantity)
				if err != nil {
					return err
				}
				count, _ := result.RowsAffected()
				if count != 1 {
					return apperr.New(apperr.ErrConflict, "store.advance_maintenance", "insufficient spare part capacity")
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO part_movements(part_id,order_id,quantity,kind,created_at) VALUES(?,?,?,'reserve',?)`, *order.SparePartID, id, order.SpareQuantity, encodeTime(now)); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `UPDATE robot_cells SET status=?,version=version+1,updated_at=? WHERE id=? AND status=?`, lifecycle.CellMaintenance, encodeTime(now), order.CellID, lifecycle.CellProduction); err != nil {
				return err
			}
		}
		if next == maintenance.Closed && order.SparePartID != nil {
			result, err := tx.ExecContext(ctx, `UPDATE spare_parts SET available=available-?,reserved=reserved-?,version=version+1,updated_at=? WHERE id=? AND reserved>=?`, order.SpareQuantity, order.SpareQuantity, encodeTime(now), *order.SparePartID, order.SpareQuantity)
			if err != nil {
				return err
			}
			count, _ := result.RowsAffected()
			if count != 1 {
				return apperr.New(apperr.ErrConflict, "store.advance_maintenance", "reserved spare part state changed")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO part_movements(part_id,order_id,quantity,kind,created_at) VALUES(?,?,?,'consume',?)`, *order.SparePartID, id, order.SpareQuantity, encodeTime(now)); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE robot_cells SET status=?,version=version+1,updated_at=? WHERE id=? AND status=?`, lifecycle.CellProduction, encodeTime(now), order.CellID, lifecycle.CellMaintenance); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE maintenance_orders SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, next, encodeTime(now), id, expected)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return apperr.New(apperr.ErrVersion, "store.advance_maintenance", "maintenance order changed concurrently")
		}
		event, err := audit.New(principal.UserID, string(principal.Role), "maintenance.transition", "maintenance_order", strconv.FormatInt(id, 10), requestID, audit.ResultSucceeded, map[string]any{"from": order.Status, "to": next}, now)
		if err != nil {
			return err
		}
		if _, err = appendAudit(ctx, tx, event); err != nil {
			return err
		}
		updated, err = getOrder(ctx, tx, id)
		return err
	})
	return updated, err
}

func (s *Store) GetSparePart(ctx context.Context, id int64) (maintenance.SparePart, error) {
	var p maintenance.SparePart
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,code,name,available,reserved,version,updated_at FROM spare_parts WHERE id=?`, id).Scan(&p.ID, &p.Code, &p.Name, &p.Available, &p.Reserved, &p.Version, &updated)
	if err != nil {
		return maintenance.SparePart{}, err
	}
	p.UpdatedAt, err = decodeTime(updated)
	return p, err
}
