package tui

import (
	"testing"
	"time"
)

func TestStats_Record(t *testing.T) {
	var s stats
	now := time.Now()

	s.record(now, 100)
	s.record(now, 200)

	if s.totalMessages != 2 {
		t.Errorf("totalMessages = %d, want 2", s.totalMessages)
	}
	if s.totalBytes != 300 {
		t.Errorf("totalBytes = %d, want 300", s.totalBytes)
	}
}

func TestStats_AvgSize(t *testing.T) {
	var s stats
	now := time.Now()

	if s.avgSize() != 0 {
		t.Errorf("avgSize with no messages should be 0, got %d", s.avgSize())
	}

	s.record(now, 100)
	s.record(now, 300)

	if s.avgSize() != 200 {
		t.Errorf("avgSize = %d, want 200", s.avgSize())
	}
}

func TestStats_MsgPerSec(t *testing.T) {
	var s stats
	now := time.Now()

	// No messages
	if s.msgPerSec(now) != 0 {
		t.Errorf("msgPerSec with no messages should be 0, got %f", s.msgPerSec(now))
	}

	// 10 messages over 2 seconds
	for i := 0; i < 10; i++ {
		s.record(now.Add(-2*time.Second+time.Duration(i)*200*time.Millisecond), 50)
	}

	rate := s.msgPerSec(now)
	if rate < 1.0 || rate > 10.0 {
		t.Errorf("msgPerSec = %f, expected between 1.0 and 10.0", rate)
	}
}

func TestStats_MsgPerSec_WindowExpiry(t *testing.T) {
	var s stats
	now := time.Now()

	// Messages from 20 seconds ago (outside 10s window)
	for i := 0; i < 5; i++ {
		s.record(now.Add(-20*time.Second), 50)
	}

	rate := s.msgPerSec(now)
	if rate != 0 {
		t.Errorf("msgPerSec for old messages should be 0, got %f", rate)
	}
}

func TestStats_FormatRate(t *testing.T) {
	tests := []struct {
		rate float64
		want string
	}{
		{0, "0.0 msg/s"},
		{1.5, "1.5 msg/s"},
		{123.456, "123.5 msg/s"},
	}
	for _, tt := range tests {
		got := formatRate(tt.rate)
		if got != tt.want {
			t.Errorf("formatRate(%f) = %q, want %q", tt.rate, got, tt.want)
		}
	}
}

func TestStats_FormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.bytes)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}
