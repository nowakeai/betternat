package gcpcloud

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultMetadataBaseURL = "http://metadata.google.internal/computeMetadata/v1"

var metadataHTTPClient = &http.Client{Timeout: 2 * time.Second}
var metadataBaseURL = defaultMetadataBaseURL

func ResolveLocalInstanceID(ctx context.Context, _ string) (string, error) {
	return metadataValue(ctx, "instance/name")
}

func metadataValue(ctx context.Context, path string) (string, error) {
	base := strings.TrimRight(metadataBaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+strings.TrimLeft(path, "/"), nil)
	if err != nil {
		return "", fmt.Errorf("build gcp metadata request: %w", err)
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := metadataHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("read gcp metadata %q: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("read gcp metadata %q body: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("read gcp metadata %q: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("read gcp metadata %q: empty value", path)
	}
	return value, nil
}
