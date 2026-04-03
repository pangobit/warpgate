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

func TestParseSidecarStatuses(t *testing.T) {
	sidecars := map[string]config.SidecarConfig{
		"redis":      {Port: 6379},
		"litestream": {Port: 0},
	}

	tests := []struct {
		name     string
		psOutput string
		sidecars map[string]config.SidecarConfig
		want     []ContainerStatus
	}{
		{
			name:     "no sidecars configured",
			psOutput: "app\tauth-blue-app-1\tUp 2 minutes (healthy)",
			sidecars: nil,
			want:     nil,
		},
		{
			name:     "empty output",
			psOutput: "",
			sidecars: sidecars,
			want:     nil,
		},
		{
			name:     "only main service",
			psOutput: "auth\tauth-blue-auth-1\tUp 2 minutes (healthy)",
			sidecars: sidecars,
			want:     nil,
		},
		{
			name:     "sidecar healthy",
			psOutput: "auth\tauth-blue-auth-1\tUp 2 minutes (healthy)\nredis\tauth-blue-redis-1\tUp 2 minutes (healthy)",
			sidecars: sidecars,
			want: []ContainerStatus{
				{Service: "redis", Name: "auth-blue-redis-1", State: "healthy"},
			},
		},
		{
			name:     "sidecar unhealthy",
			psOutput: "auth\tauth-blue-auth-1\tUp 2 minutes (healthy)\nredis\tauth-blue-redis-1\tUp 30 seconds (unhealthy)",
			sidecars: sidecars,
			want: []ContainerStatus{
				{Service: "redis", Name: "auth-blue-redis-1", State: "unhealthy"},
			},
		},
		{
			name:     "sidecar running without healthcheck",
			psOutput: "auth\tauth-blue-auth-1\tUp 2 minutes (healthy)\nlitestream\tauth-blue-litestream-1\tUp 2 minutes",
			sidecars: sidecars,
			want: []ContainerStatus{
				{Service: "litestream", Name: "auth-blue-litestream-1", State: "running"},
			},
		},
		{
			name:     "multiple sidecars",
			psOutput: "auth\tauth-blue-auth-1\tUp 2 minutes (healthy)\nredis\tauth-blue-redis-1\tUp 2 minutes (healthy)\nlitestream\tauth-blue-litestream-1\tUp 2 minutes",
			sidecars: sidecars,
			want: []ContainerStatus{
				{Service: "redis", Name: "auth-blue-redis-1", State: "healthy"},
				{Service: "litestream", Name: "auth-blue-litestream-1", State: "running"},
			},
		},
		{
			name:     "malformed line ignored",
			psOutput: "bad-line-no-tabs\nredis\tauth-blue-redis-1\tUp 2 minutes (healthy)",
			sidecars: sidecars,
			want: []ContainerStatus{
				{Service: "redis", Name: "auth-blue-redis-1", State: "healthy"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSidecarStatuses(tt.psOutput, tt.sidecars)
			if len(got) != len(tt.want) {
				t.Fatalf("parseSidecarStatuses() returned %d items, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("parseSidecarStatuses()[%d] = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}
