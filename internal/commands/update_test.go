package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entropix-in/laradev/internal/version"
)

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{current: "dev", latest: "v1.0.0", want: false},
		{current: "v1.0.0", latest: "v1.0.0", want: true},
		{current: "v1.1.0", latest: "v1.0.0", want: true},
		{current: "v1.0.0", latest: "v1.1.0", want: false},
		{current: "v1.0.0-rc.1", latest: "v1.0.0", want: false},
	}
	for _, tt := range tests {
		got, err := versionAtLeast(tt.current, tt.latest)
		if err != nil {
			t.Errorf("versionAtLeast(%q, %q): %v", tt.current, tt.latest, err)
			continue
		}
		if got != tt.want {
			t.Errorf("versionAtLeast(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestUpdateDownloadsAndReplacesExecutable(t *testing.T) {
	oldVersion := version.Value
	version.Value = "v1.0.0"
	t.Cleanup(func() { version.Value = oldVersion })

	binary := []byte("new laradev binary")
	archive := releaseArchive(t, binary)
	digest := sha256.Sum256(archive)
	checksum := hex.EncodeToString(digest[:]) + "  " + releaseAsset + "\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_, _ = io.WriteString(w, `{"tag_name":"v1.1.0","draft":false,"prerelease":false}`)
		case "/download/v1.1.0/" + releaseAsset:
			_, _ = w.Write(archive)
		case "/download/v1.1.0/" + releaseChecksumAsset:
			_, _ = io.WriteString(w, checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "laradev")
	if err := os.WriteFile(path, []byte("old laradev binary"), 0755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Update(context.Background(), updateOptions{
		Client:          server.Client(),
		APIURL:          server.URL + "/latest",
		DownloadBaseURL: server.URL + "/download",
		ExecutablePath:  path,
		GOOS:            "linux",
		GOARCH:          "amd64",
		Out:             &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Fatalf("updated binary = %q, want %q", got, binary)
	}
	if !strings.Contains(out.String(), "updated laradev from v1.0.0 to v1.1.0") {
		t.Fatalf("unexpected update output: %s", out.String())
	}
}

func TestUpdateCheckDoesNotReplaceCurrentExecutable(t *testing.T) {
	oldVersion := version.Value
	version.Value = "v1.1.0"
	t.Cleanup(func() { version.Value = oldVersion })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"tag_name":"v1.1.0","draft":false,"prerelease":false}`)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "laradev")
	if err := os.WriteFile(path, []byte("current"), 0755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Update(context.Background(), updateOptions{
		Client:         server.Client(),
		APIURL:         server.URL,
		ExecutablePath: path,
		GOOS:           "linux",
		GOARCH:         "amd64",
		CheckOnly:      true,
		Out:            &out,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "current" {
		t.Fatalf("check-only changed executable to %q", got)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatalf("unexpected check output: %s", out.String())
	}
}

func TestUpdateRejectsUnsupportedPlatform(t *testing.T) {
	err := Update(context.Background(), updateOptions{GOOS: "darwin", GOARCH: "arm64"})
	if err == nil || !strings.Contains(err.Error(), "only on linux/amd64") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyChecksumRejectsMismatch(t *testing.T) {
	if err := verifyChecksum([]byte("archive"), []byte(strings.Repeat("0", 64)+"  file\n")); err == nil {
		t.Fatal("verifyChecksum unexpectedly accepted a mismatch")
	}
}

func releaseArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: releaseBinary, Mode: 0755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestReleaseDownloadURL(t *testing.T) {
	got := releaseDownloadURL("https://example.test/download/", "v1.0.0", releaseAsset)
	want := "https://example.test/download/v1.0.0/" + releaseAsset
	if got != want {
		t.Fatalf("releaseDownloadURL() = %q, want %q", got, want)
	}
}

func TestExtractBinaryRequiresExpectedName(t *testing.T) {
	archive := releaseArchiveWithName(t, "other", []byte("binary"))
	if _, err := extractBinary(archive); err == nil || !strings.Contains(err.Error(), "does not contain laradev") {
		t.Fatalf("unexpected extract error: %v", err)
	}
}

func releaseArchiveWithName(t *testing.T, name string, binary []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
