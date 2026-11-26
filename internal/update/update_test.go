package update_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/update"
	"github.com/admSla99/BeforeCode/internal/update/updatetest"
)

func TestLatestReadsTagFromRedirect(t *testing.T) {
	t.Parallel()
	srv := updatetest.New(t, "v0.2.0")
	tag, err := update.Latest(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.2.0" {
		t.Errorf("Latest = %q, want v0.2.0", tag)
	}
	if got := srv.Requests(); len(got) != 1 || got[0] != "/releases/latest" {
		t.Errorf("requests = %v, want only /releases/latest (the redirect is not followed)", got)
	}
}

func TestLatestWithoutRedirectFails(t *testing.T) {
	t.Parallel()
	srv := updatetest.New(t, "")
	if _, err := update.Latest(context.Background(), srv.URL); err == nil {
		t.Fatal("Latest without a release succeeded")
	}
}

func TestLatestMalformedRedirectFails(t *testing.T) {
	t.Parallel()
	for name, location := range map[string]string{
		"not a tag page": "/releases",
		"not a version":  "/releases/tag/nightly",
		"empty tag":      "/releases/tag/",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, location, http.StatusFound)
		}))
		_, err := update.Latest(context.Background(), srv.URL)
		srv.Close()
		if err == nil {
			t.Errorf("%s: Latest with Location %q succeeded", name, location)
		}
	}
}

// AC-14: releases/latest never points to a pre-release; should it, bfc
// still does not take one as the latest.
func TestLatestRefusesPreRelease(t *testing.T) {
	t.Parallel()
	srv := updatetest.New(t, "v0.3.0-rc.1")
	if _, err := update.Latest(context.Background(), srv.URL); err == nil {
		t.Fatal("Latest returned a pre-release")
	}
}

func TestDownloadExtractsBinary(t *testing.T) {
	t.Parallel()
	for _, platform := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}} {
		goos, goarch := platform[0], platform[1]
		srv := updatetest.New(t, "v0.2.0", updatetest.Release{Tag: "v0.2.0", GOOS: goos, GOARCH: goarch, Binary: []byte("new bfc")})
		path, err := update.Download(context.Background(), srv.URL, "v0.2.0", goos, goarch, t.TempDir())
		if err != nil {
			t.Fatalf("%s/%s: %v", goos, goarch, err)
		}
		want := "bfc"
		if goos == "windows" {
			want = "bfc.exe"
		}
		if filepath.Base(path) != want {
			t.Errorf("%s/%s: binary at %s, want a file named %s", goos, goarch, path, want)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "new bfc" {
			t.Errorf("%s/%s: binary holds %q", goos, goarch, data)
		}
		if goos != "windows" {
			if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o100 == 0 && os.PathSeparator == '/' {
				t.Errorf("%s/%s: binary is not executable: %v", goos, goarch, info.Mode())
			}
		}
	}
}

// AC-6: a version with no release fails with a message naming it.
func TestDownloadUnknownRelease(t *testing.T) {
	t.Parallel()
	srv := updatetest.New(t, "v0.2.0", updatetest.Release{Tag: "v0.2.0", Binary: []byte("x")})
	_, err := update.Download(context.Background(), srv.URL, "v0.1.5", "linux", "amd64", t.TempDir())
	if !errors.Is(err, update.ErrNoRelease) {
		t.Fatalf("err = %v, want ErrNoRelease", err)
	}
	if !strings.Contains(err.Error(), "v0.1.5") {
		t.Errorf("error %q does not name v0.1.5", err)
	}
}

// AC-7: a failed download, a missing checksum line or a mismatch is an
// error that says why.
func TestDownloadFailures(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		release updatetest.Release
		want    []string
	}{
		"missing asset":         {updatetest.Release{NoArchive: true}, []string{"bfc_0.2.0_linux_amd64.tar.gz"}},
		"missing checksum line": {updatetest.Release{NoChecksumLine: true}, []string{"checksums.txt", "bfc_0.2.0_linux_amd64.tar.gz"}},
		"checksum mismatch":     {updatetest.Release{BadChecksum: true}, []string{"expected " + strings.Repeat("0", 64), "got "}},
	} {
		r := tc.release
		r.Tag, r.GOOS, r.GOARCH, r.Binary = "v0.2.0", "linux", "amd64", []byte("new bfc")
		srv := updatetest.New(t, "v0.2.0", r)
		dir := t.TempDir()
		path, err := update.Download(context.Background(), srv.URL, "v0.2.0", "linux", "amd64", dir)
		if err == nil {
			t.Errorf("%s: Download succeeded with %s", name, path)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q lacks %q", name, err, w)
			}
		}
		if _, statErr := os.Stat(filepath.Join(dir, "bfc")); statErr == nil {
			t.Errorf("%s: a binary was extracted despite the error", name)
		}
	}
}

func TestDownloadFromEmptyServer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	if _, err := update.Download(context.Background(), srv.URL, "v0.2.0", "linux", "amd64", t.TempDir()); err == nil {
		t.Fatal("Download from an empty server succeeded")
	}
}
