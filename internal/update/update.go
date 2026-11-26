// Package update finds bfc releases and downloads them, for bfc update
// (ADR 0018). It reads GitHub's release pages, not the GitHub API: the
// latest release comes from the redirect of <base>/releases/latest, and
// the assets from <base>/releases/download/<tag>/<file>.
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// DefaultBase is the repository whose releases bfc update installs. The
// variable BFC_RELEASES_URL replaces it, for tests and mirrors.
const DefaultBase = "https://github.com/admSla99/BeforeCode"

// ErrNoRelease is returned when the named release does not exist.
var ErrNoRelease = errors.New("no such release")

// maxBinary bounds the size of an extracted binary.
const maxBinary = 512 << 20

// client does not follow redirects, so that Latest can read the tag from
// the Location header, and gives up on a stalled server.
var client = &http.Client{
	Timeout: 5 * time.Minute,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// downloads follows the redirects GitHub sends for release assets.
var downloads = &http.Client{Timeout: 5 * time.Minute}

// Latest returns the tag of the latest release, such as v0.2.0, read from
// the redirect of <base>/releases/latest to <base>/releases/tag/<tag>.
// It never returns a pre-release.
func Latest(ctx context.Context, base string) (string, error) {
	u := strings.TrimSuffix(base, "/") + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("look up the latest release: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("look up the latest release: %s answered %s, not a redirect to the release", u, resp.Status)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		return "", fmt.Errorf("look up the latest release: bad redirect: %w", err)
	}
	dir, tag := path.Split(loc.Path)
	if !strings.HasSuffix(dir, "/releases/tag/") || !semver.IsValid(tag) {
		return "", fmt.Errorf("look up the latest release: %s redirects to %q, not to a release tag", u, resp.Header.Get("Location"))
	}
	if semver.Prerelease(tag) != "" {
		return "", fmt.Errorf("look up the latest release: %s points to the pre-release %s", u, tag)
	}
	return tag, nil
}

// ArchiveName is the release archive for tag, goos and goarch, as
// GoReleaser names it: bfc_0.2.0_linux_amd64.tar.gz, .zip on Windows.
func ArchiveName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("bfc_%s_%s_%s%s", strings.TrimPrefix(tag, "v"), goos, goarch, ext)
}

// Download fetches the archive of release tag for goos and goarch into
// dir, checks its SHA-256 against the release's checksums.txt and
// extracts the binary, bfc or bfc.exe. It returns the binary's path. On
// any error no binary is left in dir.
func Download(ctx context.Context, base, tag, goos, goarch, dir string) (string, error) {
	base = strings.TrimSuffix(base, "/") + "/releases/download/" + tag + "/"
	name := ArchiveName(tag, goos, goarch)

	sums, err := fetch(ctx, base+"checksums.txt")
	if errors.Is(err, errNotFound) {
		return "", fmt.Errorf("%w: bfc %s (no %s)", ErrNoRelease, tag, base+"checksums.txt")
	}
	if err != nil {
		return "", fmt.Errorf("download checksums.txt of bfc %s: %w", tag, err)
	}
	want, ok := checksum(sums, name)
	if !ok {
		return "", fmt.Errorf("checksums.txt of bfc %s has no line for %s", tag, name)
	}

	archive, err := fetch(ctx, base+name)
	if errors.Is(err, errNotFound) {
		return "", fmt.Errorf("release bfc %s has no archive %s for %s/%s", tag, name, goos, goarch)
	}
	if err != nil {
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", name, want, got)
	}

	binName := "bfc"
	if goos == "windows" {
		binName = "bfc.exe"
	}
	var bin []byte
	if goos == "windows" {
		bin, err = fromZip(archive, binName)
	} else {
		bin, err = fromTarGz(archive, binName)
	}
	if err != nil {
		return "", fmt.Errorf("unpack %s: %w", name, err)
	}
	dst := filepath.Join(dir, binName)
	if err := os.WriteFile(dst, bin, 0o755); err != nil { //nolint:gosec // the binary must be executable
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}

var errNotFound = errors.New("not found")

func fetch(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := downloads.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", u, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBinary+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBinary {
		return nil, fmt.Errorf("%s is larger than %d bytes", u, maxBinary)
	}
	return data, nil
}

// checksum finds the SHA-256 of name in a checksums.txt, whose lines are
// "<hex sum>  <file name>".
func checksum(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

func fromTarGz(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == name {
			return io.ReadAll(io.LimitReader(tr, maxBinary))
		}
	}
}

func fromZip(archive []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if path.Base(f.Name) != name || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(io.LimitReader(rc, maxBinary))
	}
	return nil, fmt.Errorf("no %s in the archive", name)
}
