package tui

import (
	"regexp"
	"sort"
	"strings"
)

// computeFilteredIndices returns indices into msgs that match the filter expression.
// Uses the same field prefix syntax as search (rk:, body:, ex:, hdr:, type:, re:).
// Returns nil for empty expressions or invalid regex.
func computeFilteredIndices(msgs []Message, expr string) []int {
	if expr == "" {
		return nil
	}

	field, query := parseSearchQuery(expr)

	var re *regexp.Regexp
	if field == "re" {
		var err error
		re, err = compileSearchRegex(query)
		if err != nil {
			return nil
		}
	} else {
		query = strings.ToLower(query)
	}

	var indices []int
	for i, msg := range msgs {
		if matchesSearch(msg, field, query, re) {
			indices = append(indices, i)
		}
	}
	return indices
}

// nextVisible returns the next visible index after current in a sorted filtered list.
// Returns current if already at the last visible index.
func nextVisible(filtered []int, current int) int {
	idx := sort.SearchInts(filtered, current+1)
	if idx < len(filtered) {
		return filtered[idx]
	}
	// Stay at current if already at end
	if len(filtered) > 0 {
		return filtered[len(filtered)-1]
	}
	return current
}

// prevVisible returns the previous visible index before current in a sorted filtered list.
// Returns current if already at the first visible index.
func prevVisible(filtered []int, current int) int {
	idx := sort.SearchInts(filtered, current) - 1
	if idx >= 0 {
		return filtered[idx]
	}
	if len(filtered) > 0 {
		return filtered[0]
	}
	return current
}

// isVisible returns true if idx is in the sorted filtered list.
func isVisible(filtered []int, idx int) bool {
	i := sort.SearchInts(filtered, idx)
	return i < len(filtered) && filtered[i] == idx
}
