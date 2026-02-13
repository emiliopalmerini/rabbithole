package tui

import (
	"strings"
	"testing"
	"time"
)

func TestIsDLXMessage(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]any
		want    bool
	}{
		{"nil headers", nil, false},
		{"empty headers", map[string]any{}, false},
		{"no dlx headers", map[string]any{"trace": "abc"}, false},
		{"x-death present", map[string]any{"x-death": []any{}}, true},
		{"x-first-death-reason present", map[string]any{"x-first-death-reason": "rejected"}, true},
		{"x-first-death-queue present", map[string]any{"x-first-death-queue": "my-queue"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Message{Headers: tt.headers}
			if got := isDLXMessage(msg); got != tt.want {
				t.Errorf("isDLXMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDLXInfo(t *testing.T) {
	headers := map[string]any{
		"x-first-death-reason": "rejected",
		"x-first-death-queue":  "orders.process",
		"x-death": []any{
			map[string]any{
				"count":        int64(3),
				"reason":       "rejected",
				"queue":        "orders.process",
				"exchange":     "orders",
				"routing-keys": []any{"order.created"},
				"time":         time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			},
			map[string]any{
				"count":    int64(1),
				"reason":   "expired",
				"queue":    "orders.retry",
				"exchange": "orders.dlx",
			},
		},
	}

	info := parseDLXInfo(headers)

	if info.FirstDeathReason != "rejected" {
		t.Errorf("FirstDeathReason = %q, want %q", info.FirstDeathReason, "rejected")
	}
	if info.FirstDeathQueue != "orders.process" {
		t.Errorf("FirstDeathQueue = %q, want %q", info.FirstDeathQueue, "orders.process")
	}
	if len(info.Deaths) != 2 {
		t.Fatalf("Deaths count = %d, want 2", len(info.Deaths))
	}

	d := info.Deaths[0]
	if d.Count != 3 {
		t.Errorf("Deaths[0].Count = %d, want 3", d.Count)
	}
	if d.Reason != "rejected" {
		t.Errorf("Deaths[0].Reason = %q, want %q", d.Reason, "rejected")
	}
	if d.Queue != "orders.process" {
		t.Errorf("Deaths[0].Queue = %q, want %q", d.Queue, "orders.process")
	}
	if d.Exchange != "orders" {
		t.Errorf("Deaths[0].Exchange = %q, want %q", d.Exchange, "orders")
	}
	if len(d.RoutingKeys) != 1 || d.RoutingKeys[0] != "order.created" {
		t.Errorf("Deaths[0].RoutingKeys = %v, want [order.created]", d.RoutingKeys)
	}
}

func TestRenderDLXTab(t *testing.T) {
	headers := map[string]any{
		"x-first-death-reason": "rejected",
		"x-first-death-queue":  "orders.process",
		"x-death": []any{
			map[string]any{
				"count":        int64(2),
				"reason":       "rejected",
				"queue":        "orders.process",
				"exchange":     "orders",
				"routing-keys": []any{"order.created"},
			},
		},
	}

	msg := Message{Headers: headers}
	lines := renderDLXTab(msg)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "rejected") {
		t.Errorf("DLX tab should contain 'rejected', got:\n%s", joined)
	}
	if !strings.Contains(joined, "orders.process") {
		t.Errorf("DLX tab should contain 'orders.process', got:\n%s", joined)
	}
	if !strings.Contains(joined, "2") {
		t.Errorf("DLX tab should contain count '2', got:\n%s", joined)
	}
}

func TestParseDLXInfo_Empty(t *testing.T) {
	info := parseDLXInfo(nil)
	if info.FirstDeathReason != "" || len(info.Deaths) != 0 {
		t.Errorf("parseDLXInfo(nil) should return empty info")
	}
}
