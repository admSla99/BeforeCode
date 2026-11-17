package main

import (
	"strings"
	"testing"
)

func TestEditorCommandRunsTheEditorThroughTheShellLikeGit(t *testing.T) {
	name, args, err := editorCommand("linux", "/usr/bin/git", "code --wait", "/tmp/bfc-plan 1.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-c", `code --wait "$@"`, "code --wait", "/tmp/bfc-plan 1.md"}
	if name != "sh" || strings.Join(args, "|") != strings.Join(want, "|") {
		t.Errorf("command = %s %q, want sh %q", name, args, want)
	}
}

func TestEditorCommandOnWindowsUsesTheShellOfGitForWindows(t *testing.T) {
	editor := `"C:\Program Files\Microsoft VS Code\bin\code.cmd" --wait`
	path := `C:\Users\Jana Nováková\AppData\Local\Temp\bfc-plan-1.md`
	for gitExe, sh := range map[string]string{
		`C:\Program Files\Git\cmd\git.exe`:         `C:\Program Files\Git\bin\sh.exe`,
		`C:\Program Files\Git\mingw64\bin\git.exe`: `C:\Program Files\Git\bin\sh.exe`,
		`D:\tools\PortableGit\cmd\git.exe`:         `D:\tools\PortableGit\usr\bin\sh.exe`,
	} {
		exists := func(p string) bool { return p == sh }

		name, args, err := editorCommand("windows", gitExe, editor, path, exists)
		if err != nil {
			t.Errorf("git at %s: %v", gitExe, err)
			continue
		}
		want := []string{"-c", editor + ` "$@"`, editor, path}
		if name != sh || strings.Join(args, "|") != strings.Join(want, "|") {
			t.Errorf("git at %s: command = %s %q, want %s %q", gitExe, name, args, sh, want)
		}
	}
}

func TestEditorCommandOnWindowsWithoutShSaysWhatIsMissing(t *testing.T) {
	_, _, err := editorCommand("windows", `C:\git\cmd\git.exe`, "notepad", `C:\tmp\x.md`, func(string) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "sh.exe") {
		t.Errorf("error = %v, want one naming sh.exe", err)
	}
}
