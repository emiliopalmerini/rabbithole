package tui

import (
	"fmt"
	"time"
)

const statsWindow = 10 * time.Second

// stats tracks message throughput and size statistics.
type stats struct {
	msgTimes      []time.Time
	totalMessages int64
	totalBytes    int64
}

// record logs a message arrival.
func (s *stats) record(t time.Time, bodySize int) {
	s.msgTimes = append(s.msgTimes, t)
	s.totalMessages++
	s.totalBytes += int64(bodySize)
}

// msgPerSec returns the message rate over the rolling window.
func (s *stats) msgPerSec(now time.Time) float64 {
	cutoff := now.Add(-statsWindow)

	// Trim expired entries
	i := 0
	for i < len(s.msgTimes) && s.msgTimes[i].Before(cutoff) {
		i++
	}
	s.msgTimes = s.msgTimes[i:]

	if len(s.msgTimes) == 0 {
		return 0
	}

	elapsed := now.Sub(s.msgTimes[0]).Seconds()
	if elapsed < 1 {
		elapsed = 1
	}
	return float64(len(s.msgTimes)) / elapsed
}

// avgSize returns average message body size in bytes.
func (s *stats) avgSize() int64 {
	if s.totalMessages == 0 {
		return 0
	}
	return s.totalBytes / s.totalMessages
}

func formatRate(rate float64) string {
	return fmt.Sprintf("%.1f msg/s", rate)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
