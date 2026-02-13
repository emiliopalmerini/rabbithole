package tui

import (
	"testing"
)

func TestApplyFilter_RoutingKey(t *testing.T) {
	msgs := []Message{
		{ID: 1, RoutingKey: "events.user.created", Exchange: "main"},
		{ID: 2, RoutingKey: "events.order.placed", Exchange: "main"},
		{ID: 3, RoutingKey: "events.user.deleted", Exchange: "main"},
	}

	indices := computeFilteredIndices(msgs, "user")
	if len(indices) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(indices))
	}
	if indices[0] != 0 || indices[1] != 2 {
		t.Errorf("expected [0, 2], got %v", indices)
	}
}

func TestApplyFilter_FieldPrefix(t *testing.T) {
	msgs := []Message{
		{ID: 1, RoutingKey: "events.user.created", Exchange: "main"},
		{ID: 2, RoutingKey: "events.order.placed", Exchange: "orders"},
	}

	indices := computeFilteredIndices(msgs, "ex:orders")
	if len(indices) != 1 || indices[0] != 1 {
		t.Errorf("expected [1], got %v", indices)
	}
}

func TestApplyFilter_Regex(t *testing.T) {
	msgs := []Message{
		{ID: 1, RoutingKey: "events.user.created"},
		{ID: 2, RoutingKey: "events.order.placed"},
		{ID: 3, RoutingKey: "logs.error.timeout"},
	}

	indices := computeFilteredIndices(msgs, `re:^events\.`)
	if len(indices) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(indices))
	}
	if indices[0] != 0 || indices[1] != 1 {
		t.Errorf("expected [0, 1], got %v", indices)
	}
}

func TestApplyFilter_Empty(t *testing.T) {
	msgs := []Message{
		{ID: 1, RoutingKey: "events.user.created"},
	}

	indices := computeFilteredIndices(msgs, "")
	if indices != nil {
		t.Errorf("expected nil for empty filter, got %v", indices)
	}
}

func TestApplyFilter_NoMatches(t *testing.T) {
	msgs := []Message{
		{ID: 1, RoutingKey: "events.user.created"},
	}

	indices := computeFilteredIndices(msgs, "zzz_nonexistent")
	if len(indices) != 0 {
		t.Errorf("expected 0 matches, got %d", len(indices))
	}
}

func TestApplyFilter_InvalidRegex(t *testing.T) {
	msgs := []Message{
		{ID: 1, RoutingKey: "events.user.created"},
	}

	// Invalid regex should return empty (not panic)
	indices := computeFilteredIndices(msgs, "re:[invalid")
	if indices != nil {
		t.Errorf("expected nil for invalid regex, got %v", indices)
	}
}

func TestFilteredNavigation(t *testing.T) {
	// Test that nextVisible/prevVisible navigate correctly through filtered indices
	filtered := []int{0, 3, 5, 8}

	next := nextVisible(filtered, 0)
	if next != 3 {
		t.Errorf("nextVisible(0) = %d, want 3", next)
	}

	next = nextVisible(filtered, 5)
	if next != 8 {
		t.Errorf("nextVisible(5) = %d, want 8", next)
	}

	// At last visible, stay there
	next = nextVisible(filtered, 8)
	if next != 8 {
		t.Errorf("nextVisible(8) = %d, want 8", next)
	}

	prev := prevVisible(filtered, 8)
	if prev != 5 {
		t.Errorf("prevVisible(8) = %d, want 5", prev)
	}

	prev = prevVisible(filtered, 0)
	if prev != 0 {
		t.Errorf("prevVisible(0) = %d, want 0", prev)
	}
}
