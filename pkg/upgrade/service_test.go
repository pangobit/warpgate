package upgrade

import (
	"errors"
	"testing"
)

func TestUnitExistsFromLoadState(t *testing.T) {
	tests := []struct {
		name      string
		loadState string
		want      bool
	}{
		{name: "loaded", loadState: "loaded", want: true},
		{name: "not found", loadState: "not-found", want: false},
		{name: "empty", loadState: "", want: false},
		{name: "whitespace loaded", loadState: "  loaded  ", want: true},
		{name: "whitespace not found", loadState: "  not-found  ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unitExistsFromLoadState(tt.loadState); got != tt.want {
				t.Fatalf("unitExistsFromLoadState(%q) = %v, want %v", tt.loadState, got, tt.want)
			}
		})
	}
}

func TestIsInactiveUnit(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "inactive", message: "systemctl is-active warpgate.service: inactive", want: true},
		{name: "failed", message: "systemctl is-active warpgate.service: failed", want: true},
		{name: "deactivating", message: "systemctl is-active warpgate.service: deactivating", want: true},
		{name: "other error", message: "systemctl is-active warpgate.service: permission denied", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInactiveUnit(errors.New(tt.message)); got != tt.want {
				t.Fatalf("isInactiveUnit(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}
