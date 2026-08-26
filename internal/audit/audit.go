package audit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Result string

const (
	ResultSucceeded Result = "succeeded"
	ResultRejected  Result = "rejected"
	ResultFailed    Result = "failed"
)

type Event struct {
	ID           int64           `json:"id"`
	ActorID      int64           `json:"actor_id"`
	ActorRole    string          `json:"actor_role"`
	Action       string          `json:"action"`
	ObjectType   string          `json:"object_type"`
	ObjectID     string          `json:"object_id"`
	Result       Result          `json:"result"`
	RequestID    string          `json:"request_id"`
	Details      json.RawMessage `json:"details"`
	OccurredAt   time.Time       `json:"occurred_at"`
	PreviousHash string          `json:"previous_hash"`
	EventHash    string          `json:"event_hash"`
}

func New(actorID int64, role, action, objectType, objectID, requestID string, result Result, details any, at time.Time) (Event, error) {
	if actorID <= 0 || strings.TrimSpace(action) == "" || strings.TrimSpace(objectType) == "" || strings.TrimSpace(objectID) == "" {
		return Event{}, fmt.Errorf("audit actor, action and object identity are required")
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return Event{}, fmt.Errorf("encode audit details: %w", err)
	}
	return Event{ActorID: actorID, ActorRole: role, Action: action, ObjectType: objectType, ObjectID: objectID, Result: result, RequestID: requestID, Details: payload, OccurredAt: at.UTC()}, nil
}
