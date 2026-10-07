package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Ibnu-Afdel/dbwiz/internal/desktop"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/omarchy"
)

// NewDesktopCommand builds `dbwiz desktop install|remove`: putting DBWiz in the
// app launcher (and, on Omarchy, the Omarchy menu) or taking it back out.
func NewDesktopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "desktop",
		Short: "Add DBWiz to your app launcher (and the Omarchy menu)",
		Long: "Install or remove DBWiz's desktop integration.\n\n" +
			"On any Linux desktop this adds a launcher entry and icon, so DBWiz shows up in\n" +
			"your app launcher. On Omarchy the launcher opens DBWiz in your terminal — or\n" +
			"focuses it if it's already open — and a \"Databases\" section is added to the\n" +
			"Omarchy menu (skip it with --no-menu).",
	}
	cmd.AddCommand(newDesktopInstallCommand(), newDesktopRemoveCommand())
	return cmd
}

func newDesktopInstallCommand() *cobra.Command {
	var noMenu bool
	c := &cobra.Command{
		Use:          "install",
		Short:        "Add the launcher entry, icon, and Omarchy menu section",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := desktopOptions()
			if err != nil {
				return err
			}
			o.Menu = !noMenu
			r, err := desktop.Install(o)
			report(cmd.OutOrStdout(), "installed", r)
			if err != nil {
				return err
			}
			refreshDesktopCaches(o)
			printInstallNotes(cmd.OutOrStdout(), o)
			return nil
		},
	}
	c.Flags().BoolVar(&noMenu, "no-menu", false, "don't add the Databases section to the Omarchy menu")
	return c
}

func newDesktopRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "remove",
		Short:        "Remove everything `dbwiz desktop install` added",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := desktopOptions()
			if err != nil {
				return err
			}
			r, err := desktop.Remove(o)
			report(cmd.OutOrStdout(), "removed", r)
			if err == nil && len(r.Changed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Nothing to remove.")
			}
			if err == nil {
				refreshDesktopCaches(o)
			}
			return err
		},
	}
}

// desktopOptions gathers the install context from the running system.
func desktopOptions() (desktop.Options, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return desktop.Options{}, err
	}
	exe, err := installedExe()
	if err != nil {
		return desktop.Options{}, err
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	return desktop.Options{
		Exe:      exe,
		Sudo:     !docker.SocketReachable(),
		Omarchy:  omarchy.Detect(),
		Home:     home,
		DataHome: dataHome,
	}, nil
}

// installedExe returns the path the launcher should run: the dbwiz on PATH
// when that's this binary (a stable path that survives upgrades, like
// /usr/bin/dbwiz), else this binary's own path. A `go run` build lives in a
// temp dir that's gone once it exits, so it's refused.
func installedExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		real = exe
	}
	if strings.Contains(real, string(filepath.Separator)+"go-build") {
		return "", errors.New("this is a temporary `go run` build — install dbwiz first (go install github.com/Ibnu-Afdel/dbwiz@latest), then run `dbwiz desktop install`")
	}
	if onPath, err := exec.LookPath("dbwiz"); err == nil {
		if abs, err := filepath.Abs(onPath); err == nil {
			if r, err := filepath.EvalSymlinks(abs); err == nil && r == real {
				return abs, nil
			}
		}
	}
	return real, nil
}

func report(w io.Writer, verb string, r desktop.Result) {
	for _, p := range r.Changed {
		fmt.Fprintf(w, "%s %s\n", verb, p)
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(w, "skipped %s\n", s)
	}
}

// refreshDesktopCaches nudges desktops that cache launcher entries and icons.
// Both tools are optional; Omarchy's launcher watches the directories itself.
func refreshDesktopCaches(o desktop.Options) {
	if p, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(p, "-q", filepath.Join(o.DataHome, "applications")).Run()
	}
	if p, err := exec.LookPath("gtk-update-icon-cache"); err == nil {
		_ = exec.Command(p, "-q", "-t", filepath.Join(o.DataHome, "icons", "hicolor")).Run()
	}
}

func printInstallNotes(w io.Writer, o desktop.Options) {
	fmt.Fprintln(w)
	if o.Omarchy {
		if o.Menu {
			fmt.Fprintln(w, "DBWiz is in the app launcher, and in the Omarchy menu under Databases.")
		} else {
			fmt.Fprintln(w, "DBWiz is in the app launcher.")
		}
		if cmd := desktop.LaunchCommand(o); cmd != "" {
			fmt.Fprintln(w, "For a key of its own, add this to ~/.config/hypr/bindings.lua:")
			fmt.Fprintf(w, "  o.bind(\"SUPER + SHIFT + ALT + D\", \"DBWiz\", %q)\n", cmd)
		}
	} else {
		fmt.Fprintln(w, "DBWiz is in your app launcher.")
	}
	if o.Sudo {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Your user can't reach the Docker socket, so the launcher runs `dbwiz --sudo` and")
		fmt.Fprintln(w, "asks for your password once per launch. To skip that: "+docker.PermissionFix()+",")
		fmt.Fprintln(w, "then re-run `dbwiz desktop install`.")
	}
}
