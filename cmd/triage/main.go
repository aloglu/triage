// Command triage is a fast keyboard client for GitHub issues.
package main

import (
	"os"

	"github.com/aloglu/triage/internal/cli"
	"github.com/aloglu/triage/internal/tui"
)

// Set at build time with -ldflags "-X main.version=... -X main.channel=...".
// Release builds set channel to "release"; `go install` leaves it empty.
var (
	version = "dev"
	channel = ""
)

func main() {
	os.Exit(cli.Main(os.Args[1:], version, channel, tui.Run))
}
