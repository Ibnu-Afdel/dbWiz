// Package config reads DBWiz's opt-in user configuration from
// $XDG_CONFIG_HOME/dbwiz/config.toml (default ~/.config/dbwiz/config.toml). It
// is config, not state: hand-authored, and entirely optional — a missing file
// yields the zero Config and the app runs exactly as before. Only a file that
// exists but doesn't parse is reported as an error, so a typo is surfaced rather
// than silently ignored.
//
// It is a leaf package (no internal imports) and stores no secrets: a saved
// target carries host/port/user for a non-Docker database, never a password
// (DBWiz prompts for that at connect time, as it does for containers).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the whole config document. Every field is optional; the zero value
// is a valid, do-nothing config.
type Config struct {
	// Theme names a color preference applied at startup (see styles.Apply). An
	// unknown name falls back to the default palette.
	Theme string `toml:"theme"`
	// DefaultRowLimit overrides how many rows a table preview pulls. Zero or
	// negative means "use the built-in default".
	DefaultRowLimit int `toml:"default_row_limit"`
	// Targets are saved manual (non-Docker) databases, offered on the home menu.
	Targets []ManualTarget `toml:"target"`
	// Editor holds SQL-editor preferences (v2 2.4).
	Editor EditorConfig `toml:"editor"`
}

// EditorConfig collects the opt-in SQL-editor preferences. The zero value is the
// current, non-modal editor.
type EditorConfig struct {
	// Vim turns on the bespoke modal (vim-style) editor: the SQL pane opens in
	// normal mode with hjkl/w/b motions and i/a/o inserts (v2 2.4). Off by default.
	Vim bool `toml:"vim"`
}

// ManualTarget is a saved connection to a database DBWiz can't discover through
// Docker — a remote or host-native server the user reaches by host/port. No
// password is stored; it is prompted at connect time.
type ManualTarget struct {
	Name     string `toml:"name"`
	Engine   string `toml:"engine"` // "postgres", "mysql", or "mariadb"
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Database string `toml:"database"` // optional initial/maintenance database
}

// knownEngines is the set of engine strings a saved target may name. SQLite is
// excluded: it's a file, opened through the SQLite picker, not a host/port.
var knownEngines = map[string]bool{"postgres": true, "mysql": true, "mariadb": true}

// Valid reports whether the target has the minimum needed to attempt a
// connection, with a plain-language reason when it doesn't. Invalid targets are
// dropped from the menu rather than offered as dead links.
func (t ManualTarget) Valid() (bool, string) {
	switch {
	case strings.TrimSpace(t.Name) == "":
		return false, "missing name"
	case !knownEngines[t.Engine]:
		return false, fmt.Sprintf("engine %q must be postgres, mysql, or mariadb", t.Engine)
	case strings.TrimSpace(t.Host) == "":
		return false, "missing host"
	case t.Port <= 0 || t.Port > 65535:
		return false, "missing or out-of-range port"
	case strings.TrimSpace(t.User) == "":
		return false, "missing user"
	}
	return true, ""
}

// Load reads and parses the config file. A missing file is not an error — it
// returns the zero Config so the app stays fully functional without any config.
// A file that exists but doesn't parse is returned as an error for the caller to
// surface.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return c, nil
}

// ValidTargets returns only the saved targets that pass Valid, preserving order.
func (c Config) ValidTargets() []ManualTarget {
	var out []ManualTarget
	for _, t := range c.Targets {
		if ok, _ := t.Valid(); ok {
			out = append(out, t)
		}
	}
	return out
}

// Path returns $XDG_CONFIG_HOME/dbwiz/config.toml, falling back to
// ~/.config/dbwiz/config.toml — the conventional per-user config location.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "dbwiz", "config.toml"), nil
}
