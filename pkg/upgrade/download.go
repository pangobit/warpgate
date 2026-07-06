package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
)

// HTTPDownloader downloads release assets over HTTP.
type HTTPDownloader struct {
	// HTTPClient performs asset download requests.
	HTTPClient *http.Client
}

// DownloadVerified downloads an asset and verifies its SHA-256 checksum.
func (d *HTTPDownloader) DownloadVerified(ctx context.Context, assetURL, checksumsURL, assetName string) ([]byte, error) {
	checksumsData, err := d.download(ctx, checksumsURL)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	checksums, err := parseChecksums(checksumsData)
	if err != nil {
		return nil, err
	}
	expected, ok := checksums[assetName]
	if !ok {
		return nil, fmt.Errorf("checksums file missing entry for %q", assetName)
	}

	data, err := d.download(ctx, assetURL)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", assetName, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != expected {
		return nil, fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return data, nil
}

func (d *HTTPDownloader) download(ctx context.Context, url string) ([]byte, error) {
	client := d.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s: %s", resp.Status, string(body))
	}
	return io.ReadAll(resp.Body)
}
