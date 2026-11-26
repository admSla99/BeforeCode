package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/update/updatetest"
)

// bfcUpdate runs bfc built as version, with exe as the binary that bfc
// update replaces, and the test's variables.
func bfcUpdate(t *testing.T, version, exe string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd(version)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"update"}, args...))
	err := cmd.ExecuteContext(withPlace(context.Background(), place{dir: t.TempDir(), env: envOf(t), executable: exe}))
	return out.String(), err
}

// installed returns a fake installed bfc holding content.
func installed(t *testing.T, content string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "bfc")
	if err := os.WriteFile(exe, []byte(content), 0o755); err != nil { //nolint:gosec // a fake binary
		t.Fatal(err)
	}
	return exe
}

func wantFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a test file
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("%s holds %q, want %q", path, data, want)
	}
}

func wantExit(t *testing.T, err error, code int, out string) {
	t.Helper()
	got := 0
	if err != nil {
		got = 1
		var exit *exitCodeError
		if errors.As(err, &exit) {
			got = exit.code
		}
	}
	if got != code {
		t.Errorf("exit code %d (err %v), want %d\n%s", got, err, code, out)
	}
}

// releases serves v0.1.5, v0.2.0 (latest) and the pre-release
// v0.3.0-rc.1, and points BFC_RELEASES_URL at it.
func releases(t *testing.T, latest string) *updatetest.Server {
	t.Helper()
	srv := updatetest.New(t, latest,
		updatetest.Release{Tag: "v0.1.5", Binary: []byte("bfc v0.1.5")},
		updatetest.Release{Tag: "v0.2.0", Binary: []byte("bfc v0.2.0")},
		updatetest.Release{Tag: "v0.3.0-rc.1", Binary: []byte("bfc v0.3.0-rc.1")},
		updatetest.Release{Tag: "v0.4.0", Binary: []byte("bfc v0.4.0"), BadChecksum: true},
		updatetest.Release{Tag: "v0.5.0", Binary: []byte("bfc v0.5.0"), NoChecksumLine: true},
	)
	setEnv(t, "BFC_RELEASES_URL", srv.URL)
	return srv
}

// AC-3: the latest release replaces the binary.
func TestUpdateInstallsLatest(t *testing.T) {
	t.Parallel()
	releases(t, "v0.2.0")
	exe := installed(t, "bfc v0.1.0")
	out, err := bfcUpdate(t, "v0.1.0", exe)
	if err != nil {
		t.Fatalf("bfc update: %v\n%s", err, out)
	}
	if !strings.Contains(out, "updated bfc v0.1.0 -> v0.2.0") {
		t.Errorf("output lacks the update line:\n%s", out)
	}
	wantFile(t, exe, "bfc v0.2.0")
}

func TestUpdateJSON(t *testing.T) {
	t.Parallel()
	releases(t, "v0.2.0")
	exe := installed(t, "bfc v0.1.0")
	out, err := bfcUpdate(t, "v0.1.0", exe, "--json")
	if err != nil {
		t.Fatalf("bfc update --json: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	want := map[string]any{"current": "v0.1.0", "latest": "v0.2.0", "newer": true, "installed": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v\n%s", k, got[k], v, out)
		}
	}
}

// AC-4: the latest release is installed already.
func TestUpdateAlreadyInstalled(t *testing.T) {
	t.Parallel()
	srv := releases(t, "v0.2.0")
	exe := installed(t, "bfc v0.2.0 (local)")
	out, err := bfcUpdate(t, "v0.2.0", exe)
	if err != nil {
		t.Fatalf("bfc update: %v\n%s", err, out)
	}
	if !strings.Contains(out, "bfc v0.2.0 is already installed") {
		t.Errorf("output does not say it is installed:\n%s", out)
	}
	if srv.Downloaded() {
		t.Errorf("an archive was downloaded: %v", srv.Requests())
	}
	wantFile(t, exe, "bfc v0.2.0 (local)")
}

// AC-5: --check exits 0, 1 or 2 and installs nothing.
func TestUpdateCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, current, latest string
		code                  int
		want                  []string
	}{
		{"latest", "v0.2.0", "v0.2.0", 0, []string{"v0.2.0", "latest"}},
		{"newer", "v0.1.0", "v0.2.0", 1, []string{"v0.1.0", "v0.2.0", "bfc update"}},
		{"lookup fails", "v0.1.0", "", 2, []string{"latest release"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := releases(t, tc.latest)
			exe := installed(t, "old")
			out, err := bfcUpdate(t, tc.current, exe, "--check")
			wantExit(t, err, tc.code, out)
			text := out
			if err != nil {
				text += err.Error()
			}
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Errorf("output lacks %q:\n%s", w, text)
				}
			}
			if srv.Downloaded() {
				t.Errorf("--check downloaded an archive: %v", srv.Requests())
			}
			wantFile(t, exe, "old")
		})
	}
}

func TestUpdateCheckJSON(t *testing.T) {
	t.Parallel()
	releases(t, "v0.2.0")
	out, err := bfcUpdate(t, "v0.1.0", installed(t, "old"), "--check", "--json")
	wantExit(t, err, 1, out)
	var got struct {
		Current string `json:"current"`
		Latest  string `json:"latest"`
		Newer   bool   `json:"newer"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got.Current != "v0.1.0" || got.Latest != "v0.2.0" || !got.Newer {
		t.Errorf("got %+v", got)
	}
}

// AC-6: a named older release is installed and the output says so.
func TestUpdateRollsBackToNamedVersion(t *testing.T) {
	t.Parallel()
	releases(t, "v0.2.0")
	for _, arg := range []string{"v0.1.5", "0.1.5"} {
		exe := installed(t, "bfc v0.2.0")
		out, err := bfcUpdate(t, "v0.2.0", exe, arg)
		if err != nil {
			t.Fatalf("bfc update %s: %v\n%s", arg, err, out)
		}
		if !strings.Contains(out, "updated bfc v0.2.0 -> v0.1.5") || !strings.Contains(out, "older") {
			t.Errorf("bfc update %s: output lacks the roll back:\n%s", arg, out)
		}
		wantFile(t, exe, "bfc v0.1.5")
	}
}

// AC-6: a version without a release fails naming it.
func TestUpdateUnknownVersion(t *testing.T) {
	t.Parallel()
	releases(t, "v0.2.0")
	exe := installed(t, "bfc v0.2.0")
	out, err := bfcUpdate(t, "v0.2.0", exe, "v0.9.9")
	if err == nil || !strings.Contains(err.Error(), "v0.9.9") {
		t.Errorf("err = %v, want an error naming v0.9.9\n%s", err, out)
	}
	wantFile(t, exe, "bfc v0.2.0")
}

func TestUpdateRejectsMalformedVersion(t *testing.T) {
	t.Parallel()
	srv := releases(t, "v0.2.0")
	_, err := bfcUpdate(t, "v0.2.0", installed(t, "x"), "latest-please")
	if err == nil || !strings.Contains(err.Error(), "latest-please") {
		t.Errorf("err = %v, want an error naming the version", err)
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("requests for a malformed version: %v", srv.Requests())
	}
}

// AC-7: a mismatch or a missing checksum line leaves the binary unchanged.
func TestUpdateChecksumFailures(t *testing.T) {
	t.Parallel()
	releases(t, "v0.2.0")
	for tag, want := range map[string]string{"v0.4.0": "expected", "v0.5.0": "checksums.txt"} {
		exe := installed(t, "bfc v0.2.0")
		out, err := bfcUpdate(t, "v0.2.0", exe, tag)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("bfc update %s: err = %v, want one with %q\n%s", tag, err, want, out)
		}
		wantFile(t, exe, "bfc v0.2.0")
		entries, _ := os.ReadDir(filepath.Dir(exe))
		if len(entries) != 1 {
			t.Errorf("bfc update %s left files next to the binary: %v", tag, entries)
		}
	}
}

// AC-14: a named pre-release is installed.
func TestUpdateInstallsNamedPreRelease(t *testing.T) {
	t.Parallel()
	releases(t, "v0.2.0")
	exe := installed(t, "bfc v0.2.0")
	out, err := bfcUpdate(t, "v0.2.0", exe, "v0.3.0-rc.1")
	if err != nil {
		t.Fatalf("bfc update v0.3.0-rc.1: %v\n%s", err, out)
	}
	wantFile(t, exe, "bfc v0.3.0-rc.1")
}

// AC-9: a dev build is not replaced and gets the go install hint.
func TestUpdateRefusesDevBuild(t *testing.T) {
	t.Parallel()
	srv := releases(t, "v0.2.0")
	const hint = "go install github.com/admSla99/BeforeCode/cmd/bfc@latest"
	for args, code := range map[string]int{"": 1, "--check": 2} {
		exe := installed(t, "bfc dev")
		var a []string
		if args != "" {
			a = []string{args}
		}
		out, err := bfcUpdate(t, "dev", exe, a...)
		wantExit(t, err, code, out)
		if err == nil || !strings.Contains(out+err.Error(), hint) {
			t.Errorf("bfc update %s: no go install hint:\n%s", args, out)
		}
		wantFile(t, exe, "bfc dev")
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("a dev build made requests: %v", srv.Requests())
	}
}

// AC-10: inside an agent installing refuses before any request; --check
// works.
func TestUpdateInsideAgent(t *testing.T) {
	t.Parallel()
	srv := releases(t, "v0.2.0")
	setEnv(t, agentEnv, "1")
	exe := installed(t, "bfc v0.1.0")
	out, err := bfcUpdate(t, "v0.1.0", exe)
	if err == nil {
		t.Fatalf("bfc update ran inside an agent:\n%s", out)
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("requests before refusing: %v", srv.Requests())
	}
	wantFile(t, exe, "bfc v0.1.0")

	out, err = bfcUpdate(t, "v0.1.0", exe, "--check")
	wantExit(t, err, 1, out)
	if !strings.Contains(out, "v0.2.0") {
		t.Errorf("--check inside an agent lacks the latest version:\n%s", out)
	}
}

// AC-11: the code never names the GitHub API host.
func TestNoGitHubAPI(t *testing.T) {
	t.Parallel()
	host := "api." + "github.com"
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != filepath.Join("..", "..") {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // the repository's own files
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(host)) {
			t.Errorf("%s names %s", path, host)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
