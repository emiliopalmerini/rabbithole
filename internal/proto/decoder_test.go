package proto

import "testing"

func TestRoutingKeyToTypeHint(t *testing.T) {
	tests := []struct {
		name       string
		routingKey string
		want       string
	}{
		{
			name:       "standard two-part entity.action",
			routingKey: "editorial.it.country.updated",
			want:       "CountryUpdated",
		},
		{
			name:       "simple two segments",
			routingKey: "user.created",
			want:       "UserCreated",
		},
		{
			name:       "snake_case entity",
			routingKey: "admin.administrative_area.deleted",
			want:       "AdministrativeAreaDeleted",
		},
		{
			name:       "single segment returns empty",
			routingKey: "created",
			want:       "",
		},
		{
			name:       "empty string returns empty",
			routingKey: "",
			want:       "",
		},
		{
			name:       "many segments uses last two",
			routingKey: "a.b.c.d.order.shipped",
			want:       "OrderShipped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := routingKeyToTypeHint(tt.routingKey)
			if got != tt.want {
				t.Errorf("routingKeyToTypeHint(%q) = %q, want %q", tt.routingKey, got, tt.want)
			}
		})
	}
}

func TestIsPrintableText(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "valid UTF-8 text",
			data: []byte("hello world"),
			want: true,
		},
		{
			name: "text with newline, carriage return, tab",
			data: []byte("line1\nline2\r\ttab"),
			want: true,
		},
		{
			name: "invalid UTF-8 bytes",
			data: []byte{0xff, 0xfe, 0xfd},
			want: false,
		},
		{
			name: "null byte control character",
			data: []byte{0x00},
			want: false,
		},
		{
			name: "SOH control character",
			data: []byte("hello\x01world"),
			want: false,
		},
		{
			name: "empty slice",
			data: []byte{},
			want: true,
		},
		{
			name: "unicode text",
			data: []byte("ciao mondo"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPrintableText(tt.data)
			if got != tt.want {
				t.Errorf("isPrintableText(%v) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}
