package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/audit"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/lifecycle"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
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

// StageInspectionEvidence persists field evidence before the inspection result
// is applied to a robot cell. The caller coordinates the later state change.
func (s *Store) StageInspectionEvidence(ctx context.Context, item lifecycle.Inspection) error {
	if item.CellID <= 0 || item.InspectorID <= 0 || item.RecordedAt.IsZero() {
		return apperr.New(apperr.ErrInvalid, "store.stage_inspection", "complete inspection identity is required")
	}
	if item.Kind != lifecycle.InspectionSafety && item.Kind != lifecycle.InspectionQuality {
		return apperr.New(apperr.ErrInvalid, "store.stage_inspection", "inspection kind is invalid")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := getCell(ctx, tx, item.CellID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO inspections(cell_id,kind,passed,inspector_id,notes,recorded_at) VALUES(?,?,?,?,?,?)`, item.CellID, item.Kind, boolInt(item.Passed), item.InspectorID, item.Notes, encodeTime(item.RecordedAt))
		if err != nil {
			return fmt.Errorf("stage inspection evidence: %w", err)
		}
		return nil
	})
}

func (s *Store) RetireCell(ctx context.Context, principal identity.Principal, id, expected int64, reason, requestID string, now time.Time) (lifecycle.RobotCell, error) {
	var updated lifecycle.RobotCell
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		cell, err := getCell(ctx, tx, id)
		if err != nil {
			return err
		}
		if cell.Version != expected {
			return apperr.New(apperr.ErrVersion, "store.retire_cell", "robot cell changed concurrently")
		}
		if !cell.CanTransition(lifecycle.CellRetired) {
			return apperr.New(apperr.ErrInvalid, "store.retire_cell", "robot cell is not eligible for retirement")
		}
		var activeMaintenance int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM maintenance_orders WHERE cell_id=? AND status NOT IN (?,?)`, id, maintenance.Closed, maintenance.Cancelled).Scan(&activeMaintenance); err != nil {
			return fmt.Errorf("check active maintenance: %w", err)
		}
		if activeMaintenance > 0 {
			return apperr.New(apperr.ErrConflict, "store.retire_cell", "active maintenance orders must be closed or cancelled")
		}
		var activeReservations int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_reservations r JOIN work_windows w ON w.id=r.window_id WHERE w.cell_id=? AND r.released_at IS NULL`, id).Scan(&activeReservations); err != nil {
			return fmt.Errorf("check active reservations: %w", err)
		}
		if activeReservations > 0 {
			return apperr.New(apperr.ErrConflict, "store.retire_cell", "active resource reservations must be released")
		}
		if cell.BatchID != nil {
			var batchStatus lifecycle.BatchStatus
			if err = tx.QueryRowContext(ctx, `SELECT status FROM production_batches WHERE id=?`, *cell.BatchID).Scan(&batchStatus); err != nil {
				return fmt.Errorf("read production batch: %w", err)
			}
			if batchStatus == lifecycle.BatchActive {
				return apperr.New(apperr.ErrConflict, "store.retire_cell", "active production batch still references robot cell")
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE robot_cells SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, lifecycle.CellRetired, encodeTime(now), id, expected)
		if err != nil {
			return fmt.Errorf("retire robot cell: %w", err)
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return apperr.New(apperr.ErrVersion, "store.retire_cell", "robot cell changed concurrently")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO cell_transitions(cell_id,from_status,to_status,actor_id,reason,request_id,occurred_at) VALUES(?,?,?,?,?,?,?)`, id, cell.Status, lifecycle.CellRetired, principal.UserID, reason, requestID, encodeTime(now)); err != nil {
			return fmt.Errorf("record retirement transition: %w", err)
		}
		event, err := audit.New(principal.UserID, string(principal.Role), "cell.retire", "robot_cell", strconv.FormatInt(id, 10), requestID, audit.ResultSucceeded, map[string]any{"from": cell.Status, "to": lifecycle.CellRetired}, now)
		if err != nil {
			return err
		}
		if _, err = appendAudit(ctx, tx, event); err != nil {
			return err
		}
		updated, err = getCell(ctx, tx, id)
		return err
	})
	return updated, err
}

func (s *Store) CompensateCalibration(ctx context.Context, job recovery.Job, now time.Time) error {
	var payload struct {
		CellID  int64  `json:"cell_id"`
		Reason  string `json:"reason"`
		ActorID int64  `json:"actor_id"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return apperr.Wrap(apperr.ErrInvalid, "store.compensate_calibration", "invalid recovery payload", err)
	}
	if payload.CellID != job.ObjectID || payload.ActorID <= 0 || payload.Reason == "" {
		return apperr.New(apperr.ErrInvalid, "store.compensate_calibration", "recovery payload does not identify the failed calibration")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		cell, err := getCell(ctx, tx, payload.CellID)
		if err != nil {
			return err
		}
		if cell.Status == lifecycle.CellInstalling {
			return nil
		}
		if cell.Status != lifecycle.CellCalibrating {
			return apperr.New(apperr.ErrConflict, "store.compensate_calibration", "robot cell moved beyond the recoverable calibration state")
		}
		var actorRole identity.Role
		if err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=? AND active=1`, payload.ActorID).Scan(&actorRole); err != nil {
			return fmt.Errorf("read recovery actor: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE robot_cells SET status=?,calibration_ref='',version=version+1,updated_at=? WHERE id=? AND status=? AND version=?`, lifecycle.CellInstalling, encodeTime(now), cell.ID, lifecycle.CellCalibrating, cell.Version)
		if err != nil {
			return fmt.Errorf("apply calibration compensation: %w", err)
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return apperr.New(apperr.ErrVersion, "store.compensate_calibration", "robot cell changed during compensation")
		}
		requestID := fmt.Sprintf("recovery-job-%d", job.ID)
		if _, err = tx.ExecContext(ctx, `INSERT INTO cell_transitions(cell_id,from_status,to_status,actor_id,reason,request_id,occurred_at) VALUES(?,?,?,?,?,?,?)`, cell.ID, cell.Status, lifecycle.CellInstalling, payload.ActorID, "calibration compensation: "+payload.Reason, requestID, encodeTime(now)); err != nil {
			return fmt.Errorf("record compensation transition: %w", err)
		}
		event, err := audit.New(payload.ActorID, string(actorRole), "recovery.calibration_compensate", "robot_cell", strconv.FormatInt(cell.ID, 10), requestID, audit.ResultSucceeded, map[string]any{"job_id": job.ID, "reason": payload.Reason}, now)
		if err != nil {
			return err
		}
		_, err = appendAudit(ctx, tx, event)
		return err
	})
}
