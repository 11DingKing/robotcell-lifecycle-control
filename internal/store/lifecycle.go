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
)

func (s *Store) CreateBatch(ctx context.Context, batch lifecycle.ProductionBatch) (lifecycle.ProductionBatch, error) {
	if err := batch.Validate(); err != nil {
		return lifecycle.ProductionBatch{}, apperr.Wrap(apperr.ErrInvalid, "store.create_batch", "invalid production batch", err)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO production_batches(code,name,status,starts_at,ends_at,version,created_at,updated_at) VALUES(?,?,?,?,?,1,?,?)`, batch.Code, batch.Name, batch.Status, encodeTime(batch.StartsAt), encodeTime(batch.EndsAt), encodeTime(now), encodeTime(now))
	if err != nil {
		return lifecycle.ProductionBatch{}, fmt.Errorf("insert batch: %w", err)
	}
	batch.ID, err = result.LastInsertId()
	batch.Version = 1
	batch.CreatedAt = now
	batch.UpdatedAt = now
	return batch, err
}

func (s *Store) CreateCell(ctx context.Context, cell lifecycle.RobotCell) (lifecycle.RobotCell, error) {
	if err := cell.Validate(); err != nil {
		return lifecycle.RobotCell{}, apperr.Wrap(apperr.ErrInvalid, "store.create_cell", "invalid robot cell", err)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO robot_cells(code,name,batch_id,workstation_id,integrator_id,status,safety_passed,quality_passed,calibration_ref,version,created_at,updated_at) VALUES(?,?,?,?,?,?,0,0,'',1,?,?)`, cell.Code, cell.Name, cell.BatchID, cell.WorkstationID, cell.IntegratorID, cell.Status, encodeTime(now), encodeTime(now))
	if err != nil {
		return lifecycle.RobotCell{}, fmt.Errorf("insert cell: %w", err)
	}
	cell.ID, err = result.LastInsertId()
	cell.Version = 1
	cell.CreatedAt = now
	cell.UpdatedAt = now
	return cell, err
}

func getCell(ctx context.Context, q DBTX, id int64) (lifecycle.RobotCell, error) {
	var cell lifecycle.RobotCell
	var batch sql.NullInt64
	var safety, quality int
	var created, updated string
	err := q.QueryRowContext(ctx, `SELECT id,code,name,batch_id,workstation_id,integrator_id,status,safety_passed,quality_passed,calibration_ref,version,created_at,updated_at FROM robot_cells WHERE id=?`, id).Scan(&cell.ID, &cell.Code, &cell.Name, &batch, &cell.WorkstationID, &cell.IntegratorID, &cell.Status, &safety, &quality, &cell.CalibrationRef, &cell.Version, &created, &updated)
	if err == sql.ErrNoRows {
		return lifecycle.RobotCell{}, apperr.Wrap(apperr.ErrNotFound, "store.get_cell", "robot cell not found", err)
	}
	if err != nil {
		return lifecycle.RobotCell{}, fmt.Errorf("scan cell: %w", err)
	}
	if batch.Valid {
		cell.BatchID = &batch.Int64
	}
	cell.SafetyPassed = safety == 1
	cell.QualityPassed = quality == 1
	var parseErr error
	if cell.CreatedAt, parseErr = decodeTime(created); parseErr != nil {
		return lifecycle.RobotCell{}, parseErr
	}
	if cell.UpdatedAt, parseErr = decodeTime(updated); parseErr != nil {
		return lifecycle.RobotCell{}, parseErr
	}
	return cell, nil
}

func (s *Store) GetCell(ctx context.Context, id int64) (lifecycle.RobotCell, error) {
	return getCell(ctx, s.db, id)
}

func (s *Store) ListCells(ctx context.Context, status lifecycle.CellStatus, page, size int) (lifecycle.Page, error) {
	page, size = lifecycle.NormalizePage(page, size)
	where := ""
	args := []any{}
	if status != "" {
		where = " WHERE status=?"
		args = append(args, status)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM robot_cells"+where, args...).Scan(&total); err != nil {
		return lifecycle.Page{}, fmt.Errorf("count cells: %w", err)
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM robot_cells`+where+` ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return lifecycle.Page{}, fmt.Errorf("list cells: %w", err)
	}
	defer rows.Close()
	items := make([]lifecycle.RobotCell, 0, size)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return lifecycle.Page{}, err
		}
		cell, err := getCell(ctx, s.db, id)
		if err != nil {
			return lifecycle.Page{}, err
		}
		items = append(items, cell)
	}
	return lifecycle.Page{Items: items, Total: total, Page: page, PageSize: size}, rows.Err()
}

func (s *Store) TransitionCell(ctx context.Context, principal identity.Principal, id int64, expected int64, next lifecycle.CellStatus, reason, requestID string, now time.Time) (lifecycle.RobotCell, error) {
	var updated lifecycle.RobotCell
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		cell, err := getCell(ctx, tx, id)
		if err != nil {
			return err
		}
		if cell.Version != expected {
			return apperr.New(apperr.ErrVersion, "store.transition_cell", "robot cell changed concurrently")
		}
		if !cell.CanTransition(next) {
			return apperr.New(apperr.ErrInvalid, "store.transition_cell", fmt.Sprintf("transition %s to %s is not allowed", cell.Status, next))
		}
		result, err := tx.ExecContext(ctx, `UPDATE robot_cells SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, next, encodeTime(now), id, expected)
		if err != nil {
			return fmt.Errorf("update cell state: %w", err)
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return apperr.New(apperr.ErrVersion, "store.transition_cell", "robot cell changed concurrently")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO cell_transitions(cell_id,from_status,to_status,actor_id,reason,request_id,occurred_at) VALUES(?,?,?,?,?,?,?)`, id, cell.Status, next, principal.UserID, reason, requestID, encodeTime(now)); err != nil {
			return fmt.Errorf("record transition: %w", err)
		}
		event, err := audit.New(principal.UserID, string(principal.Role), "cell.transition", "robot_cell", strconv.FormatInt(id, 10), requestID, audit.ResultSucceeded, map[string]any{"from": cell.Status, "to": next, "reason": reason}, now)
		if err != nil {
			return err
		}
		if _, err = appendAudit(ctx, tx, event); err != nil {
			return err
		}
		updated, err = getCell(ctx, tx, id)
		return err
	})
	if err != nil {
		return lifecycle.RobotCell{}, err
	}
	return updated, nil
}

func (s *Store) RecordInspection(ctx context.Context, principal identity.Principal, item lifecycle.Inspection, requestID string) (lifecycle.RobotCell, error) {
	var updated lifecycle.RobotCell
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		cell, err := getCell(ctx, tx, item.CellID)
		if err != nil {
			return err
		}
		if cell.Status != lifecycle.CellSafetyReview {
			return apperr.New(apperr.ErrInvalid, "store.record_inspection", "cell is not awaiting review")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inspections(cell_id,kind,passed,inspector_id,notes,recorded_at) VALUES(?,?,?,?,?,?)`, item.CellID, item.Kind, boolInt(item.Passed), item.InspectorID, item.Notes, encodeTime(item.RecordedAt)); err != nil {
			return fmt.Errorf("insert inspection: %w", err)
		}
		field := "safety_passed"
		if item.Kind == lifecycle.InspectionQuality {
			field = "quality_passed"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE robot_cells SET `+field+`=?,version=version+1,updated_at=? WHERE id=?`, boolInt(item.Passed), encodeTime(item.RecordedAt), item.CellID); err != nil {
			return fmt.Errorf("apply inspection: %w", err)
		}
		event, err := audit.New(principal.UserID, string(principal.Role), "inspection.record", "robot_cell", strconv.FormatInt(item.CellID, 10), requestID, audit.ResultSucceeded, map[string]any{"kind": item.Kind, "passed": item.Passed}, item.RecordedAt)
		if err != nil {
			return err
		}
		if _, err = appendAudit(ctx, tx, event); err != nil {
			return err
		}
		updated, err = getCell(ctx, tx, item.CellID)
		return err
	})
	return updated, err
}
