package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEditorScriptRunsTheSettingOnTheFileLikeGit(t *testing.T) {
	if got := editorScript(`"C:/Program Files/Microsoft VS Code/bin/code" --wait`); got != `"C:/Program Files/Microsoft VS Code/bin/code" --wait "$@"`+"\n" {
		t.Errorf("editorScript = %q", got)
	}
}

func TestEditorScriptHandlesWaitFlagsAndPathsWithSpaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runs /bin/sh directly; on Windows the editor tests cover sh.exe")
	}
	dir := filepath.Join(t.TempDir(), "my editor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An "editor" that records its arguments, set up like "code --wait".
	editor := filepath.Join(dir, "code")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf '%s|' \"$@\" > \"$(dirname \"$0\")/args\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte(editorScript(`"`+editor+`" --wait`)), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "the plan.md")

	name, args, err := editorCommand(runtime.GOOS, "/usr/bin/git", script, file, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %q: %v\n%s", name, args, err, out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "--wait|"+file+"|" {
		t.Errorf("editor got arguments %q", got)
	}
}

func TestEditorCommandPassesOnlyPaths(t *testing.T) {
	name, args, err := editorCommand("linux", "/usr/bin/git", "/tmp/bfc-plan 1.md.sh", "/tmp/bfc-plan 1.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/bfc-plan 1.md.sh", "/tmp/bfc-plan 1.md"}
	if name != "sh" || strings.Join(args, "|") != strings.Join(want, "|") {
		t.Errorf("command = %s %q, want sh %q", name, args, want)
	}
}

func TestEditorCommandOnWindowsUsesTheShellOfGitForWindows(t *testing.T) {
	script := `C:\Users\Jana Nováková\AppData\Local\Temp\bfc-plan-1.md.sh`
	path := `C:\Users\Jana Nováková\AppData\Local\Temp\bfc-plan-1.md`
	for gitExe, sh := range map[string]string{
		`C:\Program Files\Git\cmd\git.exe`:         `C:\Program Files\Git\bin\sh.exe`,
		`C:\Program Files\Git\mingw64\bin\git.exe`: `C:\Program Files\Git\bin\sh.exe`,
		`D:\tools\PortableGit\cmd\git.exe`:         `D:\tools\PortableGit\usr\bin\sh.exe`,
	} {
		exists := func(p string) bool { return p == sh }

		name, args, err := editorCommand("windows", gitExe, script, path, exists)
		if err != nil {
			t.Errorf("git at %s: %v", gitExe, err)
			continue
		}
		if name != sh || strings.Join(args, "|") != script+"|"+path {
			t.Errorf("git at %s: command = %s %q, want %s [script path]", gitExe, name, args, sh)
		}
	}
}

func TestEditorCommandOnWindowsWithoutShSaysWhatIsMissing(t *testing.T) {
	_, _, err := editorCommand("windows", `C:\git\cmd\git.exe`, `C:\tmp\x.md.sh`, `C:\tmp\x.md`, func(string) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "sh.exe") {
		t.Errorf("error = %v, want one naming sh.exe", err)
	}
}
