// Command pulse-cli uploads JavaScript source maps to Pulse.
//
//	pulse-cli sourcemaps inject dist/
//	pulse-cli sourcemaps upload --release web@1.2.3 --url-prefix dist/ dist/
//
// inject gives every script with a source map a debug id, so errors find
// their map whatever URL the script is served from. upload sends the maps of
// a release; the server, project and token come from flags or the
// PULSE_URL, PULSE_PROJECT and PULSE_UPLOAD_TOKEN environment variables.
package main

import (
	"fmt"
	"os"
)

const usage = `usage:
  pulse-cli sourcemaps inject <dir>
  pulse-cli sourcemaps upload --release <version> [--url-prefix ~/assets/] [--delete] <dir>

upload flags (or environment):
  --url      Pulse API base URL (PULSE_URL, default http://localhost:8081)
  --project  project id (PULSE_PROJECT)
  --token    upload token from project settings (PULSE_UPLOAD_TOKEN)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pulse-cli:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 || args[0] != "sourcemaps" {
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command")
	}
	switch args[1] {
	case "inject":
		return injectCmd(args[2:])
	case "upload":
		return uploadCmd(args[2:])
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command sourcemaps %s", args[1])
}
