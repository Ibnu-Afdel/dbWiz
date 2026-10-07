// Package desktop installs DBWiz into the desktop: an XDG launcher entry and
// icon on any Linux desktop, and on Omarchy a launch-or-focus launcher plus a
// "Databases" section in the Omarchy menu. It only ever writes files the user's
// own `dbwiz desktop install` asked for, marks each one as DBWiz's, and
// `dbwiz desktop remove` takes back exactly those — never anything else.
package desktop

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed icon.svg
var iconSVG []byte

// managedKey marks a desktop entry as written by DBWiz, so remove never deletes
// a launcher the user made by hand.
const managedKey = "X-DBWiz-Managed=true"

// Menu block markers. Omarchy's menu loader drops full-line // comments, so the
// markers are invisible to it but let DBWiz find and replace its own block.
const (
	menuBegin = "  // >>> dbwiz — managed by `dbwiz desktop install`; remove with `dbwiz desktop remove`"
	menuEnd   = "  // <<< dbwiz"
)

// Options describes one install.
type Options struct {
	Exe      string // absolute path of the dbwiz binary the launcher runs
	Sudo     bool   // launch with --sudo (Docker is behind sudo on this machine)
	Omarchy  bool   // integrate with Omarchy's launcher and menu
	Menu     bool   // add the Omarchy menu section (Omarchy only)
	Home     string // the user's home directory
	DataHome string // $XDG_DATA_HOME, or ~/.local/share
}

// Paths are the files an install manages.
type Paths struct {
	Entry string // the .desktop launcher
	Icon  string // the scalable icon
	Menu  string // Omarchy's user menu extension (JSONC)
}

// PathsFor resolves where o's files live. Omarchy reads its menu extension from
// ~/.config regardless of $XDG_CONFIG_HOME, so that path follows Omarchy.
func PathsFor(o Options) Paths {
	return Paths{
		Entry: filepath.Join(o.DataHome, "applications", "dbwiz.desktop"),
		Icon:  filepath.Join(o.DataHome, "icons", "hicolor", "scalable", "apps", "dbwiz.svg"),
		Menu:  filepath.Join(o.Home, ".config", "omarchy", "extensions", "omarchy-menu.jsonc"),
	}
}

// shellSafe matches a path that needs no quoting in a shell command or an
// Exec line. Omarchy's launch-or-focus re-splits its command with eval, so the
// Omarchy route needs one.
var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)

// omarchyRoute reports whether the launcher can use Omarchy's launch-or-focus.
func (o Options) omarchyRoute() bool { return o.Omarchy && shellSafe.MatchString(o.Exe) }

// command is the dbwiz invocation with its flags.
func (o Options) command(args ...string) string {
	parts := []string{quote(o.Exe)}
	if o.Sudo {
		parts = append(parts, "--sudo")
	}
	return strings.Join(append(parts, args...), " ")
}

// quote single-quotes s for a POSIX shell when it isn't already safe.
func quote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// LaunchCommand is the Omarchy launch-or-focus command for DBWiz — what the
// launcher, the menu, and a suggested keybinding run. It is "" off Omarchy or
// when the binary's path can't go through Omarchy's launcher.
func LaunchCommand(o Options) string {
	if !o.omarchyRoute() {
		return ""
	}
	return "omarchy-launch-or-focus-tui " + o.command()
}

// Entry renders the .desktop launcher. On Omarchy it goes through
// omarchy-launch-or-focus-tui: that opens DBWiz in the user's chosen terminal
// with the org.omarchy.dbwiz app id, or focuses the window if DBWiz is already
// open. Elsewhere it is a standard Terminal=true entry any desktop can launch.
func Entry(o Options) string {
	var exec, terminal string
	if o.omarchyRoute() {
		exec, terminal = LaunchCommand(o), "false"
	} else {
		exec, terminal = desktopExec(o), "true"
	}
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Version=1.0",
		"Name=DBWiz",
		"GenericName=Database Browser",
		"Comment=Browse, administer, and query your local SQL databases",
		"Exec=" + exec,
		"Icon=dbwiz",
		"Terminal=" + terminal,
		"Categories=Development;Database;ConsoleOnly;",
		"Keywords=sql;database;postgres;postgresql;mysql;mariadb;sqlite;docker;",
		"StartupNotify=false",
		managedKey,
		"",
	}, "\n")
}

// desktopExec renders the Exec value for a plain entry, quoting per the
// Desktop Entry spec (double quotes, with ", `, $ and \ escaped).
func desktopExec(o Options) string {
	exe := o.Exe
	if !shellSafe.MatchString(exe) {
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
		exe = `"` + r.Replace(exe) + `"`
	}
	if o.Sudo {
		exe += " --sudo"
	}
	return exe
}

// menuItem is one Omarchy menu row, in the shape omarchy-menu.jsonc expects.
type menuItem struct {
	Icon        string `json:"icon"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Action      string `json:"action,omitempty"`
}

// MenuBlock renders DBWiz's section for the Omarchy menu: a root "Databases"
// submenu that opens DBWiz, lists databases, runs a health check, and reaches
// Omarchy's own database installer. One-shot commands run in Omarchy's floating
// presentation terminal, which waits for a key before closing.
func MenuBlock(o Options) string {
	floating := func(cmd string) string {
		return "omarchy-launch-floating-terminal-with-presentation " + quote(cmd)
	}
	items := []struct {
		id   string
		item menuItem
	}{
		{"dbwiz", menuItem{Icon: "\U000F01BC", Label: "Databases", Description: "DBWiz — browse and query your databases"}},
		{"dbwiz.open", menuItem{Icon: "\U000F01BC", Label: "Open DBWiz", Action: LaunchCommand(o)}},
		{"dbwiz.list", menuItem{Icon: "\U000F0279", Label: "List Databases", Action: floating(o.command("list"))}},
		{"dbwiz.health", menuItem{Icon: "\U000F05F6", Label: "Health Check", Action: floating(o.command("health"))}},
		{"dbwiz.add", menuItem{Icon: "\U000F0415", Label: "Add a Database Server", Action: floating("omarchy-install-docker-dbs")}},
	}
	lines := []string{menuBegin}
	for _, it := range items {
		lines = append(lines, fmt.Sprintf("  %q: %s,", it.id, marshal(it.item)))
	}
	return strings.Join(append(lines, menuEnd), "\n")
}

// marshal encodes without HTML escaping, so shell operators stay readable in
// the user's config file.
func marshal(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(b.String())
}

// Result reports what an install or remove did, for the CLI to print.
type Result struct {
	Changed []string // files written or removed
	Skipped []string // human-readable notes about what was left alone
}

// Install writes the launcher, the icon, and (on Omarchy, with Menu) the menu
// section. Re-running it updates them in place.
func Install(o Options) (Result, error) {
	var r Result
	p := PathsFor(o)
	if err := writeFile(p.Icon, iconSVG); err != nil {
		return r, err
	}
	r.Changed = append(r.Changed, p.Icon)

	if existing, err := os.ReadFile(p.Entry); err == nil && !strings.Contains(string(existing), managedKey) {
		return r, fmt.Errorf("%s exists and wasn't written by DBWiz — move it aside first", p.Entry)
	}
	if err := writeFile(p.Entry, []byte(Entry(o))); err != nil {
		return r, err
	}
	r.Changed = append(r.Changed, p.Entry)

	if o.Omarchy && o.Menu {
		if !o.omarchyRoute() {
			r.Skipped = append(r.Skipped, "Omarchy menu: the dbwiz path needs quoting, which Omarchy's launcher can't take — install dbwiz to a plain path")
			return r, nil
		}
		changed, err := upsertMenuFile(p.Menu, MenuBlock(o))
		if err != nil {
			return r, err
		}
		if changed {
			r.Changed = append(r.Changed, p.Menu)
		}
	}
	return r, nil
}

// Remove deletes the launcher and icon DBWiz wrote and strips its menu block.
// A launcher DBWiz didn't write is left alone.
func Remove(o Options) (Result, error) {
	var r Result
	p := PathsFor(o)

	switch data, err := os.ReadFile(p.Entry); {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return r, err
	case !strings.Contains(string(data), managedKey):
		r.Skipped = append(r.Skipped, p.Entry+" wasn't written by DBWiz; left it")
	default:
		if err := os.Remove(p.Entry); err != nil {
			return r, err
		}
		r.Changed = append(r.Changed, p.Entry)
	}

	if err := os.Remove(p.Icon); err == nil {
		r.Changed = append(r.Changed, p.Icon)
	} else if !errors.Is(err, os.ErrNotExist) {
		return r, err
	}

	data, err := os.ReadFile(p.Menu)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return r, nil
	case err != nil:
		return r, err
	}
	if out, found := removeMenuBlock(string(data)); found {
		if err := writeFile(p.Menu, []byte(out)); err != nil {
			return r, err
		}
		r.Changed = append(r.Changed, p.Menu)
	}
	return r, nil
}

// writeFile writes atomically (temp file + rename), creating parent dirs, so a
// crash never leaves a half-written config the desktop then fails to parse.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
