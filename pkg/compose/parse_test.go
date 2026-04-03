package compose

import (
	"testing"
)

func TestParseComposeServices(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
		wantErr bool
	}{
		{
			name: "single service",
			content: `services:
  auth:
    image: ghcr.io/org/auth
    ports: ["8085:8085"]
`,
			want: []string{"auth"},
		},
		{
			name: "multiple services sorted",
			content: `services:
  brighter-platform:
    image: ghcr.io/org/client
  admin:
    image: ghcr.io/org/admin
`,
			want: []string{"admin", "brighter-platform"},
		},
		{
			name: "five services",
			content: `services:
  auth:
    image: ghcr.io/org/auth
  litestream-restore:
    image: litestream/litestream
  copy-migrations:
    image: ghcr.io/org/auth
  run-migrations:
    image: golang:1.24-alpine
  litestream:
    image: litestream/litestream
`,
			want: []string{"auth", "copy-migrations", "litestream", "litestream-restore", "run-migrations"},
		},
		{
			name:    "empty content",
			content: "",
			want:    nil,
		},
		{
			name:    "no services key",
			content: "version: '3'\n",
			want:    nil,
		},
		{
			name:    "invalid yaml",
			content: "{{invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseComposeServices(tt.content)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseComposeServices() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseComposeServices() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseComposeServices()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
