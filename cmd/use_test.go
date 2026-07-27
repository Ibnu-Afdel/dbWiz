package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// fakeDetect swaps the docker scan for a fixed set of containers for one test.
func fakeDetect(t *testing.T, containers []docker.Container, err error) {
	t.Helper()
	old := detect
	detect = func(context.Context) ([]docker.Container, error) { return containers, err }
	t.Cleanup(func() { detect = old })
}

// TestUseSetsContext verifies a known container name is recorded as the last-used
// target and confirmed on stdout.
func TestUseSetsContext(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{
		{Name: "pg-dev", Engine: docker.EnginePostgres, State: docker.StateRunning},
	}, nil)

	var out bytes.Buffer
	if err := runUse(context.Background(), &out, "pg-dev"); err != nil {
		t.Fatalf("runUse: %v", err)
	}
	if !strings.Contains(out.String(), "pg-dev") {
		t.Errorf("expected confirmation naming the container, got %q", out.String())
	}

	last := state.Load().Last
	if last == nil || last.Kind != state.KindDocker || last.Container != "pg-dev" || last.Engine != "postgres" {
		t.Fatalf("context not saved correctly: %+v", last)
	}
}

// TestUseStoppedContainerWarns verifies pinning a stopped container still records
// it but warns that it needs starting.
func TestUseStoppedContainerWarns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{
		{Name: "pg-old", Engine: docker.EnginePostgres, State: docker.StateStopped},
	}, nil)

	var out bytes.Buffer
	if err := runUse(context.Background(), &out, "pg-old"); err != nil {
		t.Fatalf("runUse: %v", err)
	}
	if !strings.Contains(out.String(), "isn't running") {
		t.Errorf("expected a not-running warning, got %q", out.String())
	}
	if state.Load().Last == nil {
		t.Error("stopped container should still be recorded")
	}
}

// TestUseUnknownNameLists verifies a typo fails and the error lists the names
// that were actually detected, so it's self-correcting.
func TestUseUnknownNameLists(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{
		{Name: "pg-dev", Engine: docker.EnginePostgres, State: docker.StateRunning},
		{Name: "my-app", Engine: docker.EngineMySQL, State: docker.StateRunning},
	}, nil)

	var out bytes.Buffer
	err := runUse(context.Background(), &out, "postgres")
	if err == nil {
		t.Fatal("expected an error for an unknown container name")
	}
	for _, want := range []string{"postgres", "pg-dev", "my-app"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
	if state.Load().Last != nil {
		t.Error("an unknown name must not change the saved context")
	}
}

// TestUseNoContainers verifies a helpful message when nothing was detected.
func TestUseNoContainers(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, nil, nil)

	var out bytes.Buffer
	err := runUse(context.Background(), &out, "anything")
	if err == nil || !strings.Contains(err.Error(), "no database containers detected") {
		t.Fatalf("expected a no-containers error, got: %v", err)
	}
}
