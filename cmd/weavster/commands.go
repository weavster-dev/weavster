package main

import (
	"context"
	"errors"
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
		return deployedStatus(ctx, client, stdout, stderr, debug)
	case "flow":
		return flowCommand(ctx, client, fields[1:], stdout, stderr, debug)
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

// shellError prints err; in debug mode it adds each wrapped cause with its
// type. It returns exit code 2.
func shellError(stderr io.Writer, debug bool, err error) int {
	_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
	if debug {
		for cause := errors.Unwrap(err); cause != nil; cause = errors.Unwrap(cause) {
			_, _ = fmt.Fprintf(stderr, "  caused by %T: %v\n", cause, cause)
		}
	}
	return 2
}

func printShellHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "commands: help, status, version, flow <subcommand> (flow help), user list, quit")
}
