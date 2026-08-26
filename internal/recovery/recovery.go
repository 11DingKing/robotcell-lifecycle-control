package recovery

import (
	"fmt"
	"time"
)

type Status string

const (
	Pending         Status = "pending"
	Running         Status = "running"
	RetryWait       Status = "retry_wait"
	Succeeded       Status = "succeeded"
	PermanentFailed Status = "permanent_failed"
	Cancelled       Status = "cancelled"
)

type Job struct {
	ID             int64      `json:"id"`
	Kind           string     `json:"kind"`
	ObjectType     string     `json:"object_type"`
	ObjectID       int64      `json:"object_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	Payload        []byte     `json:"payload"`
	Status         Status     `json:"status"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	LeaseOwner     string     `json:"lease_owner"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
	LastError      string     `json:"last_error"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (j Job) Validate() error {
	if j.Kind == "" || j.ObjectType == "" || j.ObjectID <= 0 || j.IdempotencyKey == "" {
		return fmt.Errorf("recovery job identity is incomplete")
	}
	if j.MaxAttempts <= 0 {
		return fmt.Errorf("max attempts must be positive")
	}
	return nil
}

func (j Job) ClaimableAt(now time.Time) bool {
	if j.Status != Pending && j.Status != RetryWait && j.Status != Running {
		return false
	}
	if j.Status == Running {
		return j.LeaseUntil != nil && !j.LeaseUntil.After(now)
	}
	return !j.NextAttemptAt.After(now)
}

func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}
