package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// adminTimeout bounds a single non-interactive admin operation so a wedged
// server fails the command instead of hanging a script.
const adminTimeout = 30 * time.Second

// dbAdmin is the slice of the engine the create/drop commands use. Taking a
// narrow interface (not the whole db.Engine) keeps the command logic testable
// with a small fake; db.Engine satisfies it.
type dbAdmin interface {
	Capabilities() db.Capabilities
	CreateDatabase(ctx context.Context, name string, opts db.CreateOpts) error
	CreateUser(ctx context.Context, name, password string) error
	Grant(ctx context.Context, user, database string, level db.GrantLevel) error
	DropDatabase(ctx context.Context, name string) error
}

// NewCreateCommand builds `dbwiz create <db> [--user]`: the non-interactive
// counterpart to the dashboard's create-database form, with the same one-shot
// "also make a matching user and grant it everything" shortcut. It acts on the
// target resolved from context (see resolveTarget); pass --target to be explicit.
func NewCreateCommand() *cobra.Command {
	var (
		targetFlag, password, userPassword string
		withUser                           bool
	)
	cmd := &cobra.Command{
		Use:   "create <database>",
		Short: "Create a database (optionally with a matching user)",
		Long: "Create a database on the target resolved from context (the container\n" +
			"pinned by `dbwiz use`, an explicit --target, or the sole detected one).\n\n" +
			"With --user, DBWiz also creates a login user named like the database, makes\n" +
			"it the owner, and grants it ALL on the new database — the same one-shot flow\n" +
			"the dashboard offers. Use --user-password to set that user's password.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), adminTimeout)
			defer cancel()
			t, err := resolveTarget(ctx, targetFlag)
			if err != nil {
				return err
			}
			engine, err := open(ctx, t, password)
			if err != nil {
				return err
			}
			defer engine.Close()
			return runCreate(ctx, cmd.OutOrStdout(), engine, args[0], withUser, userPassword)
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().BoolVar(&withUser, "user", false, "also create a matching login user, owner, granted ALL")
	cmd.Flags().StringVar(&userPassword, "user-password", "", "password for the user created by --user")
	return cmd
}

// runCreate creates the database and, with withUser, a matching user granted ALL
// — the create-and-continue flow, mirroring screens.createDatabaseCmd. The user
// is created first so CREATE DATABASE … OWNER can hand it ownership. It stops at
// the first failure so a partial result reads clearly.
func runCreate(ctx context.Context, out io.Writer, e dbAdmin, name string, withUser bool, userPassword string) error {
	if err := db.ValidateIdent(name); err != nil {
		return err
	}
	if withUser {
		if !e.Capabilities().Users {
			return fmt.Errorf("this engine has no users, so --user doesn't apply")
		}
		if err := e.CreateUser(ctx, name, userPassword); err != nil {
			return cliError(err)
		}
	}
	if err := e.CreateDatabase(ctx, name, db.CreateOpts{Owner: ownerIf(withUser, name)}); err != nil {
		return cliError(err)
	}
	if !withUser {
		fmt.Fprintf(out, "Created database %s\n", name)
		return nil
	}
	if err := e.Grant(ctx, name, name, db.GrantAll); err != nil {
		return cliError(err)
	}
	fmt.Fprintf(out, "Created database %s + user %s (granted ALL)\n", name, name)
	return nil
}

// ownerIf returns name when withUser is set, so CreateDatabase hands ownership to
// the just-created user; otherwise the engine default owner is used.
func ownerIf(withUser bool, name string) string {
	if withUser {
		return name
	}
	return ""
}

// NewDropCommand builds `dbwiz drop <db> --yes`: the non-interactive counterpart
// to the dashboard's type-to-confirm drop. Because there is no interactive
// confirmation in a script, the destructive action requires an explicit --yes —
// the same safety default the plan calls for (a script can't drop a database by
// forgetting a flag).
func NewDropCommand() *cobra.Command {
	var (
		targetFlag, password string
		yes                  bool
	)
	cmd := &cobra.Command{
		Use:   "drop <database>",
		Short: "Drop a database (requires --yes)",
		Long: "Drop a database on the target resolved from context.\n\n" +
			"This is destructive and irreversible, so it refuses to run without --yes —\n" +
			"the scripting stand-in for the dashboard's type-the-name confirmation.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Guard before connecting: a missing --yes should fail instantly, not
			// after opening a connection.
			if !yes {
				return fmt.Errorf("refusing to drop %q without --yes (this permanently deletes the database)", args[0])
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), adminTimeout)
			defer cancel()
			t, err := resolveTarget(ctx, targetFlag)
			if err != nil {
				return err
			}
			engine, err := open(ctx, t, password)
			if err != nil {
				return err
			}
			defer engine.Close()
			return runDrop(ctx, cmd.OutOrStdout(), engine, args[0])
		},
	}
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "container to act on (defaults to the used/only one)")
	cmd.Flags().StringVar(&password, "password", "", "password for the connection (or set DBWIZ_PASSWORD)")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the destructive drop (required)")
	return cmd
}

// runDrop drops the database. The engine refuses the currently-connected
// database and classifies an in-use failure, so the caller just renders the
// typed error plainly.
func runDrop(ctx context.Context, out io.Writer, e dbAdmin, name string) error {
	if err := db.ValidateIdent(name); err != nil {
		return err
	}
	if err := e.DropDatabase(ctx, name); err != nil {
		return cliError(err)
	}
	fmt.Fprintf(out, "Dropped database %s\n", name)
	return nil
}
