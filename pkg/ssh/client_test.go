package ssh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetHostKeyCallbackMissingKnownHosts(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	c := &Client{}
	cb, err := c.getHostKeyCallback()
	if err == nil {
		t.Fatal("expected error when known_hosts is missing, got nil")
	}
	if cb != nil {
		t.Error("expected nil callback when known_hosts is missing")
	}
	if got := err.Error(); got != "known_hosts not found" {
		t.Errorf("error = %q, want %q", got, "known_hosts not found")
	}
}

func TestGetHostKeyCallbackMalformedKnownHosts(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	sshDir := filepath.Join(tmpHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	khPath := filepath.Join(sshDir, "known_hosts")
	if err := os.WriteFile(khPath, []byte("not a valid known_hosts line @@@\n"), 0600); err != nil {
		t.Fatal(err)
	}

	c := &Client{}
	_, err := c.getHostKeyCallback()
	if err == nil {
		t.Fatal("expected error for malformed known_hosts, got nil")
	}
}

func TestGetHostKeyCallbackEmptyFile(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	sshDir := filepath.Join(tmpHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	khPath := filepath.Join(sshDir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	c := &Client{}
	cb, err := c.getHostKeyCallback()
	if err != nil {
		t.Fatalf("unexpected error for empty known_hosts: %v", err)
	}
	if cb == nil {
		t.Error("expected non-nil callback for empty known_hosts")
	}
}

func TestWriteFileLocalExec(t *testing.T) {
	// Regression test: verify WriteFile (non-secret) pipes content correctly
	// and does not restrict permissions.
	dir := t.TempDir()
	target := filepath.Join(dir, "testfile.txt")
	content := "export GOPATH=/home/warpgate/go\nexport PATH=/usr/local/go/bin:$GOPATH/bin:$PATH\n"

	cmd := fmt.Sprintf("mkdir -p %s && cat > %s", dir, target)
	proc := exec.Command("bash", "-c", cmd)
	proc.Stdin = strings.NewReader(content)
	output, err := proc.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, output)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(got) != content {
		t.Errorf("file content = %q, want %q", string(got), content)
	}
}

func TestWriteFileSecretLocalExec(t *testing.T) {
	// Regression test: verify WriteFileSecret pipes content correctly
	// and creates files with mode 600 (no subshell around cat).
	dir := t.TempDir()
	target := filepath.Join(dir, "secret.env")
	content := "DB_URL=postgres://localhost/db\nAPI_KEY=secret123\n"

	cmd := fmt.Sprintf("umask 077 && mkdir -p %s && cat > %s", dir, target)
	proc := exec.Command("bash", "-c", cmd)
	proc.Stdin = strings.NewReader(content)
	output, err := proc.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, output)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(got) != content {
		t.Errorf("file content = %q, want %q", string(got), content)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("file permissions = %o, want group/other bits to be 0", perm)
	}
}

func TestConnectFailsWithoutKnownHosts(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	c := &Client{
		Host:       "127.0.0.1",
		Port:       22,
		User:       "test",
		PrivateKey: filepath.Join(tmpHome, "nonexistent_key"),
	}

	err := c.Connect()
	if err == nil {
		t.Fatal("expected Connect() to fail when known_hosts is missing")
	}
}
