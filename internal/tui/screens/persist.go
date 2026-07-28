package screens

import (
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// remember* record the just-connected target in the cross-session state cache so
// the next launch can offer to continue here (v2 Step 1.2). They are strictly
// best-effort: the write is fire-and-forget and its error is dropped, because a
// caching hiccup must never interrupt actually connecting to a database.

func rememberDocker(c docker.Container) {
	_ = state.SetLastDocker(c.Name, string(c.Engine))
}

func rememberSQLite(path string) {
	_ = state.SetLastSQLite(path)
}

// historyKeyFor derives the per-target key under which a dashboard's query
// history is stored (v2 Step 2.1). A SQLite target is identified by its file
// path; every other engine by its container name. It mirrors how remember*
// feeds state.Target, so the same connection maps to the same key across
// sessions.
func historyKeyFor(c docker.Container, t db.Target) string {
	if t.Path != "" {
		return state.HistoryKey(state.Target{Kind: state.KindSQLite, Path: t.Path})
	}
	return state.HistoryKey(state.Target{Kind: state.KindDocker, Container: c.Name})
}

// seedHistory reads a target's persisted statements back into the oldest-first
// order the in-memory ring expects (state stores newest-first). It's best-effort:
// an empty or missing store just yields an empty ring.
func seedHistory(key string) []string {
	entries := state.History(key)
	ring := make([]string, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		ring = append(ring, entries[i].SQL)
	}
	return ring
}
