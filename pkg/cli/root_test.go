package cli

import (
	"os"
	"testing"
)

func TestPreviewCommandSkipsConfigPreload(t *testing.T) {
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
		configPath = ""
	})
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	if err := rootCmd.PersistentPreRunE(previewCmd, nil); err != nil {
		t.Fatalf("preview pre-run should not require cluster.yml: %v", err)
	}
}
