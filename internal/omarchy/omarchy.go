// Package omarchy knows the parts of an Omarchy (https://omarchy.org) machine
// DBWiz integrates with: whether this is one, the live theme palette, and how
// Docker access is gated. It is a leaf package (no internal imports) so docker,
// styles, and cmd can all depend on it.
//
// Everything here is read-only and degrades quietly: on a non-Omarchy Linux box
// Detect reports false and the palette is simply absent, so callers fall back to
// their generic behavior.
//
// Paths track Omarchy 4, which ships as a system package under /usr/share/omarchy
// (exported as $OMARCHY_PATH) and keeps per-user state under
// ~/.local/state/omarchy. The Omarchy 3 layout (a git checkout under
// ~/.local/share/omarchy, the current theme under ~/.config/omarchy/current) is
// still recognized so older installs keep working.
package omarchy

import (
	"os"
	"path/filepath"
)

// systemPath is where the Omarchy 4 package installs itself. A var so tests can
// point it at a temp dir.
var systemPath = "/usr/share/omarchy"

// Detect reports whether this looks like an Omarchy machine.
func Detect() bool {
	home, _ := os.UserHomeDir()
	return detectIn(os.Getenv("OMARCHY_PATH"), home)
}

// detectIn is the testable core of Detect: Omarchy is present when $OMARCHY_PATH
// names a directory, the system package is installed, or the legacy per-user
// checkout exists.
func detectIn(envPath, home string) bool {
	if envPath != "" && isDir(envPath) {
		return true
	}
	if isDir(systemPath) {
		return true
	}
	return home != "" && isDir(filepath.Join(home, ".local", "share", "omarchy"))
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
