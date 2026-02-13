package tui

import (
	"fmt"
	"strings"
	"time"
)

// dlxHeaders are the AMQP headers that indicate a dead-lettered message.
var dlxHeaders = []string{"x-death", "x-first-death-reason", "x-first-death-queue"}

// isDLXMessage returns true if the message has any dead-letter headers.
func isDLXMessage(msg Message) bool {
	if len(msg.Headers) == 0 {
		return false
	}
	for _, key := range dlxHeaders {
		if _, ok := msg.Headers[key]; ok {
			return true
		}
	}
	return false
}

type deathRecord struct {
	Count       int64
	Reason      string
	Queue       string
	Exchange    string
	RoutingKeys []string
	Time        time.Time
}

type dlxInfo struct {
	FirstDeathReason string
	FirstDeathQueue  string
	Deaths           []deathRecord
}

// parseDLXInfo extracts dead-letter metadata from message headers.
func parseDLXInfo(headers map[string]any) dlxInfo {
	var info dlxInfo
	if headers == nil {
		return info
	}

	if v, ok := headers["x-first-death-reason"]; ok {
		info.FirstDeathReason = fmt.Sprint(v)
	}
	if v, ok := headers["x-first-death-queue"]; ok {
		info.FirstDeathQueue = fmt.Sprint(v)
	}

	if deaths, ok := headers["x-death"]; ok {
		if deathList, ok := deaths.([]any); ok {
			for _, d := range deathList {
				if dm, ok := d.(map[string]any); ok {
					info.Deaths = append(info.Deaths, parseDeathRecord(dm))
				}
			}
		}
	}

	return info
}

func parseDeathRecord(m map[string]any) deathRecord {
	var d deathRecord
	if v, ok := m["count"]; ok {
		switch n := v.(type) {
		case int64:
			d.Count = n
		case float64:
			d.Count = int64(n)
		}
	}
	if v, ok := m["reason"]; ok {
		d.Reason = fmt.Sprint(v)
	}
	if v, ok := m["queue"]; ok {
		d.Queue = fmt.Sprint(v)
	}
	if v, ok := m["exchange"]; ok {
		d.Exchange = fmt.Sprint(v)
	}
	if v, ok := m["routing-keys"]; ok {
		if rks, ok := v.([]any); ok {
			for _, rk := range rks {
				d.RoutingKeys = append(d.RoutingKeys, fmt.Sprint(rk))
			}
		}
	}
	if v, ok := m["time"]; ok {
		if t, ok := v.(time.Time); ok {
			d.Time = t
		}
	}
	return d
}

// renderDLXTab renders the Dead Letter tab content for the detail pane.
func renderDLXTab(msg Message) []string {
	info := parseDLXInfo(msg.Headers)
	var lines []string

	if info.FirstDeathReason != "" {
		lines = append(lines, fieldNameStyle.Render("First Death Reason: ")+info.FirstDeathReason)
	}
	if info.FirstDeathQueue != "" {
		lines = append(lines, fieldNameStyle.Render("First Death Queue: ")+info.FirstDeathQueue)
	}

	for i, d := range info.Deaths {
		if i == 0 && len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, fieldNameStyle.Render(fmt.Sprintf("Death #%d", i+1)))
		lines = append(lines, fieldNameStyle.Render("  Reason: ")+d.Reason)
		lines = append(lines, fieldNameStyle.Render("  Queue: ")+d.Queue)
		if d.Exchange != "" {
			lines = append(lines, fieldNameStyle.Render("  Exchange: ")+d.Exchange)
		}
		if len(d.RoutingKeys) > 0 {
			lines = append(lines, fieldNameStyle.Render("  Routing Keys: ")+strings.Join(d.RoutingKeys, ", "))
		}
		lines = append(lines, fieldNameStyle.Render("  Count: ")+fmt.Sprintf("%d", d.Count))
		if !d.Time.IsZero() {
			lines = append(lines, fieldNameStyle.Render("  Time: ")+d.Time.Format(time.RFC3339))
		}

		if i < len(info.Deaths)-1 {
			lines = append(lines, "")
		}
	}

	if len(lines) == 0 {
		lines = append(lines, mutedStyle.Render("No dead-letter information"))
	}

	return lines
}
