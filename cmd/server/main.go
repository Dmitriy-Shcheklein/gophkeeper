// Package main is a temporary placeholder for the GophKeeper server.
// It will be replaced by the real server implementation in a later stage.
package main

import "fmt"

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// buildDate is set at build time via -ldflags "-X main.buildDate=...".
var buildDate = "unknown"

func main() {
	fmt.Printf("gophkeeper-server version=%s buildDate=%s\n", version, buildDate)
}
