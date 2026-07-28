// Package state persists a little cross-session memory so DBWiz can pick up
// where you left off: the last target you connected to and the SQLite files
// you've opened. It lives in the XDG state directory
// ($XDG_STATE_HOME/dbwiz/state.json, default ~/.local/state/dbwiz/state.json) —
// state, not config: it's disposable, machine-local, and never required. Every
// read and write is best-effort; a missing, unreadable, or corrupt file yields
// an empty state rather than an error, so a cache problem can never keep the app
// from starting.
//
// It is a leaf package (no internal imports). It stores no secrets: a container
// name / engine label, a file path, and — for per-target query history and saved
// queries — the statement text the user ran or named. Callers are responsible
// for masking any password literal (a CREATE USER … PASSWORD '…') before handing
// SQL to AddHistory or AddSaved; this package stores whatever text it's given
// verbatim.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// recentCap bounds the recent-SQLite list so it stays a short pick-list.
const recentCap = 8

// historyCap bounds how many statements are remembered per target so the store
// stays small and the pick-list scannable. It's generous enough to hold weeks of
// a target's real queries but bounded so the file can't grow without end.
const historyCap = 200

// savedCap bounds how many named queries are kept per scope (each target, plus
// the global scope). Saved queries are curated by hand, so this is generous — a
// user is unlikely to name a hundred favourites — but bounded like everything
// else so the file can't grow without end.
const savedCap = 100

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

// HistoryEntry is one previously-run statement: the SQL text and when it ran.
// At is Unix seconds so the pick-list can show "3m ago" and order newest-first.
// No result data and no credentials are ever stored — just the statement text.
type HistoryEntry struct {
	SQL string `json:"sql"`
	At  int64  `json:"at"`
}

// SavedQuery is a named, curated statement the user chose to keep (v2 2.2). Name
// is the human label picked at save time; SQL is the statement; At is Unix
// seconds of the last save, so a re-save floats it to the top. Like history it
// stores no result data and no credentials — the caller masks any password
// literal before saving (see AddSaved).
type SavedQuery struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
	At   int64  `json:"at"`
}

// State is the whole persisted document.
type State struct {
	Last         *Target  `json:"last,omitempty"`
	RecentSQLite []string `json:"recent_sqlite,omitempty"`
	// History maps a target key (see HistoryKey) to that target's statements,
	// newest first. Keyed per target so one connection's history never bleeds
	// into another's.
	History map[string][]HistoryEntry `json:"history,omitempty"`
	// Saved maps a target key to that target's named queries, newest first. Like
	// History it is keyed per target (see HistoryKey); queries meant to be usable
	// from any target live in SavedGlobal instead.
	Saved map[string][]SavedQuery `json:"saved,omitempty"`
	// SavedGlobal holds named queries not tied to one target — offered on every
	// connection's saved-query picker.
	SavedGlobal []SavedQuery `json:"saved_global,omitempty"`
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

// HistoryKey is the stable per-target identifier under which that target's query
// history is stored: a container name for Docker, a file path for SQLite. It
// reuses Target so callers build it exactly as they build a last-used target.
func HistoryKey(t Target) string {
	if t.Kind == KindSQLite {
		return string(KindSQLite) + ":" + t.Path
	}
	return string(KindDocker) + ":" + t.Container
}

// AddHistory records a statement as the newest entry for key, dropping an
// immediate duplicate of the current newest (re-running the same statement
// doesn't clutter the list) and capping the per-target length. Blank statements
// are ignored.
func AddHistory(key, sql string) error {
	if key == "" || sql == "" {
		return nil
	}
	return mutate(func(s *State) {
		if s.History == nil {
			s.History = map[string][]HistoryEntry{}
		}
		list := s.History[key]
		if len(list) > 0 && list[0].SQL == sql {
			list[0].At = time.Now().Unix() // same statement — just refresh its time
			s.History[key] = list
			return
		}
		entry := HistoryEntry{SQL: sql, At: time.Now().Unix()}
		list = append([]HistoryEntry{entry}, list...)
		if len(list) > historyCap {
			list = list[:historyCap]
		}
		s.History[key] = list
	})
}

// History returns a target's statements newest-first, or nil if there are none.
// It is best-effort like Load: any read problem yields an empty slice.
func History(key string) []HistoryEntry {
	return Load().History[key]
}

// AddSaved stores a named query as the newest entry for a scope, upserting by
// name: saving under a name that already exists in that scope replaces its SQL
// and floats it to the top rather than adding a duplicate. An empty key targets
// the global scope (usable from any connection); any other key is a per-target
// scope (see HistoryKey). Blank names or statements are ignored. The list is
// capped per scope.
func AddSaved(key, name, sql string) error {
	if name == "" || sql == "" {
		return nil
	}
	return mutate(func(s *State) {
		if key == "" {
			s.SavedGlobal = upsertSaved(s.SavedGlobal, name, sql)
			return
		}
		if s.Saved == nil {
			s.Saved = map[string][]SavedQuery{}
		}
		s.Saved[key] = upsertSaved(s.Saved[key], name, sql)
	})
}

// DeleteSaved removes the named query from a scope (empty key = global). A name
// that isn't present is a no-op. An emptied per-target list is dropped from the
// map so the file doesn't accumulate bare keys.
func DeleteSaved(key, name string) error {
	return mutate(func(s *State) {
		if key == "" {
			s.SavedGlobal = removeSaved(s.SavedGlobal, name)
			return
		}
		list := removeSaved(s.Saved[key], name)
		if len(list) == 0 {
			delete(s.Saved, key)
			return
		}
		s.Saved[key] = list
	})
}

// Saved returns a target's named queries newest-first, or nil if there are none.
// Best-effort like Load.
func Saved(key string) []SavedQuery {
	return Load().Saved[key]
}

// GlobalSaved returns the named queries not tied to a target, newest-first.
func GlobalSaved() []SavedQuery {
	return Load().SavedGlobal
}

// upsertSaved puts name→sql at the front of the list, removing any earlier entry
// with the same name (case-sensitive) so a re-save moves it to the top instead
// of duplicating, and caps the length.
func upsertSaved(list []SavedQuery, name, sql string) []SavedQuery {
	entry := SavedQuery{Name: name, SQL: sql, At: time.Now().Unix()}
	out := make([]SavedQuery, 0, len(list)+1)
	out = append(out, entry)
	for _, q := range list {
		if q.Name != name {
			out = append(out, q)
		}
	}
	if len(out) > savedCap {
		out = out[:savedCap]
	}
	return out
}

// removeSaved returns the list without the named entry (case-sensitive),
// preserving order.
func removeSaved(list []SavedQuery, name string) []SavedQuery {
	out := make([]SavedQuery, 0, len(list))
	for _, q := range list {
		if q.Name != name {
			out = append(out, q)
		}
	}
	return out
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
