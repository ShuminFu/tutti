// Command claude-sdk-sidecar serves the Claude SDK sidecar protocol over
// stdio. It is the Go replacement for the TypeScript
// @tutti-os/claude-sdk-sidecar entry point (src/main.ts): newline-delimited
// JSON requests on stdin, versioned events on stdout.
package main

import (
	"os"

	"github.com/tutti-os/tutti/packages/agent/daemon/claudesidecar"
)

func main() {
	server := claudesidecar.NewServer(os.Stdout)
	if err := server.Serve(os.Stdin); err != nil {
		os.Exit(1)
	}
}
