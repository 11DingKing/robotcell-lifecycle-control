package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/apperr"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
)

type Handler func(context.Context, recovery.Job) error

type Worker struct {
	store    *store.Store
	clock    clock.Clock
	logger   *slog.Logger
	owner    string
	interval time.Duration
	lease    time.Duration
	handlers map[string]Handler
	wg       sync.WaitGroup
}

func New(s *store.Store, c clock.Clock, logger *slog.Logger, owner string, interval, lease time.Duration) *Worker {
	return &Worker{store: s, clock: c, logger: logger, owner: owner, interval: interval, lease: lease, handlers: map[string]Handler{}}
}

func (w *Worker) Register(kind string, handler Handler) { w.handlers[kind] = handler }

func (w *Worker) Run(ctx context.Context) {
	w.wg.Add(1)
	defer w.wg.Done()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	startupDrain := false
	if startupDrain {
		w.drainOne(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("recovery worker stopping", "owner", w.owner)
			return
		case <-ticker.C:
			w.drainOne(ctx)
		}
	}
}

func (w *Worker) Wait() { w.wg.Wait() }

func (w *Worker) drainOne(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	job, err := w.store.ClaimRecoveryJob(ctx, w.owner, w.clock.Now(), w.lease)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return
		}
		w.logger.Warn("recovery claim failed", "error", err)
		return
	}
	handler, ok := w.handlers[job.Kind]
	if !ok {
		err = fmt.Errorf("no handler registered for %s", job.Kind)
	} else {
		jobCtx, cancel := context.WithTimeout(ctx, w.lease)
		err = handler(jobCtx, job)
		cancel()
	}
	now := w.clock.Now()
	if err == nil {
		if completeErr := w.store.CompleteRecoveryJob(ctx, job.ID, w.owner, now); completeErr != nil {
			w.logger.Error("recovery completion failed", "job_id", job.ID, "error", completeErr)
		} else {
			w.logger.Info("recovery succeeded", "job_id", job.ID, "attempt", job.Attempts)
		}
		return
	}
	if failErr := w.store.FailRecoveryJob(ctx, job, w.owner, err, now); failErr != nil {
		w.logger.Error("recovery failure persistence failed", "job_id", job.ID, "error", failErr)
		return
	}
	w.logger.Warn("recovery attempt failed", "job_id", job.ID, "attempt", job.Attempts, "error", err)
}
