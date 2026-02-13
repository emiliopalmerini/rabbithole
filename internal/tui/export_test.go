package tui

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportCSV(t *testing.T) {
	dir := t.TempDir()
	exportDir := filepath.Join(dir, "exports")

	msgs := []Message{
		{
			ID:         1,
			RoutingKey: "events.user.created",
			Exchange:   "main",
			Timestamp:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			Headers:    map[string]any{"trace": "abc123"},
			Decoded:    map[string]any{"name": "Alice"},
			RawBody:    []byte(`{"name":"Alice"}`),
		},
		{
			ID:         2,
			RoutingKey: "events.order.placed",
			Exchange:   "main",
			Timestamp:  time.Date(2025, 1, 15, 10, 31, 0, 0, time.UTC),
			Decoded:    map[string]any{"total": 42.5, "note": "line1\nline2"},
			RawBody:    []byte(`{"total":42.5,"note":"line1\nline2"}`),
		},
	}

	path, err := writeCSVExport(msgs, exportDir)
	if err != nil {
		t.Fatalf("writeCSVExport failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("export file not found: %v", err)
	}

	// Parse CSV
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}

	// Header + 2 data rows
	if len(records) != 3 {
		t.Fatalf("expected 3 rows (header + 2 data), got %d", len(records))
	}

	// Check header
	expectedHeader := []string{"id", "timestamp", "exchange", "routing_key", "headers", "body"}
	for i, h := range expectedHeader {
		if records[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, records[0][i], h)
		}
	}

	// Check first row routing key
	if records[1][3] != "events.user.created" {
		t.Errorf("row 1 routing_key = %q, want %q", records[1][3], "events.user.created")
	}

	// Check that headers column contains JSON
	if !strings.Contains(records[1][4], "trace") {
		t.Errorf("row 1 headers should contain 'trace', got %q", records[1][4])
	}

	// Check that body column contains decoded content
	if !strings.Contains(records[1][5], "Alice") {
		t.Errorf("row 1 body should contain 'Alice', got %q", records[1][5])
	}

	// Check multi-line body is handled (JSON body contains real newlines from MarshalIndent)
	if !strings.Contains(records[2][5], "\n") {
		t.Errorf("row 2 body should contain newlines from JSON formatting, got %q", records[2][5])
	}
}

func TestExportCSV_Empty(t *testing.T) {
	dir := t.TempDir()
	_, err := writeCSVExport(nil, filepath.Join(dir, "exports"))
	if err == nil {
		t.Error("expected error for empty messages")
	}
}
