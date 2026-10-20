package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/auth"
	"github.com/ardzz/toe3-stormline-cloud/internal/config"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
)

const userUsage = "usage: toe3-cloud user add|passwd|disable|enable|delete <name>"

// runUser executes one operator account command against the configured database.
func runUser(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 2 {
		return errors.New(userUsage)
	}
	cmd, name := args[0], args[1]
	switch cmd {
	case "add", "passwd", "disable", "enable", "delete":
	default:
		return fmt.Errorf("unknown user command %q; %s", cmd, userUsage)
	}
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	// Read the password before connecting so no database connection is held while a human types.
	var pw string
	if cmd == "add" || cmd == "passwd" {
		if pw, err = readPassword(ctx, stdin, stdout); err != nil {
			return err
		}
	}
	if err := postgres.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	op := account.NewOperator(postgres.NewUsers(pool), auth.NewHasher(1))

	switch cmd {
	case "add":
		id, err := op.Add(ctx, name, pw)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "created user %d\n", id)
	case "passwd":
		if err := op.SetPassword(ctx, name, pw); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "password updated; sessions revoked")
	case "disable":
		if err := op.SetDisabled(ctx, name, true); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "user disabled; sessions revoked")
	case "enable":
		if err := op.SetDisabled(ctx, name, false); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "user enabled")
	case "delete":
		if err := op.Delete(ctx, name); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "user deleted")
	}
	return nil
}
