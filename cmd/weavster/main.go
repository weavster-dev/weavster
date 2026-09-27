// Command weavster is the single static binary that hosts the Weavster
// message-oriented integration platform: API gateway, scheduler, executor,
// state manager, adapters, and CLI shell (composition root).
package main

import (
	"context"
	"errors"
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

// parseFlags parses args into fs. On failure it reports false and the exit
// code: 0 for -help/--help (the caller prints usage), otherwise 2 after
// printing "Error: <problem>" and the flag list (#107 D-45).
func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer) (int, bool) {
	out := fs.Output()
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)
	fs.SetOutput(out)
	switch {
	case err == nil:
		return 0, true
	case errors.Is(err, flag.ErrHelp):
		return 0, false
	}
	_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
	fs.Usage()
	return 2, false
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
		addr     = fs.String("a", "", "server address to connect to (default http://127.0.0.1:8080)")
		user     = fs.String("u", "", "login username")
		password = fs.String("p", "", "login password")
		script   = fs.String("s", "", "script file (batch mode)")
		ver      = fs.Bool("v", false, "print the server's version")
		config   = fs.String("c", "", "connection file (YAML: address, user, password)")
		help     = fs.Bool("h", false, "print usage and exit")
		debug    = fs.Bool("d", false, "debug mode (print the cause chain of errors)")
	)
	if code, ok := parseFlags(fs, args, stderr); !ok {
		if code == 0 {
			printUsage(stdout)
		}
		return code
	}
	if *help {
		printUsage(stdout)
		return 0
	}
	conn := connection{Address: *addr, User: *user, Password: *password}
	if *config != "" {
		file, err := loadConnection(*config)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 2
		}
		conn = file.override(conn)
	}
	if conn.User == "" && conn.Password != "" {
		_, _ = fmt.Fprintln(stderr, "Error: a password needs a user (-u, or user: in the connection file)")
		return 2
	}
	client := newHTTPClient(conn.Address, conn.User, conn.Password)
	ctx := context.Background()
	if conn.User != "" {
		if err := client.login(ctx); err != nil {
			// Spec §3.3: report and carry on (the prompt, or the script).
			_, _ = fmt.Fprintln(stderr, "Could not log in to server.")
			_ = shellError(stderr, *debug, err)
		}
	}
	if *ver {
		v, err := serverVersion(ctx, client)
		if err != nil {
			return shellError(stderr, *debug, err)
		}
		_, _ = fmt.Fprintf(stdout, "weavster server %s (client %s)\n", v, version)
		return 0
	}
	if *script != "" {
		data, err := os.ReadFile(*script)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
			return 2
		}
		return runScript(data, client, stdout, stderr, *debug)
	}
	return runShell(stdin, client, stdout, stderr, *debug)
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintf(w, `Usage: weavster [flags]            interactive shell (or batch mode with -s)
       weavster server [--config FILE] [address]
       weavster test [--filter NAME] [--format junit|json] [--output DIR]

Flags:
  -a address   Server address to connect to (default http://127.0.0.1:8080)
  -u user      Login username
  -p password  Login password
  -s script    Script file (batch mode)
  -v           Print the server's version
  -c file      Connection file (YAML: address, user, password); flags override it
  -h           Print usage and exit
  -d           Debug mode (print the cause chain of errors)
`)
}
