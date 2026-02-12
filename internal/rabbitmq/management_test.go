package rabbitmq

import "testing"

func TestNewManagementClient(t *testing.T) {
	tests := []struct {
		name          string
		amqpURL       string
		managementURL string
		wantBase      string
		wantUser      string
		wantPass      string
		wantErr       bool
	}{
		{
			name:     "standard AMQP URL",
			amqpURL:  "amqp://myuser:mypass@rabbit.example.com:5672/",
			wantBase: "http://rabbit.example.com:15672/api",
			wantUser: "myuser",
			wantPass: "mypass",
		},
		{
			name:     "amqps scheme uses https",
			amqpURL:  "amqps://user:pass@secure.rabbit.io:5671/",
			wantBase: "https://secure.rabbit.io:15672/api",
			wantUser: "user",
			wantPass: "pass",
		},
		{
			name:          "management URL override used as-is",
			amqpURL:       "amqp://user:pass@host:5672/",
			managementURL: "http://custom:9999/api",
			wantBase:      "http://custom:9999/api",
			wantUser:      "user",
			wantPass:      "pass",
		},
		{
			name:     "no credentials defaults to guest",
			amqpURL:  "amqp://host:5672/",
			wantBase: "http://host:15672/api",
			wantUser: "guest",
			wantPass: "guest",
		},
		{
			name:     "empty hostname defaults to localhost",
			amqpURL:  "amqp://:5672/",
			wantBase: "http://localhost:15672/api",
			wantUser: "guest",
			wantPass: "guest",
		},
		{
			name:     "username without password defaults password to guest",
			amqpURL:  "amqp://admin@host:5672/",
			wantBase: "http://host:15672/api",
			wantUser: "admin",
			wantPass: "guest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewManagementClient(tt.amqpURL, tt.managementURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewManagementClient() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			if client.baseURL != tt.wantBase {
				t.Errorf("baseURL = %q, want %q", client.baseURL, tt.wantBase)
			}
			if client.username != tt.wantUser {
				t.Errorf("username = %q, want %q", client.username, tt.wantUser)
			}
			if client.password != tt.wantPass {
				t.Errorf("password = %q, want %q", client.password, tt.wantPass)
			}
		})
	}
}
