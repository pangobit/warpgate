package deploy

import (
	"testing"
)

func TestHealthStatusConstants(t *testing.T) {
	tests := []struct {
		name   string
		status HealthStatus
		want   int
	}{
		{"starting", HealthStarting, 0},
		{"healthy", HealthHealthy, 1},
		{"unhealthy", HealthUnhealthy, 2},
		{"none", HealthNone, 3},
		{"not found", HealthNotFound, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.status) != tt.want {
				t.Errorf("expected %d, got %d", tt.want, int(tt.status))
			}
		})
	}
}
