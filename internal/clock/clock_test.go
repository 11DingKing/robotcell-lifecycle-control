package clock_test

import (
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/clock"
)

func TestManualClockAdvanceAndSet(t *testing.T) {
	initial := time.Date(2026, 8, 26, 1, 2, 3, 4, time.FixedZone("CST", 8*60*60))
	manual := clock.NewManual(initial)
	if got := manual.Now(); !got.Equal(initial.UTC()) || got.Location() != time.UTC {
		t.Fatalf("initial time = %v", got)
	}
	manual.Advance(3*time.Hour + 2*time.Minute)
	if got := manual.Now(); !got.Equal(initial.Add(3*time.Hour + 2*time.Minute)) {
		t.Fatalf("advanced time = %v", got)
	}
	set := initial.Add(48 * time.Hour)
	manual.Set(set)
	if got := manual.Now(); !got.Equal(set.UTC()) {
		t.Fatalf("set time = %v", got)
	}
}

func TestManualClockConcurrentReadersAndWriter(t *testing.T) {
	manual := clock.NewManual(time.Unix(0, 0))
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				_ = manual.Now()
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		for iteration := 0; iteration < 100; iteration++ {
			manual.Advance(time.Second)
		}
	}()
	wait.Wait()
	if got := manual.Now(); !got.Equal(time.Unix(100, 0).UTC()) {
		t.Fatalf("final time = %v", got)
	}
}
