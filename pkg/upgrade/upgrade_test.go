package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pangobit/warpgate/pkg/version"
)

type fakeReleaseFetcher struct {
	release *Release
	err     error
}

// FetchRelease implements ReleaseFetcher for tests.
func (f fakeReleaseFetcher) FetchRelease(context.Context, string, string, string) (*Release, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.release, nil
}

type fakeDownloader struct {
	data []byte
	err  error
}

// DownloadVerified implements AssetDownloader for tests.
func (f fakeDownloader) DownloadVerified(context.Context, string, string, string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.data, nil
}

type recordingInstaller struct {
	installedPath string
	installedData []byte
	restored      bool
	installErr    error
	restoreErr    error
}

// Install implements BinaryInstaller for tests.
func (r *recordingInstaller) Install(path string, data []byte) error {
	if r.installErr != nil {
		return r.installErr
	}
	r.installedPath = path
	r.installedData = append([]byte(nil), data...)
	return nil
}

// RestoreBackup implements BinaryInstaller for tests.
func (r *recordingInstaller) RestoreBackup(string) error {
	r.restored = true
	return r.restoreErr
}

type fakeService struct {
	exists   bool
	active   bool
	stop     int
	start    int
	stopErr  error
	startErr error
}

// State implements ServiceController for tests.
func (f *fakeService) State(string) (bool, bool, error) {
	return f.exists, f.active, nil
}

// Stop implements ServiceController for tests.
func (f *fakeService) Stop(string) error {
	f.stop++
	return f.stopErr
}

// Start implements ServiceController for tests.
func (f *fakeService) Start(string) error {
	f.start++
	return f.startErr
}

func TestAssetNameForPlatform(t *testing.T) {
	got, err := assetNameForPlatform("linux", "amd64")
	if err != nil {
		t.Fatalf("assetNameForPlatform() error = %v", err)
	}
	if got != "warpgate-linux-amd64" {
		t.Fatalf("assetNameForPlatform() = %q, want warpgate-linux-amd64", got)
	}

	if _, err := assetNameForPlatform("darwin", "arm64"); err == nil {
		t.Fatal("assetNameForPlatform() expected error for unsupported platform")
	}
}

func TestParseChecksums(t *testing.T) {
	data := []byte("abc123  warpgate-linux-amd64\ndef456  checksums.txt\n")
	checksums, err := parseChecksums(data)
	if err != nil {
		t.Fatalf("parseChecksums() error = %v", err)
	}
	if checksums["warpgate-linux-amd64"] != "abc123" {
		t.Fatalf("parseChecksums() = %#v", checksums)
	}
}

func TestParseChecksumsRejectsInvalidLine(t *testing.T) {
	if _, err := parseChecksums([]byte("only-one-field\n")); err == nil {
		t.Fatal("parseChecksums() expected error for invalid line")
	}
}

func TestFileInstallerInstall(t *testing.T) {
	dir := t.TempDir()
	installPath := filepath.Join(dir, "warpgate")
	if err := os.WriteFile(installPath, []byte("old-binary"), 0755); err != nil {
		t.Fatalf("seed install binary: %v", err)
	}

	installer := FileInstaller{}
	newData := []byte("new-binary")
	if err := installer.Install(installPath, newData); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	got, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("installed binary = %q, want new-binary", string(got))
	}

	backup, err := os.ReadFile(installPath + ".bak")
	if err != nil {
		t.Fatalf("read backup binary: %v", err)
	}
	if string(backup) != "old-binary" {
		t.Fatalf("backup binary = %q, want old-binary", string(backup))
	}
}

func TestFileInstallerRequiresWritableDirectory(t *testing.T) {
	dir := t.TempDir()
	installPath := filepath.Join(dir, "warpgate")
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod install dir: %v", err)
	}

	installer := FileInstaller{}
	if err := installer.Install(installPath, []byte("data")); err == nil {
		t.Fatal("Install() expected permission error")
	} else if !strings.Contains(err.Error(), "sudo") {
		t.Fatalf("Install() error = %v, want sudo guidance", err)
	}
}

func TestUpgraderSkipsWhenAlreadyAtTargetVersion(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })
	version.Version = "v1.2.3"

	var output bytes.Buffer
	upgrader := Upgrader{
		Releases: fakeReleaseFetcher{release: &Release{Tag: "v1.2.3"}},
		Output:   &output,
	}

	err := upgrader.Run(context.Background(), Options{InstallPath: t.TempDir() + "/warpgate"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(output.String(), "Already at v1.2.3") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestUpgraderDryRun(t *testing.T) {
	var output bytes.Buffer
	upgrader := Upgrader{
		Releases: fakeReleaseFetcher{release: &Release{
			Tag:       "v1.2.3",
			AssetName: "warpgate-linux-amd64",
		}},
		Service: &fakeService{exists: true, active: true},
		Output:  &output,
	}

	err := upgrader.Run(context.Background(), Options{
		InstallPath: t.TempDir() + "/warpgate",
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(output.String(), "Target: v1.2.3") {
		t.Fatalf("dry-run output = %q", output.String())
	}
	if !strings.Contains(output.String(), "Would stop and start warpgate.service") {
		t.Fatalf("dry-run output = %q", output.String())
	}
}

func TestUpgraderRestoresBackupOnStartFailure(t *testing.T) {
	var output bytes.Buffer
	service := &fakeService{exists: true, active: true, startErr: errors.New("start failed")}
	installer := &recordingInstaller{}
	upgrader := Upgrader{
		Releases: fakeReleaseFetcher{release: &Release{
			Tag:          "v2.0.0",
			AssetName:    "warpgate-linux-amd64",
			AssetURL:     "https://example.com/binary",
			ChecksumsURL: "https://example.com/checksums.txt",
		}},
		Downloads: fakeDownloader{data: []byte("new-binary")},
		Install:   installer,
		Service:   service,
		Output:    &output,
	}

	installPath := filepath.Join(t.TempDir(), "warpgate")
	err := upgrader.Run(context.Background(), Options{InstallPath: installPath})
	if err == nil {
		t.Fatal("Run() expected start failure error")
	}
	if !installer.restored {
		t.Fatal("expected backup restore after start failure")
	}
	if service.stop != 1 || service.start != 2 {
		t.Fatalf("service stop/start counts = %d/%d, want 1/2", service.stop, service.start)
	}
}

func TestHTTPDownloaderDownloadVerified(t *testing.T) {
	binary := []byte("binary-data")
	sum := sha256.Sum256(binary)
	checksumsBody := hex.EncodeToString(sum[:]) + "  warpgate-linux-amd64\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			_, _ = w.Write(binary)
		case "/checksums":
			_, _ = w.Write([]byte(checksumsBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	downloader := HTTPDownloader{HTTPClient: server.Client()}
	data, err := downloader.DownloadVerified(
		context.Background(),
		server.URL+"/binary",
		server.URL+"/checksums",
		"warpgate-linux-amd64",
	)
	if err != nil {
		t.Fatalf("DownloadVerified() error = %v", err)
	}
	if string(data) != string(binary) {
		t.Fatalf("DownloadVerified() = %q, want %q", string(data), string(binary))
	}
}

func TestHTTPDownloaderRejectsChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			_, _ = w.Write([]byte("binary-data"))
		case "/checksums":
			_, _ = w.Write([]byte("deadbeef  warpgate-linux-amd64\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	downloader := HTTPDownloader{HTTPClient: server.Client()}
	if _, err := downloader.DownloadVerified(
		context.Background(),
		server.URL+"/binary",
		server.URL+"/checksums",
		"warpgate-linux-amd64",
	); err == nil {
		t.Fatal("DownloadVerified() expected checksum mismatch error")
	}
}
