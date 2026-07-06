package stackstate_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/stackstate"
)

func TestSnapshotJSONBackwardCompat(t *testing.T) {
	legacy := `{"releases":{"api":"rel-1"},"updated_at":"2026-01-02T03:04:05Z"}`
	var snapshot stackstate.Snapshot
	if err := json.Unmarshal([]byte(legacy), &snapshot); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if snapshot.Releases["api"] != "rel-1" {
		t.Fatalf("releases = %#v, want api=rel-1", snapshot.Releases)
	}
	if snapshot.ClusterFileSHA != "" {
		t.Fatalf("cluster sha = %q, want empty for legacy payload", snapshot.ClusterFileSHA)
	}
	if len(snapshot.AppConfigSHAs) != 0 {
		t.Fatalf("app shas = %#v, want empty for legacy payload", snapshot.AppConfigSHAs)
	}
}

func TestAttemptJSONRoundTrip(t *testing.T) {
	finished := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	attempt := stackstate.Attempt{
		ID:           "stack-1",
		Status:       stackstate.StatusSucceeded,
		Releases:     map[string]string{"api": "rel-1"},
		ActorEmail:   "admin@example.com",
		StartedAt:    time.Date(2026, time.January, 2, 3, 0, 0, 0, time.UTC),
		FinishedAt:   &finished,
		DeployedApps: []string{"api"},
		SkippedApps:  []string{"web"},
		Forced:       false,
	}
	data, err := json.Marshal(attempt)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded stackstate.Attempt
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(decoded.DeployedApps) != 1 || decoded.DeployedApps[0] != "api" {
		t.Fatalf("deployed apps = %#v, want [api]", decoded.DeployedApps)
	}
	if len(decoded.SkippedApps) != 1 || decoded.SkippedApps[0] != "web" {
		t.Fatalf("skipped apps = %#v, want [web]", decoded.SkippedApps)
	}
}
