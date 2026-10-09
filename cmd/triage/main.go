// Command triage is a fast keyboard client for GitHub issues.
package main

import (
	"os"

	"github.com/aloglu/triage/internal/cli"
	"github.com/aloglu/triage/internal/tui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Main(os.Args[1:], version, tui.Run))
}
