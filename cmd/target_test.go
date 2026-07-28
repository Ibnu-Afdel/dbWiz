package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// full is a container with every credential already recovered, so resolveTarget
// never needs to shell out to `docker inspect` to fill a gap — keeping these
// tests hermetic.
func full(name string, eng docker.Engine) docker.Container {
	return docker.Container{
		Name: name, Engine: eng, State: docker.StateRunning, HostPort: 5432,
		Creds: docker.Creds{User: "u", Password: "p", Database: "d"},
	}
}

// TestResolveExplicitTarget: --target names a container directly.
func TestResolveExplicitTarget(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{full("pg-dev", docker.EnginePostgres)}, nil)

	got, err := resolveTarget(context.Background(), "pg-dev")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if got.sqlite || got.container.Name != "pg-dev" || got.kind != db.KindPostgres {
		t.Fatalf("unexpected target: %+v", got)
	}
}

// TestResolveUnknownTargetLists: an explicit typo self-corrects with the names.
func TestResolveUnknownTargetLists(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{full("pg-dev", docker.EnginePostgres)}, nil)

	_, err := resolveTarget(context.Background(), "nope")
	if err == nil || !strings.Contains(err.Error(), "pg-dev") {
		t.Fatalf("expected a self-correcting error listing pg-dev, got %v", err)
	}
}

// TestResolveLastDocker: with no flag, the container pinned by `use` is re-resolved.
func TestResolveLastDocker(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := state.SetLastDocker("my-app", "mysql"); err != nil {
		t.Fatal(err)
	}
	fakeDetect(t, []docker.Container{
		full("pg-dev", docker.EnginePostgres),
		full("my-app", docker.EngineMySQL),
	}, nil)

	got, err := resolveTarget(context.Background(), "")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if got.container.Name != "my-app" || got.kind != db.KindMySQL {
		t.Fatalf("expected the pinned container, got %+v", got)
	}
}

// TestResolveLastSQLite: a pinned SQLite file resolves without touching Docker.
func TestResolveLastSQLite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := state.SetLastSQLite("/tmp/app.sqlite"); err != nil {
		t.Fatal(err)
	}
	// detect must not even be consulted for a SQLite target.
	fakeDetect(t, nil, context.DeadlineExceeded)

	got, err := resolveTarget(context.Background(), "")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if !got.sqlite || got.kind != db.KindSQLite || got.target.Path != "/tmp/app.sqlite" {
		t.Fatalf("unexpected SQLite target: %+v", got)
	}
}

// TestResolveSoleContainer: no flag, no pin, exactly one detected → use it.
func TestResolveSoleContainer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{full("only", docker.EnginePostgres)}, nil)

	got, err := resolveTarget(context.Background(), "")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if got.container.Name != "only" {
		t.Fatalf("expected the sole container, got %+v", got)
	}
}

// TestResolveAmbiguous: no flag, no pin, several detected → refuse and list them.
func TestResolveAmbiguous(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{
		full("a", docker.EnginePostgres),
		full("b", docker.EngineMySQL),
	}, nil)

	_, err := resolveTarget(context.Background(), "")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	for _, want := range []string{"a", "b", "dbwiz use"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// TestResolveNoTarget: nothing detected and nothing pinned → a clear error.
func TestResolveNoTarget(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, nil, nil)

	_, err := resolveTarget(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "no target") {
		t.Fatalf("expected a no-target error, got %v", err)
	}
}

// TestResolveStoppedContainer: a resolved container that isn't running is a clear
// error, not a later cryptic connection failure.
func TestResolveStoppedContainer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	c := full("stopped", docker.EnginePostgres)
	c.State = docker.StateStopped
	fakeDetect(t, []docker.Container{c}, nil)

	_, err := resolveTarget(context.Background(), "stopped")
	if err == nil || !strings.Contains(err.Error(), "isn't running") {
		t.Fatalf("expected a not-running error, got %v", err)
	}
}
