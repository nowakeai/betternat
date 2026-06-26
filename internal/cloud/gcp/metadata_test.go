package gcpcloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveLocalInstanceIDReadsGCPMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/computeMetadata/v1/instance/name" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Metadata-Flavor") != "Google" {
			t.Fatalf("missing metadata header: %#v", r.Header)
		}
		_, _ = w.Write([]byte("bnat-gcp-gw-a\n"))
	}))
	defer server.Close()
	restore := overrideMetadataBaseURL(server.URL + "/computeMetadata/v1")
	defer restore()

	got, err := ResolveLocalInstanceID(context.Background(), "us-west1")
	if err != nil {
		t.Fatalf("resolve metadata: %v", err)
	}
	if got != "bnat-gcp-gw-a" {
		t.Fatalf("unexpected instance id: %s", got)
	}
}

func TestResolveLocalInstanceIDRequiresSuccessfulMetadataResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "metadata unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	restore := overrideMetadataBaseURL(server.URL + "/computeMetadata/v1")
	defer restore()

	_, err := ResolveLocalInstanceID(context.Background(), "us-west1")
	if err == nil {
		t.Fatal("expected metadata error")
	}
}

func overrideMetadataBaseURL(value string) func() {
	previous := metadataBaseURL
	metadataBaseURL = value
	return func() {
		metadataBaseURL = previous
	}
}
