package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pangobit/warpgate/pkg/config"
)

func TestBuildReleaseManifestUsesDigestComposeAndEnvHash(t *testing.T) {
	app := &config.AppConfig{
		Name:        "api",
		Image:       "ghcr.io/acme/api",
		ImageTag:    "v1.2.3",
		ImageDigest: "sha256:abc123",
		Environment: map[string]string{
			"LOG_LEVEL": "debug",
			"APP_ENV":   "prod",
		},
		SecretsPrefix: "api/prod",
	}
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	first := Build(app, []byte("services:\n  api:\n"), now)
	second := Build(app, []byte("services:\n  api:\n"), now)

	if first.ID == "" {
		t.Fatal("expected release ID")
	}
	if first.ID != second.ID {
		t.Fatalf("release ID is not deterministic: %q != %q", first.ID, second.ID)
	}
	if first.ImageRef != "ghcr.io/acme/api@sha256:abc123" {
		t.Errorf("ImageRef = %q, want digest ref", first.ImageRef)
	}
	if !strings.HasPrefix(first.ComposeRev, "sha256:") {
		t.Errorf("ComposeRev = %q, want sha256 digest", first.ComposeRev)
	}
	if !strings.HasPrefix(first.EnvHash, "sha256:") {
		t.Errorf("EnvHash = %q, want sha256 digest", first.EnvHash)
	}
}

func TestBuildReleaseManifestChangesIDWhenEnvChanges(t *testing.T) {
	base := &config.AppConfig{
		Name:        "api",
		Image:       "ghcr.io/acme/api",
		ImageDigest: "sha256:abc123",
		Environment: map[string]string{
			"LOG_LEVEL": "info",
		},
	}
	changed := &config.AppConfig{
		Name:        "api",
		Image:       "ghcr.io/acme/api",
		ImageDigest: "sha256:abc123",
		Environment: map[string]string{
			"LOG_LEVEL": "debug",
		},
	}

	first := Build(base, []byte("services:\n  api:\n"), time.Time{})
	second := Build(changed, []byte("services:\n  api:\n"), time.Time{})

	if first.ID == second.ID {
		t.Fatal("expected env change to produce a different release ID")
	}
}

func TestSaveAndLoadReleaseManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := &Manifest{
		ID:         "abc123",
		App:        "api",
		ImageRef:   "ghcr.io/acme/api@sha256:abc123",
		ComposeRev: "main",
		EnvHash:    "sha256:env",
		CreatedAt:  time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
	}

	if err := Save(dir, manifest); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	for _, name := range []string{"abc123.json", "latest.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}

	loaded, err := Load(dir, "latest")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.ID != manifest.ID {
		t.Errorf("loaded ID = %q, want %q", loaded.ID, manifest.ID)
	}
}
