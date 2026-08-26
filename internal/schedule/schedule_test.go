package schedule_test

import (
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/schedule"
)

func validWindow() schedule.WorkWindow {
	start := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	return schedule.WorkWindow{
		CellID: 1, WorkstationID: 2, ToolID: 3, QualifiedUserID: 4,
		StartsAt: start, EndsAt: start.Add(4 * time.Hour), Purpose: "安装和标定",
		Status: schedule.WindowRequested,
	}
}

func TestWorkWindowValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*schedule.WorkWindow)
		valid  bool
	}{
		{"valid", func(*schedule.WorkWindow) {}, true},
		{"missing cell", func(w *schedule.WorkWindow) { w.CellID = 0 }, false},
		{"missing workstation", func(w *schedule.WorkWindow) { w.WorkstationID = 0 }, false},
		{"missing tool", func(w *schedule.WorkWindow) { w.ToolID = 0 }, false},
		{"missing person", func(w *schedule.WorkWindow) { w.QualifiedUserID = 0 }, false},
		{"zero duration", func(w *schedule.WorkWindow) { w.EndsAt = w.StartsAt }, false},
		{"reverse duration", func(w *schedule.WorkWindow) { w.EndsAt = w.StartsAt.Add(-time.Minute) }, false},
		{"over 24 hours", func(w *schedule.WorkWindow) { w.EndsAt = w.StartsAt.Add(24*time.Hour + time.Second) }, false},
		{"exactly 24 hours", func(w *schedule.WorkWindow) { w.EndsAt = w.StartsAt.Add(24 * time.Hour) }, true},
		{"blank purpose", func(w *schedule.WorkWindow) { w.Purpose = "  " }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			window := validWindow()
			test.change(&window)
			err := window.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected valid window, got %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected invalid window")
			}
		})
	}
}

func TestWorkWindowTransitions(t *testing.T) {
	tests := []struct {
		from schedule.WindowStatus
		to   schedule.WindowStatus
		want bool
	}{
		{schedule.WindowRequested, schedule.WindowApproved, true},
		{schedule.WindowRequested, schedule.WindowCancelled, true},
		{schedule.WindowRequested, schedule.WindowReserved, false},
		{schedule.WindowApproved, schedule.WindowReserved, true},
		{schedule.WindowApproved, schedule.WindowCancelled, true},
		{schedule.WindowApproved, schedule.WindowActive, false},
		{schedule.WindowReserved, schedule.WindowActive, true},
		{schedule.WindowReserved, schedule.WindowCancelled, true},
		{schedule.WindowReserved, schedule.WindowExpired, true},
		{schedule.WindowActive, schedule.WindowCompleted, true},
		{schedule.WindowActive, schedule.WindowCancelled, true},
		{schedule.WindowCompleted, schedule.WindowActive, false},
		{schedule.WindowCancelled, schedule.WindowRequested, false},
		{schedule.WindowExpired, schedule.WindowRequested, false},
	}
	for _, test := range tests {
		window := schedule.WorkWindow{Status: test.from}
		if got := window.CanTransition(test.to); got != test.want {
			t.Errorf("transition %s -> %s = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestWorkWindowOverlapUsesHalfOpenIntervals(t *testing.T) {
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		a    schedule.WorkWindow
		b    schedule.WorkWindow
		want bool
	}{
		{"same interval", schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(time.Hour)}, schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(time.Hour)}, true},
		{"inside", schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(4 * time.Hour)}, schedule.WorkWindow{StartsAt: base.Add(time.Hour), EndsAt: base.Add(2 * time.Hour)}, true},
		{"contains", schedule.WorkWindow{StartsAt: base.Add(time.Hour), EndsAt: base.Add(2 * time.Hour)}, schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(4 * time.Hour)}, true},
		{"overlap left", schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(2 * time.Hour)}, schedule.WorkWindow{StartsAt: base.Add(time.Hour), EndsAt: base.Add(3 * time.Hour)}, true},
		{"overlap right", schedule.WorkWindow{StartsAt: base.Add(time.Hour), EndsAt: base.Add(3 * time.Hour)}, schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(2 * time.Hour)}, true},
		{"touching right", schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(time.Hour)}, schedule.WorkWindow{StartsAt: base.Add(time.Hour), EndsAt: base.Add(2 * time.Hour)}, false},
		{"touching left", schedule.WorkWindow{StartsAt: base.Add(time.Hour), EndsAt: base.Add(2 * time.Hour)}, schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(time.Hour)}, false},
		{"separated", schedule.WorkWindow{StartsAt: base, EndsAt: base.Add(time.Hour)}, schedule.WorkWindow{StartsAt: base.Add(2 * time.Hour), EndsAt: base.Add(3 * time.Hour)}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.a.Overlaps(test.b); got != test.want {
				t.Fatalf("overlap = %v, want %v", got, test.want)
			}
			if got := test.b.Overlaps(test.a); got != test.want {
				t.Fatalf("reverse overlap = %v, want %v", got, test.want)
			}
		})
	}
}

func TestQualificationValidity(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(365 * 24 * time.Hour)
	qualification := schedule.Qualification{ValidFrom: start, ValidTo: end}
	if !qualification.ValidAt(start) {
		t.Fatal("qualification must be valid at inclusive start")
	}
	if !qualification.ValidAt(end.Add(-time.Nanosecond)) {
		t.Fatal("qualification must be valid before end")
	}
	if qualification.ValidAt(end) {
		t.Fatal("qualification end is exclusive")
	}
	if qualification.ValidAt(start.Add(-time.Nanosecond)) {
		t.Fatal("qualification is not valid before start")
	}
	revoked := start.Add(time.Hour)
	qualification.RevokedAt = &revoked
	if qualification.ValidAt(start) {
		t.Fatal("revoked qualification must never be accepted")
	}
}
