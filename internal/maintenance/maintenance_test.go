package maintenance_test

import (
	"testing"
	"time"

	"github.com/11DingKing/robotcell-lifecycle-control/internal/maintenance"
)

func validOrder() maintenance.Order {
	partID := int64(8)
	return maintenance.Order{
		Code: "MO-2026-001", CellID: 1, AssigneeID: 2, SparePartID: &partID,
		SpareQuantity: 2, Priority: 3, Summary: "更换腕部减速器",
		Status: maintenance.Opened, DueAt: time.Now().Add(24 * time.Hour),
	}
}

func TestOrderValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*maintenance.Order)
		valid  bool
	}{
		{"valid with part", func(*maintenance.Order) {}, true},
		{"valid without part", func(o *maintenance.Order) { o.SparePartID = nil; o.SpareQuantity = 0 }, true},
		{"missing code", func(o *maintenance.Order) { o.Code = "" }, false},
		{"missing summary", func(o *maintenance.Order) { o.Summary = " " }, false},
		{"missing cell", func(o *maintenance.Order) { o.CellID = 0 }, false},
		{"missing assignee", func(o *maintenance.Order) { o.AssigneeID = 0 }, false},
		{"priority below range", func(o *maintenance.Order) { o.Priority = 0 }, false},
		{"priority above range", func(o *maintenance.Order) { o.Priority = 6 }, false},
		{"minimum priority", func(o *maintenance.Order) { o.Priority = 1 }, true},
		{"maximum priority", func(o *maintenance.Order) { o.Priority = 5 }, true},
		{"quantity without part", func(o *maintenance.Order) { o.SparePartID = nil }, false},
		{"zero quantity with part", func(o *maintenance.Order) { o.SpareQuantity = 0 }, false},
		{"negative quantity", func(o *maintenance.Order) { o.SpareQuantity = -1 }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order := validOrder()
			test.change(&order)
			err := order.Validate()
			if test.valid && err != nil {
				t.Fatalf("expected valid order: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected invalid order")
			}
		})
	}
}

func TestOrderTransitions(t *testing.T) {
	tests := []struct {
		from maintenance.Status
		to   maintenance.Status
		want bool
	}{
		{maintenance.Opened, maintenance.Triaged, true},
		{maintenance.Opened, maintenance.Cancelled, true},
		{maintenance.Opened, maintenance.Approved, false},
		{maintenance.Triaged, maintenance.Approved, true},
		{maintenance.Triaged, maintenance.Cancelled, true},
		{maintenance.Triaged, maintenance.Executing, false},
		{maintenance.Approved, maintenance.Executing, true},
		{maintenance.Approved, maintenance.Cancelled, true},
		{maintenance.Executing, maintenance.Verifying, true},
		{maintenance.Executing, maintenance.Cancelled, true},
		{maintenance.Verifying, maintenance.Closed, true},
		{maintenance.Verifying, maintenance.Executing, true},
		{maintenance.Closed, maintenance.Executing, false},
		{maintenance.Cancelled, maintenance.Opened, false},
	}
	for _, test := range tests {
		order := maintenance.Order{Status: test.from}
		if got := order.CanTransition(test.to); got != test.want {
			t.Errorf("transition %s -> %s = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestSparePartCapacity(t *testing.T) {
	tests := []struct {
		available int
		reserved  int
		request   int
		want      bool
	}{
		{10, 0, 1, true},
		{10, 0, 10, true},
		{10, 0, 11, false},
		{10, 4, 6, true},
		{10, 4, 7, false},
		{10, 10, 1, false},
		{0, 0, 1, false},
		{10, 0, 0, false},
		{10, 0, -1, false},
	}
	for _, test := range tests {
		part := maintenance.SparePart{Available: test.available, Reserved: test.reserved}
		if got := part.CanReserve(test.request); got != test.want {
			t.Errorf("capacity available=%d reserved=%d request=%d got=%v want=%v", test.available, test.reserved, test.request, got, test.want)
		}
	}
}
