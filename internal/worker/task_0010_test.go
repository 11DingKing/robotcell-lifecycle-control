package worker_test

import (
 "context"
 "io"
 "log/slog"
 "testing"
 "time"
 "github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
 "github.com/11DingKing/robotcell-lifecycle-control/internal/worker"
)

func TestWorkerPersistsSuccessfulHandlerCompletion(t *testing.T) {
 database, manual := workerStore(t)
 job := createJob(t, database, manual.Now(), "completion-error", "calibration_compensation", 2)
 instance := worker.New(database, manual, slog.New(slog.NewTextHandler(io.Discard,nil)), "completion", time.Millisecond, time.Second)
 instance.Register(job.Kind, func(context.Context, recovery.Job) error { return nil })
 ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond); defer cancel()
 instance.Run(ctx)
 stored, err := database.FindRecoveryByKey(context.Background(), job.IdempotencyKey)
 if err != nil { t.Fatal(err) }
 if stored.Status != recovery.Succeeded { t.Fatalf("successful handler not completed: %#v", stored) }
}
