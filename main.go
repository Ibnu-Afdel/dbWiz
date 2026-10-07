package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/cmd"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui"
)

// version is the build version reported by --version. It defaults to "dev" and
// is overridden at release time via the linker:
//
//	go build -ldflags "-X main.version=v1.0.0" .
//
// GoReleaser/the release workflow set it from the git tag.
var version = "dev"

// rootCmd is the base command for the CLI. With no subcommand it launches the
// TUI; subcommands (added from v2 on) stay available for scripting. Cobra still
// handles --help and --version without entering the TUI.
var rootCmd = &cobra.Command{
	Use:   "dbwiz",
	Short: "DBWiz — browse and manage local databases running in Docker",
	Long: "DBWiz detects the databases running in your local Docker containers and " +
		"lets you browse, administer, and query them from a friendly terminal UI.\n\n" +
		"Run with no arguments to launch the interactive UI.",
	Version: version,
	// Print just "dbwiz <version>" for --version rather than the default template.
	// Don't print usage on runtime errors from the TUI; usage is for arg errors.
	SilenceUsage: true,
	// main() prints the returned error once and exits non-zero; without this cobra
	// would also print it, so a failing subcommand would report twice.
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return enableDockerSudo(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return tui.Run()
	},
}

// enableDockerSudo honors --sudo (or DBWIZ_SUDO=1): when this session can't
// reach the Docker socket — the Omarchy 4 default, which keeps users out of the
// root-equivalent docker group — it asks sudo for the password once, up front in
// the terminal, and routes DBWiz's docker calls through it. With a reachable
// socket it does nothing, so the flag is safe to leave in a launcher or alias.
func enableDockerSudo(cmd *cobra.Command) error {
	on, _ := cmd.Flags().GetBool("sudo")
	if !on && os.Getenv("DBWIZ_SUDO") != "1" {
		return nil
	}
	if docker.SocketReachable() {
		return nil
	}
	if !docker.SudoAvailable() {
		return fmt.Errorf("--sudo: sudo isn't installed")
	}
	auth := docker.AuthorizeSudo()
	auth.Stdin, auth.Stdout, auth.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := auth.Run(); err != nil {
		return fmt.Errorf("--sudo: sudo didn't authorize: %w", err)
	}
	docker.SetSudo(true)
	return nil
}

// main executes the root command.
func main() {
	// Scripting subcommands (v2 on) live in package cmd; the bare `dbwiz` still
	// launches the TUI via rootCmd's RunE.
	rootCmd.AddCommand(
		cmd.NewKeysCommand(),
		cmd.NewUseCommand(),
		cmd.NewListCommand(),
		cmd.NewSetupCommand(),
		cmd.NewCreateCommand(),
		cmd.NewDropCommand(),
		cmd.NewQueryCommand(),
		cmd.NewExplainCommand(),
		cmd.NewDumpCommand(),
		cmd.NewRestoreCommand(),
		cmd.NewURLCommand(),
		cmd.NewHealthCommand(),
		cmd.NewExtCommand(),
		cmd.NewSchemaCommand(),
		cmd.NewMigrationsCommand(),
	)
	rootCmd.PersistentFlags().Bool("sudo", false,
		"reach Docker through sudo when your user can't use its socket (asks for your password once)")
	// "dbwiz v1.0.0" rather than cobra's default "dbwiz version v1.0.0".
	rootCmd.SetVersionTemplate("dbwiz {{.Version}}\n")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var de *docker.DockerError
		if errors.As(err, &de) && de.Kind == docker.DockerErrSocketPermission && !docker.SudoEnabled() {
			fmt.Fprintln(os.Stderr, "  try: re-run with --sudo")
			fmt.Fprintln(os.Stderr, "  or:  "+docker.PermissionFix())
		}
		os.Exit(1)
	}
}
