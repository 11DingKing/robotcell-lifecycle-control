package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/auth"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/config"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/httpapi"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/identity"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/recovery"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/service"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/store"
	"github.com/11DingKing/robotcell-lifecycle-control/internal/worker"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		response, err := http.Get("http://127.0.0.1:8080/healthz")
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration rejected", "error", err)
		os.Exit(1)
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	database, err := store.Open(rootCtx, cfg.DatabasePath)
	if err != nil {
		logger.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	if err = bootstrap(rootCtx, database, cfg.BootstrapPassword); err != nil {
		logger.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}
	appClock := clock.Real{}
	authService := auth.New(database, appClock, cfg.SessionTTL)
	lifecycleService := service.NewLifecycle(database, appClock)
	schedulingService := service.NewScheduling(database, appClock)
	maintenanceService := service.NewMaintenance(database, appClock)
	recoveryWorker := worker.New(database, appClock, logger, "server-worker", cfg.WorkerInterval, cfg.WorkerLease)
	recoveryWorker.Register("calibration_compensation", func(ctx context.Context, job recovery.Job) error {
		return database.CompensateCalibration(ctx, job, appClock.Now())
	})
	go recoveryWorker.Run(rootCtx)
	handler := httpapi.New(database, authService, lifecycleService, schedulingService, maintenanceService, logger)
	server := &http.Server{Addr: cfg.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		logger.Info("server listening", "address", cfg.Address)
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", serveErr)
			stop()
		}
	}()
	<-rootCtx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err = server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		_ = server.Close()
	}
	recoveryWorker.Wait()
	logger.Info("server stopped")
}

func bootstrap(ctx context.Context, database *store.Store, password string) error {
	hash, err := auth.PasswordHash(password)
	if err != nil {
		return err
	}
	users := []identity.User{{Username: "line.manager", DisplayName: "产线负责人", Role: identity.RoleLineManager, Active: true, PasswordHash: hash}, {Username: "operator", DisplayName: "现场操作员", Role: identity.RoleOperator, Active: true, PasswordHash: hash}, {Username: "safety", DisplayName: "安全员", Role: identity.RoleSafetyOfficer, Active: true, PasswordHash: hash}, {Username: "quality", DisplayName: "质量工程师", Role: identity.RoleQualityEngineer, Active: true, PasswordHash: hash}, {Username: "maintenance", DisplayName: "维护工程师", Role: identity.RoleMaintenance, Active: true, PasswordHash: hash}, {Username: "integrator", DisplayName: "外部集成方", Role: identity.RoleIntegrator, Active: true, PasswordHash: hash}}
	for _, user := range users {
		if _, findErr := database.FindUserByUsername(ctx, user.Username); findErr == nil {
			continue
		}
		if _, createErr := database.CreateUser(ctx, user); createErr != nil {
			return createErr
		}
	}
	return nil
}
