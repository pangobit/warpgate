package deploy

import (
	"testing"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestParseContainerHealth(t *testing.T) {
	tests := []struct {
		name     string
		psOutput string
		want     string
	}{
		{
			name:     "empty output",
			psOutput: "",
			want:     "not deployed",
		},
		{
			name:     "single healthy container",
			psOutput: "auth-blue-auth-1\tUp 2 minutes (healthy)",
			want:     "healthy",
		},
		{
			name:     "multiple healthy containers",
			psOutput: "auth-blue-auth-1\tUp 2 minutes (healthy)\nauth-blue-litestream-1\tUp 2 minutes (healthy)",
			want:     "healthy",
		},
		{
			name:     "one unhealthy container",
			psOutput: "auth-blue-auth-1\tUp 30 seconds (unhealthy)",
			want:     "unhealthy",
		},
		{
			name:     "mixed healthy and unhealthy",
			psOutput: "auth-blue-auth-1\tUp 2 minutes (healthy)\nauth-blue-sidecar-1\tUp 30 seconds (unhealthy)",
			want:     "unhealthy",
		},
		{
			name:     "running without healthcheck",
			psOutput: "pangobit-green-pangobit-1\tUp 5 minutes",
			want:     "running",
		},
		{
			name:     "mixed healthy and no healthcheck",
			psOutput: "auth-blue-auth-1\tUp 2 minutes (healthy)\nauth-blue-litestream-1\tUp 2 minutes",
			want:     "healthy",
		},
		{
			name:     "trailing newline",
			psOutput: "auth-blue-auth-1\tUp 2 minutes (healthy)\n",
			want:     "healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseContainerHealth(tt.psOutput)
			if got != tt.want {
				t.Errorf("ParseContainerHealth() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseReleaseServiceStatuses(t *testing.T) {
	services := map[string]config.ReleaseServiceConfig{
		"api":   {Image: "ghcr.io/org/api"},
		"admin": {Image: "ghcr.io/org/admin"},
	}

	tests := []struct {
		name     string
		psOutput string
		services map[string]config.ReleaseServiceConfig
		want     []ContainerStatus
	}{
		{
			name:     "no services configured",
			psOutput: "app\tauth-blue-app-1\tUp 2 minutes (healthy)",
			services: nil,
			want:     nil,
		},
		{
			name:     "empty output",
			psOutput: "",
			services: services,
			want:     nil,
		},
		{
			name:     "service healthy",
			psOutput: "api\tapp-blue-api-1\tUp 2 minutes (healthy)",
			services: services,
			want: []ContainerStatus{
				{Service: "api", Name: "app-blue-api-1", State: "healthy"},
			},
		},
		{
			name:     "service unhealthy",
			psOutput: "api\tapp-blue-api-1\tUp 30 seconds (unhealthy)",
			services: services,
			want: []ContainerStatus{
				{Service: "api", Name: "app-blue-api-1", State: "unhealthy"},
			},
		},
		{
			name:     "service running without healthcheck",
			psOutput: "admin\tapp-blue-admin-1\tUp 2 minutes",
			services: services,
			want: []ContainerStatus{
				{Service: "admin", Name: "app-blue-admin-1", State: "running"},
			},
		},
		{
			name:     "multiple services",
			psOutput: "api\tapp-blue-api-1\tUp 2 minutes (healthy)\nadmin\tapp-blue-admin-1\tUp 2 minutes",
			services: services,
			want: []ContainerStatus{
				{Service: "api", Name: "app-blue-api-1", State: "healthy"},
				{Service: "admin", Name: "app-blue-admin-1", State: "running"},
			},
		},
		{
			name:     "undeclared compose service ignored",
			psOutput: "api\tapp-blue-api-1\tUp 2 minutes (healthy)\nredis\tapp-blue-redis-1\tUp 2 minutes",
			services: services,
			want: []ContainerStatus{
				{Service: "api", Name: "app-blue-api-1", State: "healthy"},
			},
		},
		{
			name:     "malformed line ignored",
			psOutput: "bad-line-no-tabs\napi\tapp-blue-api-1\tUp 2 minutes (healthy)",
			services: services,
			want: []ContainerStatus{
				{Service: "api", Name: "app-blue-api-1", State: "healthy"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReleaseServiceStatuses(tt.psOutput, tt.services)
			if len(got) != len(tt.want) {
				t.Fatalf("parseReleaseServiceStatuses() returned %d items, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("parseReleaseServiceStatuses()[%d] = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}
