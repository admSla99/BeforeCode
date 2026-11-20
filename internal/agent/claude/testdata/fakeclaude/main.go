// Command fakeclaude stands in for Claude Code in the tests of the claude
// package. It writes its arguments and working directory to
// $FAKECLAUDE_ARGS, prints the stream-json fixture $FAKECLAUDE_FIXTURE line
// by line, writes $FAKECLAUDE_STDERR to stderr, then sleeps forever when
// $FAKECLAUDE_HANG is set, or exits with $FAKECLAUDE_EXIT. When it is
// started with --resume and $FAKECLAUDE_RESUME_FIXTURE is set, it uses the
// $FAKECLAUDE_RESUME_* variables instead, so that a resumed turn can fail
// while a new one succeeds.
package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

func main() {
	if slices.Contains(os.Args[1:], "--resume") && os.Getenv("FAKECLAUDE_RESUME_FIXTURE") != "" {
		for _, v := range []string{"FIXTURE", "STDERR", "EXIT"} {
			_ = os.Setenv("FAKECLAUDE_"+v, os.Getenv("FAKECLAUDE_RESUME_"+v))
		}
	}
	if path := os.Getenv("FAKECLAUDE_ARGS"); path != "" {
		wd, _ := os.Getwd()
		record := "dir=" + wd + "\n" + strings.Join(os.Args[1:], "\n") + "\n"
		if err := os.WriteFile(path, []byte(record), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if path := os.Getenv("FAKECLAUDE_FIXTURE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Stdout.Write(data)
	}
	fmt.Fprint(os.Stderr, os.Getenv("FAKECLAUDE_STDERR"))
	if os.Getenv("FAKECLAUDE_HANG") != "" {
		time.Sleep(time.Hour)
	}
	code, _ := strconv.Atoi(os.Getenv("FAKECLAUDE_EXIT"))
	os.Exit(code)
}
