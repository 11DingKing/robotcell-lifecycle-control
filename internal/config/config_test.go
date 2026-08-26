package config_test

import (
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	for _, name := range []string{"ROBOTCELL_ADDR", "ROBOTCELL_DB_PATH", "ROBOTCELL_SESSION_TTL", "ROBOTCELL_WORKER_INTERVAL", "ROBOTCELL_WORKER_LEASE", "ROBOTCELL_WORKER_MAX_ATTEMPTS", "ROBOTCELL_SHUTDOWN_TIMEOUT", "ROBOTCELL_BOOTSTRAP_PASSWORD"} {
		t.Setenv(name, "")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != ":8080" || cfg.DatabasePath != "robotcell.db" {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if cfg.SessionTTL != 8*time.Hour || cfg.WorkerInterval != 500*time.Millisecond || cfg.WorkerLease != 30*time.Second {
		t.Fatalf("unexpected timing defaults: %#v", cfg)
	}
	if cfg.WorkerMaxAttempts != 5 || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected operational defaults: %#v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("ROBOTCELL_ADDR", "127.0.0.1:9090")
	t.Setenv("ROBOTCELL_DB_PATH", "/tmp/custom.db")
	t.Setenv("ROBOTCELL_SESSION_TTL", "30m")
	t.Setenv("ROBOTCELL_WORKER_INTERVAL", "2s")
	t.Setenv("ROBOTCELL_WORKER_LEASE", "9s")
	t.Setenv("ROBOTCELL_WORKER_MAX_ATTEMPTS", "7")
	t.Setenv("ROBOTCELL_SHUTDOWN_TIMEOUT", "4s")
	t.Setenv("ROBOTCELL_BOOTSTRAP_PASSWORD", "custom-password-2026")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "127.0.0.1:9090" || cfg.DatabasePath != "/tmp/custom.db" || cfg.SessionTTL != 30*time.Minute || cfg.WorkerInterval != 2*time.Second || cfg.WorkerLease != 9*time.Second || cfg.WorkerMaxAttempts != 7 || cfg.ShutdownTimeout != 4*time.Second || cfg.BootstrapPassword != "custom-password-2026" {
		t.Fatalf("overrides not applied: %#v", cfg)
	}
}

func TestLoadRejectsInvalidDurationsAndCounts(t *testing.T) {
	tests := []struct{ name, value string }{
		{"ROBOTCELL_SESSION_TTL", "bad"},
		{"ROBOTCELL_SESSION_TTL", "0s"},
		{"ROBOTCELL_WORKER_INTERVAL", "-1s"},
		{"ROBOTCELL_WORKER_LEASE", "not-a-duration"},
		{"ROBOTCELL_SHUTDOWN_TIMEOUT", "0"},
		{"ROBOTCELL_WORKER_MAX_ATTEMPTS", "zero"},
		{"ROBOTCELL_WORKER_MAX_ATTEMPTS", "0"},
	}
	for _, test := range tests {
		t.Run(test.name+"="+test.value, func(t *testing.T) {
			t.Setenv(test.name, test.value)
			if _, err := config.Load(); err == nil {
				t.Fatal("expected invalid configuration")
			}
		})
	}
}

func TestLoadRejectsLeaseNotLongerThanInterval(t *testing.T) {
	t.Setenv("ROBOTCELL_WORKER_INTERVAL", "10s")
	t.Setenv("ROBOTCELL_WORKER_LEASE", "10s")
	if _, err := config.Load(); err == nil {
		t.Fatal("equal lease and interval should fail")
	}
	t.Setenv("ROBOTCELL_WORKER_LEASE", "9s")
	if _, err := config.Load(); err == nil {
		t.Fatal("shorter lease should fail")
	}
}

func TestLoadRejectsShortBootstrapPassword(t *testing.T) {
	t.Setenv("ROBOTCELL_BOOTSTRAP_PASSWORD", "too-short")
	if _, err := config.Load(); err == nil {
		t.Fatal("short bootstrap password should fail")
	}
}
