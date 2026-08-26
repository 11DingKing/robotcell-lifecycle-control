package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
)

func (s *Store) CreateRecoveryJob(ctx context.Context, job recovery.Job) (recovery.Job, error) {
	if err := job.Validate(); err != nil {
		return recovery.Job{}, apperr.Wrap(apperr.ErrInvalid, "store.create_recovery", "invalid recovery job", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO recovery_jobs(kind,object_type,object_id,idempotency_key,payload,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,0,?,?,'',NULL,'',?,?)`, job.Kind, job.ObjectType, job.ObjectID, job.IdempotencyKey, job.Payload, recovery.Pending, job.MaxAttempts, encodeTime(job.NextAttemptAt), encodeTime(job.CreatedAt), encodeTime(job.UpdatedAt))
	if err != nil {
		if isConflict(err) {
			existing, findErr := s.FindRecoveryByKey(ctx, job.IdempotencyKey)
			if findErr == nil {
				return existing, nil
			}
		}
		return recovery.Job{}, fmt.Errorf("insert recovery job: %w", err)
	}
	job.ID, err = result.LastInsertId()
	job.Status = recovery.Pending
	return job, err
}

func scanJob(row rowScanner) (recovery.Job, error) {
	var j recovery.Job
	var next, lease, created, updated string
	var nullableLease sql.NullString
	err := row.Scan(&j.ID, &j.Kind, &j.ObjectType, &j.ObjectID, &j.IdempotencyKey, &j.Payload, &j.Status, &j.Attempts, &j.MaxAttempts, &next, &j.LeaseOwner, &nullableLease, &j.LastError, &created, &updated)
	if err == sql.ErrNoRows {
		return recovery.Job{}, apperr.Wrap(apperr.ErrNotFound, "store.scan_recovery", "recovery job not found", err)
	}
	if err != nil {
		return recovery.Job{}, err
	}
	var parseErr error
	if j.NextAttemptAt, parseErr = decodeTime(next); parseErr != nil {
		return recovery.Job{}, parseErr
	}
	if nullableLease.Valid {
		lease = nullableLease.String
		t, err := decodeTime(lease)
		if err != nil {
			return recovery.Job{}, err
		}
		j.LeaseUntil = &t
	}
	if j.CreatedAt, parseErr = decodeTime(created); parseErr != nil {
		return recovery.Job{}, parseErr
	}
	if j.UpdatedAt, parseErr = decodeTime(updated); parseErr != nil {
		return recovery.Job{}, parseErr
	}
	return j, nil
}

func (s *Store) FindRecoveryByKey(ctx context.Context, key string) (recovery.Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT id,kind,object_type,object_id,idempotency_key,payload,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,created_at,updated_at FROM recovery_jobs WHERE idempotency_key=?`, key))
}

func (s *Store) ClaimRecoveryJob(ctx context.Context, owner string, now time.Time, lease time.Duration) (recovery.Job, error) {
	var claimed recovery.Job
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `SELECT id,kind,object_type,object_id,idempotency_key,payload,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,created_at,updated_at FROM recovery_jobs WHERE ((status IN ('pending','retry_wait') AND next_attempt_at<=?) OR (status='running' AND lease_until<=?)) ORDER BY next_attempt_at,id LIMIT 1`, encodeTime(now), encodeTime(now))
		job, err := scanJob(row)
		if err != nil {
			return err
		}
		until := now.Add(lease)
		result, err := tx.ExecContext(ctx, `UPDATE recovery_jobs SET status='running',attempts=attempts+1,lease_owner=?,lease_until=?,updated_at=? WHERE id=? AND ((status IN ('pending','retry_wait') AND next_attempt_at<=?) OR (status='running' AND lease_until<=?))`, owner, encodeTime(until), encodeTime(now), job.ID, encodeTime(now), encodeTime(now))
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return apperr.New(apperr.ErrConflict, "store.claim_recovery", "recovery job was claimed concurrently")
		}
		claimed, err = scanJob(tx.QueryRowContext(ctx, `SELECT id,kind,object_type,object_id,idempotency_key,payload,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,created_at,updated_at FROM recovery_jobs WHERE id=?`, job.ID))
		return err
	})
	return claimed, err
}

func (s *Store) CompleteRecoveryJob(ctx context.Context, id int64, owner string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE recovery_jobs SET status='succeeded',lease_owner='',lease_until=NULL,last_error='',updated_at=? WHERE id=? AND status='running' AND lease_owner=?`, encodeTime(now), id, owner)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return apperr.New(apperr.ErrConflict, "store.complete_recovery", "worker no longer owns recovery job")
	}
	return nil
}

func (s *Store) YieldRecoveryLease(ctx context.Context, id int64, owner string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE recovery_jobs SET status='retry_wait',next_attempt_at=?,lease_owner='',lease_until=NULL,updated_at=? WHERE id=? AND status='running' AND lease_owner=?`, encodeTime(now), encodeTime(now), id, owner)
	if err != nil {
		return fmt.Errorf("yield recovery lease: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read yielded lease result: %w", err)
	}
	if count != 1 {
		return apperr.New(apperr.ErrConflict, "store.yield_recovery", "worker no longer owns recovery job")
	}
	return nil
}

func (s *Store) CompletionIsExternallyVisible(kind string) bool {
	return kind == "retirement_cleanup"
}

func (s *Store) FailRecoveryJob(ctx context.Context, job recovery.Job, owner string, cause error, now time.Time) error {
	status := recovery.RetryWait
	next := now.Add(recovery.Backoff(job.Attempts))
	if job.Attempts >= job.MaxAttempts {
		status = recovery.PermanentFailed
		next = now
	}
	result, err := s.db.ExecContext(ctx, `UPDATE recovery_jobs SET status=?,next_attempt_at=?,lease_owner='',lease_until=NULL,last_error=?,updated_at=? WHERE id=? AND status='running' AND lease_owner=?`, status, encodeTime(next), cause.Error(), encodeTime(now), job.ID, owner)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return apperr.New(apperr.ErrConflict, "store.fail_recovery", "worker no longer owns recovery job")
	}
	return nil
}

func (s *Store) CancelRecoveryJob(ctx context.Context, id int64, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE recovery_jobs SET status='cancelled',lease_owner='',lease_until=NULL,updated_at=? WHERE id=? AND status IN ('pending','retry_wait')`, encodeTime(now), id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return apperr.New(apperr.ErrConflict, "store.cancel_recovery", "recovery job cannot be cancelled")
	}
	return nil
}

type IdempotencyRecord struct {
	Scope, Key, RequestHash string
	Status                  int
	Body                    []byte
	ExpiresAt               time.Time
}

func (s *Store) GetIdempotency(ctx context.Context, scope, key string, now time.Time) (IdempotencyRecord, error) {
	var record IdempotencyRecord
	var expires string
	err := s.db.QueryRowContext(ctx, `SELECT scope,idempotency_key,request_hash,response_status,response_body,expires_at FROM idempotency_records WHERE scope=? AND idempotency_key=? AND expires_at>?`, scope, key, encodeTime(now)).Scan(&record.Scope, &record.Key, &record.RequestHash, &record.Status, &record.Body, &expires)
	if err == sql.ErrNoRows {
		return IdempotencyRecord{}, apperr.New(apperr.ErrNotFound, "store.get_idempotency", "idempotency record not found")
	}
	if err != nil {
		return IdempotencyRecord{}, err
	}
	record.ExpiresAt, err = decodeTime(expires)
	return record, err
}

func (s *Store) PutIdempotency(ctx context.Context, record IdempotencyRecord, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO idempotency_records(scope,idempotency_key,request_hash,response_status,response_body,expires_at,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(scope,idempotency_key) DO NOTHING`, record.Scope, record.Key, record.RequestHash, record.Status, record.Body, encodeTime(record.ExpiresAt), encodeTime(now))
	return err
}

func (s *Store) RecoveryCounts(ctx context.Context) (map[recovery.Status]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status,COUNT(*) FROM recovery_jobs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[recovery.Status]int{}
	for rows.Next() {
		var status recovery.Status
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

func IsNoRecoveryJob(err error) bool {
	return err != nil && (sql.ErrNoRows == err || apperr.PublicCode(err) == "NOT_FOUND")
}
