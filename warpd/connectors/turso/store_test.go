package turso

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pangobit/warpgate/warpd/internal/stackstate"
)

func TestStorePersistsStackState(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "warpgate.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	}()

	empty, err := store.StackState(ctx)
	if err != nil {
		t.Fatalf("StackState() on empty store error = %v", err)
	}
	if len(empty.LastHealthy.Releases) != 0 || empty.LastAttempt != nil {
		t.Fatalf("empty stack state = %+v", empty)
	}

	finished := time.Unix(200, 0).UTC()
	state := stackstate.State{
		LastHealthy: stackstate.Snapshot{
			Releases:  map[string]string{"api": "rel-1", "worker": "rel-2"},
			UpdatedAt: time.Unix(100, 0).UTC(),
		},
		LastAttempt: &stackstate.Attempt{
			ID:         "stack-1",
			Status:     stackstate.StatusSucceeded,
			Releases:   map[string]string{"api": "rel-1", "worker": "rel-2"},
			ActorEmail: "ray@ssh",
			StartedAt:  time.Unix(150, 0).UTC(),
			FinishedAt: &finished,
		},
	}
	if err := store.SaveStackState(ctx, state); err != nil {
		t.Fatalf("SaveStackState() error = %v", err)
	}
	got, err := store.StackState(ctx)
	if err != nil {
		t.Fatalf("StackState() error = %v", err)
	}
	if got.LastHealthy.Releases["api"] != "rel-1" || got.LastHealthy.Releases["worker"] != "rel-2" {
		t.Fatalf("baseline = %+v", got.LastHealthy.Releases)
	}
	if got.LastAttempt == nil || got.LastAttempt.Status != stackstate.StatusSucceeded {
		t.Fatalf("last attempt = %+v", got.LastAttempt)
	}
	if got.LastAttempt.FinishedAt == nil || !got.LastAttempt.FinishedAt.Equal(finished) {
		t.Fatalf("finished at = %v, want %v", got.LastAttempt.FinishedAt, finished)
	}
}
