// Package state persists a little cross-session memory so DBWiz can pick up
// where you left off: the last target you connected to and the SQLite files
// you've opened. It lives in the XDG state directory
// ($XDG_STATE_HOME/dbwiz/state.json, default ~/.local/state/dbwiz/state.json) —
// state, not config: it's disposable, machine-local, and never required. Every
// read and write is best-effort; a missing, unreadable, or corrupt file yields
// an empty state rather than an error, so a cache problem can never keep the app
// from starting.
//
// It is a leaf package (no internal imports), and it stores no secrets: only a
// container name / engine label or a file path, never a password.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// recentCap bounds the recent-SQLite list so it stays a short pick-list.
const recentCap = 8

// Kind distinguishes the two kinds of target DBWiz can remember.
type Kind string

const (
	KindDocker Kind = "docker" // a database running in a Docker container
	KindSQLite Kind = "sqlite" // a local SQLite file
)

// Target is a remembered connection, identified by what's needed to reach it
// again non-interactively — a container name (the credential ladder re-resolves
// the rest on reconnect) or a file path. No password is ever stored.
type Target struct {
	Kind      Kind   `json:"kind"`
	Container string `json:"container,omitempty"`
	Engine    string `json:"engine,omitempty"`
	Path      string `json:"path,omitempty"`
}

// Label is the human-facing name for a target, used in the "continue" prompt.
func (t Target) Label() string {
	if t.Kind == KindSQLite {
		return filepath.Base(t.Path)
	}
	if t.Engine != "" {
		return t.Container + " (" + t.Engine + ")"
	}
	return t.Container
}

// State is the whole persisted document.
type State struct {
	Last         *Target  `json:"last,omitempty"`
	RecentSQLite []string `json:"recent_sqlite,omitempty"`
}

// mu serialises the load-modify-save mutators so concurrent writers (a connect
// landing while a recent list updates) can't clobber each other's changes.
var mu sync.Mutex

// Load reads the state file, returning an empty State on any problem so callers
// never have to handle an error to start.
func Load() State {
	path, err := statePath()
	if err != nil {
		return State{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}
	}
	return s
}

// Save writes the state atomically (temp file + rename) so a crash mid-write
// can't leave a truncated file that later fails to parse.
func (s State) Save() error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SetLastDocker records a Docker container as the last-used target.
func SetLastDocker(container, engine string) error {
	return mutate(func(s *State) {
		s.Last = &Target{Kind: KindDocker, Container: container, Engine: engine}
	})
}

// SetLastSQLite records a SQLite file as the last-used target and folds it into
// the recent list in one write.
func SetLastSQLite(path string) error {
	return mutate(func(s *State) {
		s.Last = &Target{Kind: KindSQLite, Path: path}
		s.RecentSQLite = prependUnique(s.RecentSQLite, path)
	})
}

// AddRecentSQLite prepends a path to the recent-files list (deduplicated, capped)
// without touching the last-used target.
func AddRecentSQLite(path string) error {
	return mutate(func(s *State) {
		s.RecentSQLite = prependUnique(s.RecentSQLite, path)
	})
}

// mutate applies fn to the loaded state and saves it under the package lock.
func mutate(fn func(*State)) error {
	mu.Lock()
	defer mu.Unlock()
	s := Load()
	fn(&s)
	return s.Save()
}

// prependUnique puts path at the front of the list, removing any earlier copy
// and capping the length.
func prependUnique(list []string, path string) []string {
	out := make([]string, 0, len(list)+1)
	out = append(out, path)
	for _, p := range list {
		if p != path {
			out = append(out, p)
		}
	}
	if len(out) > recentCap {
		out = out[:recentCap]
	}
	return out
}

// statePath returns $XDG_STATE_HOME/dbwiz/state.json, falling back to
// ~/.local/state/dbwiz/state.json — the same base the debug log uses.
func statePath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "dbwiz", "state.json"), nil
}
