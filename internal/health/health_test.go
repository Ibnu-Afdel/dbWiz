package health

import (
	"context"
	"errors"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// fakeEngine implements only the methods the self-check calls; embedding the
// interface makes any unexpected call panic loudly rather than pass silently.
type fakeEngine struct {
	db.Engine
	connectErr error
	queryErr   error
}

func (f *fakeEngine) Connect(context.Context, db.Target) error { return f.connectErr }
func (f *fakeEngine) Query(context.Context, string) (db.Result, error) {
	return db.Result{}, f.queryErr
}
func (f *fakeEngine) Close() error { return nil }

// swap installs fake seams and restores them after the test.
func swap(t *testing.T, containers []docker.Container, detectErr error, portOpen bool, eng *fakeEngine, engErr error) {
	t.Helper()
	od, or, on := detect, reachable, newEngine
	detect = func(context.Context) ([]docker.Container, error) { return containers, detectErr }
	reachable = func(context.Context, int) bool { return portOpen }
	newEngine = func(db.Kind) (db.Engine, error) {
		if engErr != nil {
			return nil, engErr
		}
		return eng, nil
	}
	t.Cleanup(func() { detect, reachable, newEngine = od, or, on })
}

func status(r Report, name string) Status {
	for _, c := range r.Checks {
		if c.Name == name {
			return c.Status
		}
	}
	return Status(-1)
}

func pgTarget() Target {
	return Target{Container: "postgres18", Port: 5432, Kind: db.KindPostgres,
		DBTarget: db.Target{Host: "127.0.0.1", Port: 5432, User: "postgres", Database: "postgres"}}
}

// TestAllGreen is the happy path: every probe passes and the report is OK.
func TestAllGreen(t *testing.T) {
	swap(t, []docker.Container{{Name: "postgres18", State: docker.StateRunning}}, nil, true, &fakeEngine{}, nil)
	r := Run(context.Background(), pgTarget())
	if !r.OK() {
		t.Fatalf("expected OK report, got %+v", r.Checks)
	}
	for _, name := range []string{"Docker daemon", "Container running", "Port reachable", "Authentication", "Query round-trip"} {
		if status(r, name) != OK {
			t.Errorf("%s not OK: %+v", name, r.Checks)
		}
	}
}

// TestDaemonDown fails the daemon check and skips the container check.
func TestDaemonDown(t *testing.T) {
	swap(t, nil, errors.New("cannot connect"), true, &fakeEngine{}, nil)
	r := Run(context.Background(), pgTarget())
	if status(r, "Docker daemon") != Fail {
		t.Errorf("daemon should fail: %+v", r.Checks)
	}
	if status(r, "Container running") != Skip {
		t.Errorf("container check should skip when the daemon is down: %+v", r.Checks)
	}
	if r.OK() {
		t.Error("report should not be OK")
	}
}

// TestContainerStopped fails the container check.
func TestContainerStopped(t *testing.T) {
	swap(t, []docker.Container{{Name: "postgres18", State: docker.StateStopped}}, nil, false, &fakeEngine{}, nil)
	r := Run(context.Background(), pgTarget())
	if status(r, "Container running") != Fail {
		t.Errorf("stopped container should fail: %+v", r.Checks)
	}
}

// TestPortClosed fails the port probe.
func TestPortClosed(t *testing.T) {
	swap(t, []docker.Container{{Name: "postgres18", State: docker.StateRunning}}, nil, false, &fakeEngine{}, nil)
	if status(Run(context.Background(), pgTarget()), "Port reachable") != Fail {
		t.Error("closed port should fail")
	}
}

// TestAuthFail fails auth and skips the query round-trip (no engine to reuse).
func TestAuthFail(t *testing.T) {
	authErr := &db.DBError{Kind: db.DBErrAuthFailed, Title: "Authentication failed"}
	swap(t, []docker.Container{{Name: "postgres18", State: docker.StateRunning}}, nil, true,
		&fakeEngine{connectErr: authErr}, nil)
	r := Run(context.Background(), pgTarget())
	if status(r, "Authentication") != Fail {
		t.Errorf("auth should fail: %+v", r.Checks)
	}
	if status(r, "Query round-trip") != Skip {
		t.Errorf("query should skip without a connection: %+v", r.Checks)
	}
}

// TestSQLiteSkipsDocker checks a file target: Docker/port probes skip, auth and
// query still run.
func TestSQLiteSkipsDocker(t *testing.T) {
	swap(t, nil, nil, false, &fakeEngine{}, nil)
	r := Run(context.Background(), Target{SQLite: true, Kind: db.KindSQLite, DBTarget: db.Target{Path: "/tmp/app.db"}})
	if status(r, "Docker daemon") != Skip || status(r, "Port reachable") != Skip {
		t.Errorf("SQLite should skip docker/port checks: %+v", r.Checks)
	}
	if status(r, "Authentication") != OK || status(r, "Query round-trip") != OK {
		t.Errorf("SQLite auth/query should run: %+v", r.Checks)
	}
	if !r.OK() {
		t.Error("SQLite report should be OK")
	}
}
