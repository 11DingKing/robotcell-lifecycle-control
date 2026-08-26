package worker_test

import (
	"context"
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

func TestSuccessfulHandlerRetainsLeaseUntilCompletion(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "lease.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	job, err := database.CreateRecoveryJob(ctx, recovery.Job{Kind: "retirement_cleanup", ObjectType: "robot_cell", ObjectID: 7, IdempotencyKey: "lease-success", Payload: []byte(`{}`), MaxAttempts: 3, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	instance := worker.New(database, clock.NewManual(now), slog.New(slog.NewTextHandler(io.Discard, nil)), "worker-a", time.Second, time.Minute)
	var calls atomic.Int32
	instance.Register(job.Kind, func(context.Context, recovery.Job) error { calls.Add(1); return nil })
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	instance.Run(runCtx)
	cancel()
	stored, err := database.FindRecoveryByKey(ctx, job.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || stored.Status != recovery.Succeeded || stored.LeaseOwner != "" {
		t.Fatalf("successful work was not finalized: calls=%d status=%s owner=%q", calls.Load(), stored.Status, stored.LeaseOwner)
	}
}
