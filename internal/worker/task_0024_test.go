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

func TestTimedOutHandlerFinishesBeforeRecoveryRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, 8, 27, 3, 0, 0, 0, time.UTC)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "worker-overlap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	job, err := db.CreateRecoveryJob(ctx, recovery.Job{Kind: "retirement_cleanup", ObjectType: "robot_cell", ObjectID: 24, IdempotencyKey: "task-24-overlap", Payload: []byte(`{}`), MaxAttempts: 3, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	lease := 20 * time.Millisecond
	instance := worker.New(db, clock.NewManual(now), slog.New(slog.NewTextHandler(io.Discard, nil)), "worker-24", time.Millisecond, lease)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	var calls atomic.Int32
	instance.Register(job.Kind, func(context.Context, recovery.Job) error {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			<-releaseFirst
		case 2:
			close(secondStarted)
		}
		return nil
	})
	done := make(chan struct{})
	go func() {
		instance.Run(ctx)
		close(done)
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first recovery handler did not start")
	}
	overlapped := false
	select {
	case <-secondStarted:
		overlapped = true
	case <-time.After(4 * lease):
	}
	close(releaseFirst)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if overlapped {
		t.Fatalf("same recovery job ran concurrently; handler calls=%d", calls.Load())
	}
}
