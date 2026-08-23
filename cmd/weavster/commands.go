package main

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// dispatch runs a single shell command and returns its exit code (§3.2/§3.3).
func dispatch(ctx context.Context, client Client, line string, stdout, stderr io.Writer, debug bool) int {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0
	}
	switch fields[0] {
	case "help":
		printShellHelp(stdout)
		return 0
	case "quit", "exit":
		return 0
	case "version":
		_, _ = fmt.Fprintln(stdout, client.Version(ctx))
		return 0
	case "status":
		out, err := client.Status(ctx)
		if err != nil {
			return shellError(stderr, debug, err)
		}
		_, _ = fmt.Fprintln(stdout, out)
		return 0
	case "flow":
		if len(fields) >= 2 && fields[1] == "list" {
			flows, err := client.FlowList(ctx)
			if err != nil {
				return shellError(stderr, debug, err)
			}
			for _, f := range flows {
				_, _ = fmt.Fprintln(stdout, f)
			}
			return 0
		}
		_, _ = fmt.Fprintln(stderr, "Error: unknown flow subcommand")
		return 2
	case "user":
		if len(fields) >= 2 && fields[1] == "list" {
			users, err := client.UserList(ctx)
			if err != nil {
				return shellError(stderr, debug, err)
			}
			for _, u := range users {
				_, _ = fmt.Fprintln(stdout, u)
			}
			return 0
		}
		_, _ = fmt.Fprintln(stderr, "Error: unknown user subcommand")
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "Error: unknown command %q\n", fields[0])
		return 2
	}
}

func shellError(stderr io.Writer, debug bool, err error) int {
	if debug {
		_, _ = fmt.Fprintf(stderr, "Error: %+v\n", err)
	} else {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
	}
	return 2
}

func printShellHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "commands: help, status, version, flow list, user list, quit")
}
