package tui

import (
	"bytes"
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/screens"
)

// These are end-to-end teatest/v2 scenarios (Phase 8.2): they drive the real
// root model through a real Bubble Tea program and assert on rendered frames.
// No live Docker or database is needed — screens.DetectFn and screens.NewEngineFn
// are swapped for deterministic fakes. Because those seams are package globals,
// none of these tests run in parallel.

// waitFor blocks until the program's output contains every want string, failing
// after a short deadline so a wiring regression fails fast instead of hanging the
// suite (the trap that pushed Phase 4 off teatest — here detection is instant and
// fake). teatest.WaitFor drains the stream, and Bubble Tea writes diff frames, so
// each call must target text newly rendered by the step that precedes it; pass
// multiple wants only when they land in the same frame.
func waitFor(t *testing.T, tm *teatest.TestModel, want ...string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		for _, w := range want {
			if !bytes.Contains(b, []byte(w)) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(10*time.Millisecond))
}

// key sends a single named special key (Enter, F5, …).
func sendKey(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code})
}

// --- fakes -----------------------------------------------------------------

// teatestEngine is a scripted db.Engine used to drive the full app without a
// server. Query always answers with a distinctive token so a scenario can prove
// the statement round-tripped.
type teatestEngine struct{ caps db.Capabilities }

func (e *teatestEngine) Kind() db.Kind                            { return db.KindPostgres }
func (e *teatestEngine) Capabilities() db.Capabilities            { return e.caps }
func (e *teatestEngine) Connect(context.Context, db.Target) error { return nil }
func (e *teatestEngine) Close() error                             { return nil }

func (e *teatestEngine) ListDatabases(context.Context) ([]db.Database, error) {
	return []db.Database{{Name: "postgres"}, {Name: "appdb"}}, nil
}
func (e *teatestEngine) ListTables(_ context.Context, database string) ([]db.Table, error) {
	if database == "appdb" {
		return []db.Table{{Name: "users", Rows: 3}}, nil
	}
	return nil, nil
}
func (e *teatestEngine) DescribeTable(context.Context, string, string) ([]db.Column, error) {
	return []db.Column{{Name: "id", Type: "int"}}, nil
}
func (e *teatestEngine) PreviewRows(context.Context, string, string, int) (db.Result, error) {
	return db.Result{Columns: []string{"id"}, Rows: [][]any{{"1"}}}, nil
}
func (e *teatestEngine) ListUsers(context.Context) ([]db.User, error) {
	return []db.User{{Name: "alice"}}, nil
}
func (e *teatestEngine) CreateUser(context.Context, string, string) error            { return nil }
func (e *teatestEngine) DropUser(context.Context, string) error                      { return nil }
func (e *teatestEngine) CreateDatabase(context.Context, string, db.CreateOpts) error { return nil }
func (e *teatestEngine) DropDatabase(context.Context, string) error                  { return nil }
func (e *teatestEngine) Grant(context.Context, string, string, db.GrantLevel) error  { return nil }
func (e *teatestEngine) Revoke(context.Context, string, string, db.GrantLevel) error { return nil }
func (e *teatestEngine) AlterUser(context.Context, string, bool, bool) error         { return nil }
func (e *teatestEngine) SetPassword(context.Context, string, string) error           { return nil }
func (e *teatestEngine) DatabasePrivileges() []db.Privilege {
	return []db.Privilege{db.PrivConnect, db.PrivCreate, db.PrivTemporary}
}
func (e *teatestEngine) ListGrants(context.Context, string, string) ([]db.Privilege, error) {
	return []db.Privilege{db.PrivConnect}, nil
}
func (e *teatestEngine) SetGrant(context.Context, string, string, db.Privilege, bool) error {
	return nil
}
func (e *teatestEngine) Query(context.Context, string) (db.Result, error) {
	return db.Result{Columns: []string{"answer"}, Rows: [][]any{{"ANSWER_42"}}}, nil
}

// runningPG is a fully-specified container so the credential ladder resolves
// entirely from its recovered creds — no docker inspect / .env rungs fire.
func runningPG() docker.Container {
	return docker.Container{
		Name:     "pg",
		Engine:   docker.EnginePostgres,
		State:    docker.StateRunning,
		HostPort: 5432,
		Creds:    docker.Creds{User: "postgres", Password: "secret", Database: "appdb"},
	}
}

// swapSeams points the detection and engine boundaries at fakes for one test and
// restores them afterwards.
func swapSeams(t *testing.T, detect func(context.Context) ([]docker.Container, error), engine db.Engine) {
	t.Helper()
	// Isolate the state cache so these scenarios neither read the developer's
	// real last-used target (which would add a "continue" row and shift the menu)
	// nor write to it when a connection lands.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	oldDetect, oldEngine := screens.DetectFn, screens.NewEngineFn
	screens.DetectFn = detect
	screens.NewEngineFn = func(db.Kind) (db.Engine, error) { return engine, nil }
	t.Cleanup(func() {
		screens.DetectFn = oldDetect
		screens.NewEngineFn = oldEngine
	})
}

func newTestModel(t *testing.T, m tea.Model) *teatest.TestModel {
	return teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
}

// --- scenarios --------------------------------------------------------------

// TestE2EHappyPath drives the whole spine: detect → home → connect → dashboard →
// run a query and see its result. Every boundary is faked; nothing touches
// Docker or a real server.
func TestE2EHappyPath(t *testing.T) {
	swapSeams(t, func(context.Context) ([]docker.Container, error) {
		return []docker.Container{runningPG()}, nil
	}, &teatestEngine{caps: db.Capabilities{Users: true, Grants: true, MultipleDatabases: true}})

	tm := newTestModel(t, newModel())

	// Detection resolves instantly (fake) → the home menu.
	waitFor(t, tm, "Use an existing database")

	// The cursor starts on "Use an existing database"; Enter routes straight to
	// connect (one running container) → dashboard.
	sendKey(tm, tea.KeyEnter)
	waitFor(t, tm, "Navigator")

	// Focus the SQL editor, type a statement, and run it with F5.
	tm.Send(tea.KeyPressMsg{Code: 'e', Text: "e"})
	tm.Type("SELECT 1")
	sendKey(tm, tea.KeyF5)
	waitFor(t, tm, "ANSWER_42")

	tm.Quit()
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// TestE2EDaemonDown shows a classified Docker failure lands on the plain-language
// error screen (not a stack trace, not a hang).
func TestE2EDaemonDown(t *testing.T) {
	swapSeams(t, func(context.Context) ([]docker.Container, error) {
		return nil, &docker.DockerError{
			Kind:   docker.DockerErrDaemonDown,
			Title:  "Docker isn't running",
			Detail: "The docker daemon isn't responding.",
			Hint:   "start it with: sudo systemctl start docker",
		}
	}, nil)

	tm := newTestModel(t, newModel())
	waitFor(t, tm, "Docker isn't running")

	tm.Quit()
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// TestE2ENoContainers shows the empty state is reachable and never a dead end:
// it names the situation and offers the SQLite escape hatch.
func TestE2ENoContainers(t *testing.T) {
	swapSeams(t, func(context.Context) ([]docker.Container, error) {
		return nil, nil // docker ran, nothing matched
	}, nil)

	tm := newTestModel(t, newModel())
	waitFor(t, tm, "No database containers found", "open a SQLite file")

	tm.Quit()
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// TestE2EDestructiveConfirm proves the type-the-name gate on a destructive
// action: Enter is inert until the database name is typed verbatim, then the
// drop fires and a confirmation toast appears.
func TestE2EDestructiveConfirm(t *testing.T) {
	eng := &teatestEngine{caps: db.Capabilities{Users: true, Grants: true, MultipleDatabases: true}}
	swapSeams(t, func(context.Context) ([]docker.Container, error) {
		return []docker.Container{runningPG()}, nil
	}, eng)

	// Seed straight onto a live dashboard so the test focuses on the confirm gate.
	// currentDB is "scratch" (shown in the status bar) so it differs from the
	// listed databases — that lets the wait below key off "appdb", which appears
	// only once the async ListDatabases lands in the navigator.
	target := db.Target{Host: "127.0.0.1", Port: 5432, User: "postgres", Password: "secret", Database: "scratch"}
	tm := newTestModel(t, newModelWith(screens.NewDashboard(eng, target, runningPG())))
	waitFor(t, tm, "appdb")

	// Delete the highlighted database (postgres) — opens the confirm, which
	// starts with the gate disabled.
	tm.Send(tea.KeyPressMsg{Code: 'D', Text: "D"})
	waitFor(t, tm, "Delete database", "enter disabled until the name matches")

	// A premature Enter is inert (the confirm can't match an empty string).
	sendKey(tm, tea.KeyEnter)

	// Typing the exact name arms the gate ("enter to confirm"), and Enter drops
	// it — proving the destructive action only fires on a verbatim match.
	tm.Type("postgres")
	waitFor(t, tm, "enter to confirm")
	sendKey(tm, tea.KeyEnter)
	waitFor(t, tm, "Dropped database")

	tm.Quit()
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}
