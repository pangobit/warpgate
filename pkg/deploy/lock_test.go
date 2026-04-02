package deploy

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestLockInfoMarshal(t *testing.T) {
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	info := LockInfo{
		Host:       "dev-machine",
		User:       "ray",
		AcquiredAt: ts,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded LockInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Host != "dev-machine" {
		t.Errorf("expected host dev-machine, got %s", decoded.Host)
	}
	if decoded.User != "ray" {
		t.Errorf("expected user ray, got %s", decoded.User)
	}
	if !decoded.AcquiredAt.Equal(ts) {
		t.Errorf("expected acquired_at %v, got %v", ts, decoded.AcquiredAt)
	}
}

func TestLockInfoUnmarshalInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"invalid json", "{bad"},
		{"wrong type", `{"host": 123}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info LockInfo
			if err := json.Unmarshal([]byte(tt.input), &info); err == nil {
				t.Error("expected error for invalid input")
			}
		})
	}
}

func TestCurrentUser(t *testing.T) {
	original := os.Getenv("USER")
	defer os.Setenv("USER", original)

	os.Setenv("USER", "testuser")
	if got := currentUser(); got != "testuser" {
		t.Errorf("expected testuser, got %s", got)
	}

	os.Unsetenv("USER")
	if got := currentUser(); got != "unknown" {
		t.Errorf("expected unknown, got %s", got)
	}
}

func TestLockTimeoutValue(t *testing.T) {
	if lockTimeout != 30*time.Minute {
		t.Errorf("expected 30m lock timeout, got %v", lockTimeout)
	}
}
