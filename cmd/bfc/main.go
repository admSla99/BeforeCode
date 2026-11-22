// Command bfc manages plans for AI coding agents. See docs/design.md.
package main

import (
	"errors"
	"os"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

// exitCodeError ends bfc with its own exit code instead of 1, such as 2
// for a wait that timed out.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

func main() {
	if err := newRootCmd(version).Execute(); err != nil {
		var exit *exitCodeError
		if errors.As(err, &exit) {
			os.Exit(exit.code)
		}
		os.Exit(1)
	}
}
