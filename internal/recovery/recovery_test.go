package recovery_test

import (
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
)

func TestJobValidation(t *testing.T) {
	base := recovery.Job{Kind: "calibration_compensation", ObjectType: "robot_cell", ObjectID: 12, IdempotencyKey: "calibration:12:key", MaxAttempts: 5}
	tests := []struct {
		name   string
		change func(*recovery.Job)
		valid  bool
	}{
		{"valid", func(*recovery.Job) {}, true},
		{"missing kind", func(j *recovery.Job) { j.Kind = "" }, false},
		{"missing object type", func(j *recovery.Job) { j.ObjectType = "" }, false},
		{"missing object id", func(j *recovery.Job) { j.ObjectID = 0 }, false},
		{"negative object id", func(j *recovery.Job) { j.ObjectID = -1 }, false},
		{"missing idempotency", func(j *recovery.Job) { j.IdempotencyKey = "" }, false},
		{"zero attempts", func(j *recovery.Job) { j.MaxAttempts = 0 }, false},
		{"negative attempts", func(j *recovery.Job) { j.MaxAttempts = -2 }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := base
			test.change(&job)
			err := job.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected valid job: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected invalid job")
			}
		})
	}
}

func TestJobClaimability(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	future := now.Add(time.Second)
	tests := []struct {
		name string
		job  recovery.Job
		want bool
	}{
		{"pending due", recovery.Job{Status: recovery.Pending, NextAttemptAt: now}, true},
		{"pending overdue", recovery.Job{Status: recovery.Pending, NextAttemptAt: past}, true},
		{"pending future", recovery.Job{Status: recovery.Pending, NextAttemptAt: future}, false},
		{"retry due", recovery.Job{Status: recovery.RetryWait, NextAttemptAt: now}, true},
		{"retry future", recovery.Job{Status: recovery.RetryWait, NextAttemptAt: future}, false},
		{"running expired lease", recovery.Job{Status: recovery.Running, LeaseUntil: &past}, true},
		{"running lease boundary", recovery.Job{Status: recovery.Running, LeaseUntil: &now}, true},
		{"running active lease", recovery.Job{Status: recovery.Running, LeaseUntil: &future}, false},
		{"running missing lease", recovery.Job{Status: recovery.Running}, false},
		{"succeeded", recovery.Job{Status: recovery.Succeeded, NextAttemptAt: past}, false},
		{"permanent failed", recovery.Job{Status: recovery.PermanentFailed, NextAttemptAt: past}, false},
		{"cancelled", recovery.Job{Status: recovery.Cancelled, NextAttemptAt: past}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.job.ClaimableAt(now); got != test.want {
				t.Fatalf("ClaimableAt = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBackoffIsBoundedExponential(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{-5, time.Second},
		{0, time.Second},
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
		{7, 64 * time.Second},
		{8, 128 * time.Second},
		{9, 128 * time.Second},
		{100, 128 * time.Second},
	}
	for _, test := range tests {
		if got := recovery.Backoff(test.attempt); got != test.want {
			t.Errorf("Backoff(%d)=%s, want %s", test.attempt, got, test.want)
		}
	}
}
