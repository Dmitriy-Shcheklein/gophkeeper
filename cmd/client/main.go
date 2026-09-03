// Package main is the GophKeeper client entry point: it wires the
// build metadata, the lazily built service stack and the cobra
// command tree together and maps failures to the exit code
// (0 success, 1 error).
package main

import (
	"os"

	"github.com/dmitriy/gophkeeper/internal/client/cli"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// buildDate is set at build time via -ldflags "-X main.buildDate=...".
var buildDate = "unknown"

// commit is set at build time via -ldflags "-X main.commit=..."; it
// is not wired by the Makefile yet and prints as "none" until then.
var commit = "none"

func main() {
	app := cli.NewApp(version, buildDate, commit)
	if err := cli.Execute(app, os.Args[1:]); err != nil {
		os.Exit(1)
	}
}
