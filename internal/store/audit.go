package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/audit"
)

func appendAudit(ctx context.Context, q DBTX, event audit.Event) (audit.Event, error) {
	var previous string
	err := q.QueryRowContext(ctx, `SELECT event_hash FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return audit.Event{}, fmt.Errorf("read audit head: %w", err)
	}
	event.PreviousHash = previous
	hash := sha256.New()
	for _, value := range []string{previous, strconv.FormatInt(event.ActorID, 10), event.ActorRole, event.Action, event.ObjectType, event.ObjectID, string(event.Result), event.RequestID, string(event.Details), encodeTime(event.OccurredAt)} {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	event.EventHash = hex.EncodeToString(hash.Sum(nil))
	result, err := q.ExecContext(ctx, `INSERT INTO audit_events(actor_id,actor_role,action,object_type,object_id,result,request_id,details,occurred_at,previous_hash,event_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, event.ActorID, event.ActorRole, event.Action, event.ObjectType, event.ObjectID, event.Result, event.RequestID, []byte(event.Details), encodeTime(event.OccurredAt), event.PreviousHash, event.EventHash)
	if err != nil {
		return audit.Event{}, fmt.Errorf("append audit: %w", err)
	}
	event.ID, err = result.LastInsertId()
	if err != nil {
		return audit.Event{}, fmt.Errorf("read audit id: %w", err)
	}
	return event, nil
}

func (s *Store) AppendAudit(ctx context.Context, event audit.Event) (audit.Event, error) {
	return appendAudit(ctx, s.db, event)
}

func (s *Store) ListAudit(ctx context.Context, objectType, objectID string, limit int) ([]audit.Event, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor_id,actor_role,action,object_type,object_id,result,request_id,details,occurred_at,previous_hash,event_hash FROM audit_events WHERE object_type=? AND object_id=? ORDER BY id DESC LIMIT ?`, objectType, objectID, limit)
	if err != nil {
		return nil, fmt.Errorf("query audit: %w", err)
	}
	defer rows.Close()
	items := make([]audit.Event, 0)
	for rows.Next() {
		var event audit.Event
		var occurred string
		if err := rows.Scan(&event.ID, &event.ActorID, &event.ActorRole, &event.Action, &event.ObjectType, &event.ObjectID, &event.Result, &event.RequestID, &event.Details, &occurred, &event.PreviousHash, &event.EventHash); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		if event.OccurredAt, err = decodeTime(occurred); err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	return items, rows.Err()
}
