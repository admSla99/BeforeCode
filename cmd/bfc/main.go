// Command bfc manages plans for AI coding agents. See docs/design.md.
package main

import (
	"errors"
	"os"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	if err := newRootCmd(version).Execute(); err != nil {
		var exit *exitCodeError
		if errors.As(err, &exit) {
			os.Exit(exit.code)
		}
		os.Exit(1)
	}
}
