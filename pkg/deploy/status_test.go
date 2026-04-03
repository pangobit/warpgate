package deploy

import (
	"testing"
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
