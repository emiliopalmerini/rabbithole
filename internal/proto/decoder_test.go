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
