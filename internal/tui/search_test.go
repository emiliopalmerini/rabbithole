package tui

import "testing"

func TestParseSearchQuery_Prefixes(t *testing.T) {
	tests := []struct {
		input     string
		wantField string
		wantQuery string
	}{
		{"hello", "", "hello"},
		{"rk:country", "rk", "country"},
		{"body:alice", "body", "alice"},
		{"ex:events", "ex", "events"},
		{"hdr:trace", "hdr", "trace"},
		{"type:User", "type", "User"},
		{"re:foo.*bar", "re", "foo.*bar"},
	}
	for _, tt := range tests {
		field, query := parseSearchQuery(tt.input)
		if field != tt.wantField || query != tt.wantQuery {
			t.Errorf("parseSearchQuery(%q) = (%q, %q), want (%q, %q)",
				tt.input, field, query, tt.wantField, tt.wantQuery)
		}
	}
}

func TestMatchesSearch_Substring(t *testing.T) {
	msg := Message{
		RoutingKey: "events.user.created",
		Exchange:   "main",
		Decoded:    map[string]any{"name": "Alice"},
	}

	if !matchesSearch(msg, "", "user", nil) {
		t.Error("expected unprefixed 'user' to match routing key")
	}
	if !matchesSearch(msg, "rk", "user", nil) {
		t.Error("expected rk:user to match")
	}
	if matchesSearch(msg, "rk", "zzz", nil) {
		t.Error("expected rk:zzz to not match")
	}
	if !matchesSearch(msg, "body", "alice", nil) {
		t.Error("expected body:alice to match (case insensitive)")
	}
}

func TestMatchesSearch_Regex(t *testing.T) {
	msg := Message{
		RoutingKey: "events.user.created",
		Exchange:   "main",
		Decoded:    map[string]any{"name": "Alice", "email": "alice@example.com"},
	}

	tests := []struct {
		pattern string
		want    bool
	}{
		{`user\.created`, true},
		{`^events\.`, true},
		{`\.deleted$`, false},
		{`alice@.*\.com`, true}, // matches body
		{`ALICE`, false},        // regex is case-sensitive by default
		{`(?i)ALICE`, true},     // case-insensitive flag
	}

	for _, tt := range tests {
		re, err := compileSearchRegex(tt.pattern)
		if err != nil {
			t.Fatalf("compileSearchRegex(%q) error: %v", tt.pattern, err)
		}
		got := matchesSearch(msg, "re", "", re)
		if got != tt.want {
			t.Errorf("matchesSearch(re:%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}

func TestCompileSearchRegex_Invalid(t *testing.T) {
	_, err := compileSearchRegex("[invalid")
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}
