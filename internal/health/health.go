// Package health runs DBWiz's connectivity self-check (v3 1.3): can we reach the
// Docker daemon, is the container running, is its port open, do the credentials
// authenticate, and does a trivial query round-trip? It returns a structured
// report so the same logic backs both the scriptable `dbwiz health` (exit code
// from Report.OK) and the TUI "doctor" screen.
//
// It imports db and docker but never tui; the dependency direction is
// tui/cmd → health → {db, docker}.
package health

import (
	"context"
	"errors"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// Status is one check's outcome. Skip means the check doesn't apply to this
// target (e.g. the Docker checks for a SQLite file); it never counts as failure.
type Status int

const (
	OK Status = iota
	Warn
	Fail
	Skip
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	case Skip:
		return "skip"
	}
	return "?"
}

// Check is one line of the report: a named probe, its outcome, and a
// plain-language detail.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Report is the whole self-check.
type Report struct {
	Checks []Check
}

// OK reports whether nothing failed — the value `dbwiz health` turns into its
// exit code. Warn and Skip do not count as failure.
func (r Report) OK() bool {
	for _, c := range r.Checks {
		if c.Status == Fail {
			return false
		}
	}
	return true
}

// Target is what to check: a resolved container (or a SQLite file) plus the
// connection details the auth and query checks use.
type Target struct {
	SQLite    bool
	Container string // container name; empty for SQLite or a manual host/port
	Port      int    // host port to TCP-probe; 0 skips the port check
	Kind      db.Kind
	DBTarget  db.Target // full connection target, including any recovered password
}

// Seams over Docker/DB so the whole self-check is testable without a live daemon
// or database. Production never reassigns them.
var (
	detect    = docker.Detect
	reachable = docker.Reachable
	newEngine = db.New
)

// Run executes the probes in order (daemon → container → port → auth → query)
// and returns the report. A SQLite target skips the Docker/port probes and
// checks only that the file opens and answers a query. The auth check's live
// engine is reused for the query round-trip and closed before returning.
func Run(ctx context.Context, t Target) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	var r Report

	if t.SQLite {
		r.Checks = append(r.Checks,
			Check{"Docker daemon", Skip, "SQLite target — not a container"},
			Check{"Container running", Skip, t.DBTarget.Path},
			Check{"Port reachable", Skip, "SQLite is a local file"},
		)
	} else {
		r.Checks = append(r.Checks, daemonAndContainer(ctx, t)...)
		r.Checks = append(r.Checks, portCheck(ctx, t))
	}

	auth, engine := authCheck(ctx, t)
	r.Checks = append(r.Checks, auth)
	if engine != nil {
		defer engine.Close()
		r.Checks = append(r.Checks, queryCheck(ctx, engine))
	} else {
		r.Checks = append(r.Checks, Check{"Query round-trip", Skip, "skipped — couldn't connect"})
	}
	return r
}

// daemonAndContainer probes the Docker daemon and, if reachable, whether the
// target container exists and is running.
func daemonAndContainer(ctx context.Context, t Target) []Check {
	containers, err := detect(ctx)
	if err != nil {
		return []Check{
			{"Docker daemon", Fail, errDetail(err)},
			{"Container running", Skip, "skipped — can't reach Docker"},
		}
	}
	daemon := Check{"Docker daemon", OK, "reachable"}
	for _, c := range containers {
		if c.Name == t.Container {
			if c.State == docker.StateRunning {
				return []Check{daemon, {"Container running", OK, t.Container + " is up"}}
			}
			return []Check{daemon, {"Container running", Fail, t.Container + " is stopped — start it first"}}
		}
	}
	return []Check{daemon, {"Container running", Fail, "no container named " + t.Container}}
}

// portCheck TCP-dials the host port — the truth of "can I connect", since a
// container can be Up yet not yet listening.
func portCheck(ctx context.Context, t Target) Check {
	if t.Port == 0 {
		return Check{"Port reachable", Skip, "no published host port"}
	}
	if reachable(ctx, t.Port) {
		return Check{"Port reachable", OK, "127.0.0.1 port is open"}
	}
	return Check{"Port reachable", Fail, "nothing is accepting connections on the host port yet"}
}

// authCheck opens a live connection with the resolved credentials. On success it
// returns the engine so the query check can reuse it; on failure the engine is
// nil and already closed.
func authCheck(ctx context.Context, t Target) (Check, db.Engine) {
	engine, err := newEngine(t.Kind)
	if err != nil {
		return Check{"Authentication", Fail, errDetail(err)}, nil
	}
	if err := engine.Connect(ctx, t.DBTarget); err != nil {
		_ = engine.Close()
		return Check{"Authentication", Fail, errDetail(err)}, nil
	}
	return Check{"Authentication", OK, "credentials accepted"}, engine
}

// queryCheck runs the cheapest possible statement to prove the connection can
// actually serve queries, not just handshake.
func queryCheck(ctx context.Context, engine db.Engine) Check {
	if _, err := engine.Query(ctx, "SELECT 1"); err != nil {
		return Check{"Query round-trip", Fail, errDetail(err)}
	}
	return Check{"Query round-trip", OK, "SELECT 1 succeeded"}
}

// errDetail reduces an error to a one-line detail, preferring a typed error's
// human title over a raw driver string.
func errDetail(err error) string {
	var de *db.DBError
	if errors.As(err, &de) {
		if de.Detail != "" {
			return de.Title + ": " + de.Detail
		}
		return de.Title
	}
	var dke *docker.DockerError
	if errors.As(err, &dke) {
		if dke.Detail != "" {
			return dke.Title + ": " + dke.Detail
		}
		return dke.Title
	}
	return err.Error()
}
