package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/provision"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// swapSetup swaps the provision run/wait/check seams for one test. conflict, when
// non-nil, is what the pre-flight check reports (nil means "clear to proceed"),
// so the tests never touch the real network.
func swapSetup(t *testing.T, run func(context.Context, provision.Spec) (string, error), ready bool, conflict *provision.Conflict) {
	t.Helper()
	oldRun, oldWait, oldCheck := setupRun, setupWait, setupCheck
	setupRun = run
	setupWait = func(context.Context, int, time.Duration) bool { return ready }
	setupCheck = func(context.Context, provision.Spec, []docker.Container) *provision.Conflict { return conflict }
	t.Cleanup(func() { setupRun, setupWait, setupCheck = oldRun, oldWait, oldCheck })
}

// TestSetupHappyPath provisions Postgres: runs the right spec, records context,
// and prints how to connect.
func TestSetupHappyPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, nil, nil)
	var got provision.Spec
	swapSetup(t, func(_ context.Context, s provision.Spec) (string, error) {
		got = s
		return "abcdef012345678", nil
	}, true, nil)

	var out bytes.Buffer
	if err := runSetup(context.Background(), &out, "postgres", true); err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	if got.Name != "postgres18" {
		t.Errorf("ran spec %q, want postgres18", got.Name)
	}
	s := out.String()
	for _, must := range []string{"postgres18", "Ready", "postgresql://", "abcdef012345"} {
		if !strings.Contains(s, must) {
			t.Errorf("output missing %q:\n%s", must, s)
		}
	}
	last := state.Load().Last
	if last == nil || last.Container != "postgres18" {
		t.Errorf("context not pinned to the new container: %+v", last)
	}
}

// TestSetupUnknownEngine refuses an engine it can't provision, listing the ones
// it can.
func TestSetupUnknownEngine(t *testing.T) {
	var out bytes.Buffer
	err := runSetup(context.Background(), &out, "mongodb", false)
	if err == nil || !strings.Contains(err.Error(), "postgres") {
		t.Fatalf("expected an unknown-engine error listing choices, got %v", err)
	}
}

// TestSetupNameConflict stops before running when the container already exists.
func TestSetupNameConflict(t *testing.T) {
	fakeDetect(t, []docker.Container{{Name: "postgres18", State: docker.StateStopped}}, nil)
	ran := false
	swapSetup(t, func(context.Context, provision.Spec) (string, error) {
		ran = true
		return "", nil
	}, true, &provision.Conflict{Kind: provision.NameTaken, Detail: "a stopped container named \"postgres18\" already exists"})

	var out bytes.Buffer
	err := runSetup(context.Background(), &out, "postgres", false)
	if err == nil || !strings.Contains(err.Error(), "already set up") {
		t.Fatalf("expected an already-set-up error, got %v", err)
	}
	if ran {
		t.Error("must not run docker when a name conflict is detected")
	}
}

// TestSetupRunErrorFormatted surfaces a docker error's detail/hint on the CLI.
func TestSetupRunErrorFormatted(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, nil, nil)
	swapSetup(t, func(context.Context, provision.Spec) (string, error) {
		return "", &docker.DockerError{
			Kind:   docker.DockerErrSocketPermission,
			Title:  "No permission to reach Docker",
			Detail: "your user can't access the socket",
			Hint:   "add yourself to the docker group",
		}
	}, true, nil)

	var out bytes.Buffer
	err := runSetup(context.Background(), &out, "mysql", false)
	if err == nil || !strings.Contains(err.Error(), "docker group") {
		t.Fatalf("expected the docker error's hint surfaced, got %v", err)
	}
}
