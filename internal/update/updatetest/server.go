// Package updatetest serves fake bfc releases over a local HTTP server,
// laid out like GitHub's release pages, so that tests of bfc update need
// no network.
package updatetest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/admSla99/BeforeCode/internal/update"
)

// Release is one fake release with one archive.
type Release struct {
	Tag    string // for example v0.2.0
	GOOS   string // runtime.GOOS when empty
	GOARCH string // runtime.GOARCH when empty
	Binary []byte // the bfc binary in the archive

	BadChecksum    bool // checksums.txt names a wrong sum for the archive
	NoChecksumLine bool // checksums.txt lacks the archive's line
	NoArchive      bool // the archive is missing from the release
}

// Server is a fake release server. Its URL is the base URL bfc update
// takes, standing in for https://github.com/admSla99/BeforeCode.
type Server struct {
	URL string

	t        testing.TB
	mu       sync.Mutex
	latest   string
	assets   map[string][]byte // path under /releases/download/ -> content
	requests []string
}

// New starts a server whose /releases/latest redirects to latest (no
// redirect when latest is empty) and that serves the given releases. A
// request for any other path fails the test.
func New(t testing.TB, latest string, releases ...Release) *Server {
	t.Helper()
	s := &Server{t: t, latest: latest, assets: map[string][]byte{}}
	for _, r := range releases {
		s.add(r)
	}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

func (s *Server) add(r Release) {
	goos, goarch := r.GOOS, r.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	name := update.ArchiveName(r.Tag, goos, goarch)
	archive := Archive(s.t, goos, r.Binary)
	sum := sha256.Sum256(archive)
	hexSum := hex.EncodeToString(sum[:])
	if r.BadChecksum {
		hexSum = strings.Repeat("0", 64)
	}
	checksums := "1111111111111111111111111111111111111111111111111111111111111111  bfc_other.tar.gz\n"
	if !r.NoChecksumLine {
		checksums += hexSum + "  " + name + "\n"
	}
	s.assets[r.Tag+"/checksums.txt"] = []byte(checksums)
	if !r.NoArchive {
		s.assets[r.Tag+"/"+name] = archive
	}
}

// Archive packs binary as GoReleaser does: bfc in a .tar.gz, bfc.exe in
// a .zip for Windows, next to LICENSE and README.md.
func Archive(t testing.TB, goos string, binary []byte) []byte {
	t.Helper()
	files := []struct {
		name string
		data []byte
		mode int64
	}{{"LICENSE", []byte("Apache-2.0\n"), 0o644}, {"bfc", binary, 0o755}, {"README.md", []byte("# bfc\n"), 0o644}}
	var buf bytes.Buffer
	if goos == "windows" {
		zw := zip.NewWriter(&buf)
		for _, f := range files {
			name := f.name
			if name == "bfc" {
				name = "bfc.exe"
			}
			w, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(f.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Requests returns the paths requested so far, in order.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// Downloaded reports whether an archive (not checksums.txt) was requested.
func (s *Server) Downloaded() bool {
	for _, p := range s.Requests() {
		if strings.HasPrefix(p, "/releases/download/") && !strings.HasSuffix(p, "/checksums.txt") {
			return true
		}
	}
	return false
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.Path)
	s.mu.Unlock()
	switch {
	case r.URL.Path == "/releases/latest":
		if s.latest == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, s.URL+"/releases/tag/"+s.latest, http.StatusFound)
	case strings.HasPrefix(r.URL.Path, "/releases/download/"):
		data, ok := s.assets[strings.TrimPrefix(r.URL.Path, "/releases/download/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	default:
		s.t.Errorf("bfc update requested %s, want only /releases/latest and /releases/download/…", r.URL.Path)
		http.NotFound(w, r)
	}
}
