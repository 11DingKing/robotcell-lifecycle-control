package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/audit"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
)

func (s *Store) CreateWindow(ctx context.Context, window schedule.WorkWindow) (schedule.WorkWindow, error) {
	if err := window.Validate(); err != nil {
		return schedule.WorkWindow{}, apperr.Wrap(apperr.ErrInvalid, "store.create_window", "invalid work window", err)
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO work_windows(cell_id,workstation_id,tool_id,qualified_user_id,starts_at,ends_at,status,purpose,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,?,?)`, window.CellID, window.WorkstationID, window.ToolID, window.QualifiedUserID, encodeTime(window.StartsAt), encodeTime(window.EndsAt), window.Status, window.Purpose, encodeTime(now), encodeTime(now))
	if err != nil {
		return schedule.WorkWindow{}, fmt.Errorf("insert window: %w", err)
	}
	window.ID, err = result.LastInsertId()
	window.Version = 1
	window.CreatedAt = now
	window.UpdatedAt = now
	return window, err
}

func getWindow(ctx context.Context, q DBTX, id int64) (schedule.WorkWindow, error) {
	var w schedule.WorkWindow
	var starts, ends, created, updated string
	err := q.QueryRowContext(ctx, `SELECT id,cell_id,workstation_id,tool_id,qualified_user_id,starts_at,ends_at,status,purpose,version,created_at,updated_at FROM work_windows WHERE id=?`, id).Scan(&w.ID, &w.CellID, &w.WorkstationID, &w.ToolID, &w.QualifiedUserID, &starts, &ends, &w.Status, &w.Purpose, &w.Version, &created, &updated)
	if err == sql.ErrNoRows {
		return schedule.WorkWindow{}, apperr.Wrap(apperr.ErrNotFound, "store.get_window", "work window not found", err)
	}
	if err != nil {
		return schedule.WorkWindow{}, fmt.Errorf("scan window: %w", err)
	}
	var parseErr error
	for target, value := range map[*time.Time]string{&w.StartsAt: starts, &w.EndsAt: ends, &w.CreatedAt: created, &w.UpdatedAt: updated} {
		*target, parseErr = decodeTime(value)
		if parseErr != nil {
			return schedule.WorkWindow{}, parseErr
		}
	}
	return w, nil
}

func (s *Store) GetWindow(ctx context.Context, id int64) (schedule.WorkWindow, error) {
	return getWindow(ctx, s.db, id)
}

func (s *Store) ApproveAndReserveWindow(ctx context.Context, principal identity.Principal, id, expected int64, qualification string, requestID string, now time.Time) (schedule.WorkWindow, error) {
	var updated schedule.WorkWindow
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		window, err := getWindow(ctx, tx, id)
		if err != nil {
			return err
		}
		if window.Version != expected {
			return apperr.New(apperr.ErrVersion, "store.reserve_window", "window changed concurrently")
		}
		if !window.CanTransition(schedule.WindowApproved) {
			return apperr.New(apperr.ErrInvalid, "store.reserve_window", "window cannot be approved")
		}
		var qualified int
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM qualifications WHERE user_id=? AND kind=? AND revoked_at IS NULL AND valid_from<=? AND valid_to>?`, window.QualifiedUserID, qualification, encodeTime(window.StartsAt), encodeTime(window.EndsAt)).Scan(&qualified)
		if err != nil {
			return fmt.Errorf("check qualification: %w", err)
		}
		if qualified == 0 {
			return apperr.New(apperr.ErrForbidden, "store.reserve_window", "assigned person lacks a valid qualification")
		}
		var toolActive int
		var due string
		if err = tx.QueryRowContext(ctx, `SELECT active,calibration_due FROM tools WHERE id=?`, window.ToolID).Scan(&toolActive, &due); err != nil {
			return fmt.Errorf("read tool: %w", err)
		}
		calibrationDue, err := decodeTime(due)
		if err != nil {
			return err
		}
		if toolActive != 1 || calibrationDue.Before(window.EndsAt) {
			return apperr.New(apperr.ErrConflict, "store.reserve_window", "tool is unavailable or calibration expires during the window")
		}
		resources := []struct {
			kind schedule.ResourceKind
			id   int64
		}{{schedule.ResourceWorkstation, window.WorkstationID}, {schedule.ResourceTool, window.ToolID}, {schedule.ResourcePerson, window.QualifiedUserID}}
		for _, resource := range resources {
			_, err = tx.ExecContext(ctx, `INSERT INTO resource_reservations(window_id,resource_kind,resource_id,starts_at,ends_at,released_at) VALUES(?,?,?,?,?,NULL)`, window.ID, resource.kind, resource.id, encodeTime(window.StartsAt), encodeTime(window.EndsAt))
			if err != nil {
				if isConflict(err) {
					return apperr.Wrap(apperr.ErrConflict, "store.reserve_window", "resource is already occupied", err)
				}
				return fmt.Errorf("reserve %s: %w", resource.kind, err)
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE work_windows SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, schedule.WindowReserved, encodeTime(now), id, expected)
		if err != nil {
			return fmt.Errorf("update window: %w", err)
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return apperr.New(apperr.ErrVersion, "store.reserve_window", "window changed concurrently")
		}
		event, err := audit.New(principal.UserID, string(principal.Role), "window.reserve", "work_window", strconv.FormatInt(id, 10), requestID, audit.ResultSucceeded, map[string]any{"resources": resources, "starts_at": window.StartsAt, "ends_at": window.EndsAt}, now)
		if err != nil {
			return err
		}
		if _, err = appendAudit(ctx, tx, event); err != nil {
			return err
		}
		updated, err = getWindow(ctx, tx, id)
		return err
	})
	return updated, err
}

func (s *Store) CancelWindow(ctx context.Context, principal identity.Principal, id, expected int64, reason, requestID string, now time.Time) (schedule.WorkWindow, error) {
	var updated schedule.WorkWindow
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		window, err := getWindow(ctx, tx, id)
		if err != nil {
			return err
		}
		if window.Version != expected {
			return apperr.New(apperr.ErrVersion, "store.cancel_window", "window changed concurrently")
		}
		if !window.CanTransition(schedule.WindowCancelled) {
			return apperr.New(apperr.ErrInvalid, "store.cancel_window", "window cannot be cancelled")
		}
		if _, err = tx.ExecContext(ctx, `UPDATE resource_reservations SET released_at=? WHERE window_id=? AND released_at IS NULL`, encodeTime(now), id); err != nil {
			return fmt.Errorf("release reservations: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE work_windows SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, schedule.WindowCancelled, encodeTime(now), id, expected)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return apperr.New(apperr.ErrVersion, "store.cancel_window", "window changed concurrently")
		}
		event, err := audit.New(principal.UserID, string(principal.Role), "window.cancel", "work_window", strconv.FormatInt(id, 10), requestID, audit.ResultSucceeded, map[string]string{"reason": strings.TrimSpace(reason)}, now)
		if err != nil {
			return err
		}
		if _, err = appendAudit(ctx, tx, event); err != nil {
			return err
		}
		updated, err = getWindow(ctx, tx, id)
		return err
	})
	return updated, err
}

func (s *Store) CountActiveReservations(ctx context.Context, windowID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_reservations WHERE window_id=? AND released_at IS NULL`, windowID).Scan(&count)
	return count, err
}
