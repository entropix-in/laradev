package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/entropix-in/laradev/internal/version"
)

const (
	releaseAPIURL        = "https://api.github.com/repos/entropix-in/laradev/releases/latest"
	releaseDownloadBase  = "https://github.com/entropix-in/laradev/releases/download"
	releaseAsset         = "laradev-linux-amd64.tar.gz"
	releaseChecksumAsset = releaseAsset + ".sha256"
	releaseBinary        = "laradev"
	maxReleaseMetadata   = 1 << 20
	maxReleaseChecksum   = 1 << 20
	maxReleaseArchive    = 100 << 20
	maxReleaseBinary     = 50 << 20
	defaultUpdateTimeout = 30 * time.Second
)

type releaseMetadata struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

type updateOptions struct {
	Client          *http.Client
	APIURL          string
	DownloadBaseURL string
	ExecutablePath  string
	GOOS            string
	GOARCH          string
	CheckOnly       bool
	Out             io.Writer
}

type releaseVersion struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease string
}

func Update(ctx context.Context, opts updateOptions) error {
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.GOOS != "linux" || opts.GOARCH != "amd64" {
		return fmt.Errorf("self-update is supported only on linux/amd64, not %s/%s", opts.GOOS, opts.GOARCH)
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: defaultUpdateTimeout}
	}
	if opts.APIURL == "" {
		opts.APIURL = releaseAPIURL
	}
	if opts.DownloadBaseURL == "" {
		opts.DownloadBaseURL = releaseDownloadBase
	}

	latest, err := fetchRelease(ctx, opts.Client, opts.APIURL)
	if err != nil {
		return err
	}
	current := version.Value
	upToDate, err := versionAtLeast(current, latest.TagName)
	if err != nil {
		return err
	}
	if upToDate {
		_, err := fmt.Fprintf(opts.Out, "laradev %s is up to date (%s)\n", current, latest.TagName)
		return err
	}
	if opts.CheckOnly {
		_, err := fmt.Fprintf(opts.Out, "update available: %s -> %s\n", current, latest.TagName)
		return err
	}

	executable, err := updateExecutable(opts.ExecutablePath)
	if err != nil {
		return err
	}
	archiveURL := releaseDownloadURL(opts.DownloadBaseURL, latest.TagName, releaseAsset)
	checksumURL := releaseDownloadURL(opts.DownloadBaseURL, latest.TagName, releaseChecksumAsset)
	archive, err := download(ctx, opts.Client, archiveURL, maxReleaseArchive)
	if err != nil {
		return fmt.Errorf("download release: %w", err)
	}
	checksum, err := download(ctx, opts.Client, checksumURL, maxReleaseChecksum)
	if err != nil {
		return fmt.Errorf("download release checksum: %w", err)
	}
	if err := verifyChecksum(archive, checksum); err != nil {
		return err
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return err
	}
	if err := replaceExecutable(executable, binary); err != nil {
		return err
	}
	_, err = fmt.Fprintf(opts.Out, "updated laradev from %s to %s\n", current, latest.TagName)
	return err
}

func fetchRelease(ctx context.Context, client *http.Client, apiURL string) (releaseMetadata, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return releaseMetadata{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "laradev/"+version.Value)
	response, err := client.Do(request)
	if err != nil {
		return releaseMetadata{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return releaseMetadata{}, fmt.Errorf("GitHub latest release returned HTTP %d", response.StatusCode)
	}
	var release releaseMetadata
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxReleaseMetadata))
	if err := decoder.Decode(&release); err != nil {
		return releaseMetadata{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if release.TagName == "" || release.Draft || release.Prerelease {
		return releaseMetadata{}, errors.New("GitHub latest release is missing or not stable")
	}
	if _, err := parseReleaseVersion(release.TagName); err != nil {
		return releaseMetadata{}, fmt.Errorf("invalid latest release version: %w", err)
	}
	return release, nil
}

func releaseDownloadURL(base, tag, asset string) string {
	return strings.TrimRight(base, "/") + "/" + tag + "/" + asset
}

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "laradev/"+version.Value)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download exceeds the allowed size")
	}
	return data, nil
}

func verifyChecksum(archive, checksum []byte) error {
	fields := strings.Fields(string(checksum))
	if len(fields) < 2 || filepath.Base(fields[1]) != releaseAsset || len(fields[0]) != sha256.Size*2 {
		return errors.New("invalid release checksum")
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil {
		return errors.New("invalid release checksum")
	}
	actual := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), hex.EncodeToString(expected)) {
		return errors.New("release checksum mismatch")
	}
	return nil
}

func extractBinary(archive []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if header.Name != releaseBinary || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size < 0 || header.Size > maxReleaseBinary {
			return nil, errors.New("release binary exceeds the allowed size")
		}
		binary, err := io.ReadAll(io.LimitReader(tarReader, maxReleaseBinary+1))
		if err != nil {
			return nil, err
		}
		if int64(len(binary)) != header.Size || int64(len(binary)) > maxReleaseBinary {
			return nil, errors.New("invalid release binary size")
		}
		return binary, nil
	}
	return nil, errors.New("release archive does not contain laradev")
}

func updateExecutable(path string) (string, error) {
	if path == "" {
		var err error
		path, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("find installed laradev: %w", err)
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect installed laradev: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("refusing to update a non-regular laradev executable")
	}
	if info.Mode()&0111 == 0 {
		return "", errors.New("installed laradev is not executable")
	}
	return path, nil
}

func replaceExecutable(path string, binary []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".laradev-update-")
	if err != nil {
		return fmt.Errorf("create update file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0755); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(binary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace installed laradev: %w", err)
	}
	return nil
}

func parseReleaseVersion(value string) (releaseVersion, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.SplitN(value, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(numbers) != 3 {
		return releaseVersion{}, fmt.Errorf("%q is not semver", value)
	}
	parsed := releaseVersion{}
	values := []*int{&parsed.Major, &parsed.Minor, &parsed.Patch}
	for i, number := range numbers {
		if number == "" || (len(number) > 1 && number[0] == '0') {
			return releaseVersion{}, fmt.Errorf("%q is not semver", value)
		}
		parsedNumber, err := strconv.Atoi(number)
		if err != nil || parsedNumber < 0 {
			return releaseVersion{}, fmt.Errorf("%q is not semver", value)
		}
		*values[i] = parsedNumber
	}
	if len(parts) == 2 {
		if parts[1] == "" {
			return releaseVersion{}, fmt.Errorf("%q is not semver", value)
		}
		parsed.Prerelease = parts[1]
	}
	return parsed, nil
}

func versionAtLeast(current, latest string) (bool, error) {
	latestVersion, err := parseReleaseVersion(latest)
	if err != nil {
		return false, err
	}
	if current == "dev" {
		return false, nil
	}
	currentVersion, err := parseReleaseVersion(current)
	if err != nil {
		return false, fmt.Errorf("current version %q is not semver", current)
	}
	if currentVersion.Major != latestVersion.Major {
		return currentVersion.Major > latestVersion.Major, nil
	}
	if currentVersion.Minor != latestVersion.Minor {
		return currentVersion.Minor > latestVersion.Minor, nil
	}
	if currentVersion.Patch != latestVersion.Patch {
		return currentVersion.Patch > latestVersion.Patch, nil
	}
	if currentVersion.Prerelease == latestVersion.Prerelease {
		return true, nil
	}
	return currentVersion.Prerelease == "" && latestVersion.Prerelease != "", nil
}
