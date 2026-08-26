package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Address           string
	DatabasePath      string
	SessionTTL        time.Duration
	WorkerInterval    time.Duration
	WorkerLease       time.Duration
	WorkerMaxAttempts int
	ShutdownTimeout   time.Duration
	BootstrapPassword string
}

func Load() (Config, error) {
	cfg := Config{
		Address:           value("ROBOTCELL_ADDR", ":8080"),
		DatabasePath:      value("ROBOTCELL_DB_PATH", "robotcell.db"),
		BootstrapPassword: value("ROBOTCELL_BOOTSTRAP_PASSWORD", "change-this-password"),
	}
	var err error
	if cfg.SessionTTL, err = duration("ROBOTCELL_SESSION_TTL", 8*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.WorkerInterval, err = duration("ROBOTCELL_WORKER_INTERVAL", 500*time.Millisecond); err != nil {
		return Config{}, err
	}
	if cfg.WorkerLease, err = duration("ROBOTCELL_WORKER_LEASE", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration("ROBOTCELL_SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerMaxAttempts, err = integer("ROBOTCELL_WORKER_MAX_ATTEMPTS", 5); err != nil {
		return Config{}, err
	}
	if cfg.WorkerLease <= cfg.WorkerInterval {
		return Config{}, fmt.Errorf("worker lease must exceed interval")
	}
	if len(cfg.BootstrapPassword) < 12 {
		return Config{}, fmt.Errorf("bootstrap password must contain at least 12 characters")
	}
	return cfg, nil
}

func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return d, nil
}

func integer(name string, fallback int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}
