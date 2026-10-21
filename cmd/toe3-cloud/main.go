// Command toe3-cloud is the self-hosted cloud-save server for the TOE3 Stormline mod.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

const usage = `usage: toe3-cloud <command> [args]

commands:
  version                print the build version
  serve                  serve the HTTP API on TOE3_LISTEN (applies migrations first)
  migrate                apply database migrations
  user add <name>        create an account (password from the prompt or the first stdin line)
  user passwd <name>     set a new password and revoke the account's sessions
  user disable <name>    disable an account and revoke its sessions
  user enable <name>     re-enable a disabled account
  user delete <name>     delete an account
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes one subcommand and returns the process exit code: 0 ok, 1 failed, 2 usage error.
func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "serve":
		return exitCode(stderr, runServe(ctx, getenv, stderr))
	case "migrate":
		return exitCode(stderr, runMigrate(ctx, getenv))
	case "user":
		return exitCode(stderr, runUser(ctx, args[1:], getenv, stdin, stdout))
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// exitCode prints err to stderr and maps it to the process exit code.
func exitCode(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}
