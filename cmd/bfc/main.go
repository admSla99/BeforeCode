// Command bfc manages plans for AI coding agents. See docs/design.md.
package main

import "os"

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	if err := newRootCmd(version).Execute(); err != nil {
		os.Exit(1)
	}
}
