package update

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeBinary(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func wantContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("%s holds %q, want %q", path, data, want)
	}
}

// wantOnly checks that dir holds exactly the named files.
func wantOnly(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	for _, n := range names {
		if !got[n] {
			t.Errorf("%s lacks %s", dir, n)
		}
		delete(got, n)
	}
	for n := range got {
		t.Errorf("%s holds the leftover %s", dir, n)
	}
}

func TestReplaceSwapsBinaryAndKeepsMode(t *testing.T) {
	t.Parallel()
	dir, src := t.TempDir(), t.TempDir()
	target := filepath.Join(dir, "bfc")
	writeBinary(t, target, "old bfc", 0o750)
	newBin := filepath.Join(src, "bfc")
	writeBinary(t, newBin, "new bfc", 0o600)

	if err := Replace(target, newBin); err != nil {
		t.Fatal(err)
	}
	wantContent(t, target, "new bfc")
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o750 {
			t.Errorf("mode %v, want the old binary's 0750", info.Mode().Perm())
		}
	}
	wantOnly(t, dir, "bfc")
}

func TestReplaceRemovesLeftoverOld(t *testing.T) {
	t.Parallel()
	dir, src := t.TempDir(), t.TempDir()
	target := filepath.Join(dir, "bfc.exe")
	writeBinary(t, target, "old bfc", 0o755)
	writeBinary(t, target+".old", "older bfc", 0o755)
	newBin := filepath.Join(src, "bfc.exe")
	writeBinary(t, newBin, "new bfc", 0o755)

	if err := Replace(target, newBin); err != nil {
		t.Fatal(err)
	}
	wantContent(t, target, "new bfc")
	wantOnly(t, dir, "bfc.exe")
}

// AC-8: when the final rename fails, the old binary is back at its path.
func TestReplaceRestoresOldBinaryWhenFinalRenameFails(t *testing.T) {
	t.Parallel()
	dir, src := t.TempDir(), t.TempDir()
	target := filepath.Join(dir, "bfc")
	writeBinary(t, target, "old bfc", 0o755)
	newBin := filepath.Join(src, "bfc")
	writeBinary(t, newBin, "new bfc", 0o755)

	failing := func(from, to string) error {
		if to == target && from != target+".old" { // the new binary, not the restore
			return errors.New("rename refused")
		}
		return os.Rename(from, to)
	}
	if err := replace(target, newBin, failing); err == nil {
		t.Fatal("replace succeeded although the final rename failed")
	}
	wantContent(t, target, "old bfc")
	wantOnly(t, dir, "bfc")
}

func TestReplaceLeavesTargetWhenMovingItAsideFails(t *testing.T) {
	t.Parallel()
	dir, src := t.TempDir(), t.TempDir()
	target := filepath.Join(dir, "bfc")
	writeBinary(t, target, "old bfc", 0o755)
	newBin := filepath.Join(src, "bfc")
	writeBinary(t, newBin, "new bfc", 0o755)

	failing := func(from, to string) error {
		if from == target {
			return errors.New("rename refused")
		}
		return os.Rename(from, to)
	}
	if err := replace(target, newBin, failing); err == nil {
		t.Fatal("replace succeeded although moving the target aside failed")
	}
	wantContent(t, target, "old bfc")
	wantOnly(t, dir, "bfc")
}

func TestReplaceMissingTarget(t *testing.T) {
	t.Parallel()
	dir, src := t.TempDir(), t.TempDir()
	newBin := filepath.Join(src, "bfc")
	writeBinary(t, newBin, "new bfc", 0o755)
	if err := Replace(filepath.Join(dir, "bfc"), newBin); err == nil {
		t.Fatal("Replace of a missing target succeeded")
	}
	wantOnly(t, dir)
}
