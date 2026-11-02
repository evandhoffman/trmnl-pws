// Command trmnl-poller serves the TRMNL plugin data.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time: -ldflags "-X main.version=<sha>".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run executes one command and returns the process exit code.
// With no arguments it behaves as serve.
func run(args []string, stdout io.Writer) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "--version":
		fmt.Fprintf(stdout, "trmnl-poller %s\n", version)
		return 0
	case "serve", "healthcheck":
		// A later task fills these in.
		fmt.Fprintf(stdout, "%s: not implemented\n", cmd)
		return 2
	default:
		fmt.Fprintf(stdout, "unknown command: %s\n", cmd)
		return 2
	}
}
