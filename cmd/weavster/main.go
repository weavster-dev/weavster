// Command weavster is the single static binary that hosts the Weavster
// message-oriented integration platform: API gateway, scheduler, executor,
// state manager, adapters, and CLI shell (composition root).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

var (
	version   = "0.1.0"
	buildDate = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run is the composition-root entrypoint, separated from main for testability.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "test":
			return runTest(args[1:], stdout, stderr)
		case "server":
			return runServer(args[1:], stderr)
		}
	}

	fs := flag.NewFlagSet("weavster", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		addr     = fs.String("a", "", "server address to connect to")
		user     = fs.String("u", "", "login username")
		password = fs.String("p", "", "login password")
		script   = fs.String("s", "", "script file (batch mode)")
		ver      = fs.Bool("v", false, "print server version")
		config   = fs.String("c", "", "path to default connection/config file")
		help     = fs.Bool("h", false, "print usage and exit")
		debug    = fs.Bool("d", false, "debug mode (print stack traces on error)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *help {
		printUsage(stdout)
		return 0
	}
	if *ver {
		_, _ = fmt.Fprintf(stdout, "weavster %s (built %s)\n", version, buildDate)
		return 0
	}
	if *script != "" {
		data, err := os.ReadFile(*script)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 2
		}
		client := newHTTPClient(*addr, *user, *password)
		return runScript(data, client, stdout, stderr, *debug)
	}
	_ = config
	return runServer(nil, stderr)
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintf(w, `Usage: weavster [flags] | weavster test [--filter NAME] [--format junit|json] [--output DIR]

Flags:
  -a address   Server address to connect to
  -u user      Login username
  -p password  Login password
  -s script    Script file (batch mode)
  -v           Print server version
  -c config    Path to default connection/config file
  -h           Print usage and exit
  -d           Debug mode (print stack traces on error)
`)
}
