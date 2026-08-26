package worker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/worker"
)

func workerStore(t *testing.T) (*store.Store, *clock.Manual) {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database, clock.NewManual(time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))
}

func createJob(t *testing.T, database *store.Store, now time.Time, key, kind string, attempts int) recovery.Job {
	t.Helper()
	job, err := database.CreateRecoveryJob(context.Background(), recovery.Job{Kind: kind, ObjectType: "robot_cell", ObjectID: 1, IdempotencyKey: key, Payload: []byte(`{}`), MaxAttempts: attempts, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestWorkerCompletesRegisteredRecoveryJob(t *testing.T) {
	database, manual := workerStore(t)
	job := createJob(t, database, manual.Now(), "worker-success", "calibration_compensation", 3)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	instance := worker.New(database, manual, logger, "worker-success", time.Millisecond, time.Second)
	var calls atomic.Int32
	instance.Register(job.Kind, func(ctx context.Context, received recovery.Job) error {
		calls.Add(1)
		if received.ID != job.ID {
			t.Errorf("received job %d want %d", received.ID, job.ID)
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	instance.Run(ctx)
	stored, err := database.FindRecoveryByKey(context.Background(), job.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || stored.Status != recovery.Succeeded || stored.Attempts != 1 {
		t.Fatalf("calls=%d stored=%#v", calls.Load(), stored)
	}
}

func TestWorkerRetriesFailedJobAndHonorsBackoff(t *testing.T) {
	database, manual := workerStore(t)
	job := createJob(t, database, manual.Now(), "worker-retry", "calibration_compensation", 3)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	instance := worker.New(database, manual, logger, "worker-retry", time.Millisecond, time.Second)
	instance.Register(job.Kind, func(context.Context, recovery.Job) error { return errors.New("controller unavailable") })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	instance.Run(ctx)
	cancel()
	stored, err := database.FindRecoveryByKey(context.Background(), job.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != recovery.RetryWait || stored.Attempts != 1 || stored.LastError != "controller unavailable" {
		t.Fatalf("unexpected retry state: %#v", stored)
	}
	if !stored.NextAttemptAt.Equal(manual.Now().Add(time.Second)) {
		t.Fatalf("next attempt = %v", stored.NextAttemptAt)
	}
}

func TestWorkerMarksMissingHandlerAsRetry(t *testing.T) {
	database, manual := workerStore(t)
	job := createJob(t, database, manual.Now(), "missing-handler", "unregistered_kind", 2)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	instance := worker.New(database, manual, logger, "worker-missing", time.Millisecond, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	instance.Run(ctx)
	cancel()
	stored, err := database.FindRecoveryByKey(context.Background(), job.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != recovery.RetryWait || stored.Attempts != 1 || stored.LastError == "" {
		t.Fatalf("unexpected missing-handler state: %#v", stored)
	}
}

func TestWorkerStopsWhenContextIsCancelled(t *testing.T) {
	database, manual := workerStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	instance := worker.New(database, manual, logger, "worker-stop", time.Millisecond, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		instance.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
	instance.Wait()
}
