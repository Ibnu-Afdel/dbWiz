package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// TestSetupMenuRenders lists every provisionable engine with its dev-auth.
func TestSetupMenuRenders(t *testing.T) {
	s := NewSetup(nil)
	got := s.View(100, 30)
	for _, want := range []string{"Set up a new database server", "postgres", "mysql", "mariadb", "postgis", "No sudo"} {
		if !strings.Contains(got, want) {
			t.Errorf("setup view missing %q:\n%s", want, got)
		}
	}
}

// TestSetupSelectStartsWorking pressing enter enters the working state (spinner)
// rather than blocking; we don't drain the command (it would run real docker).
func TestSetupSelectStartsWorking(t *testing.T) {
	scr, cmd := NewSetup(nil).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	s := scr.(setupScreen)
	if !s.working {
		t.Error("enter should enter the working state")
	}
	if cmd == nil {
		t.Error("enter should kick off the provision command")
	}
}

// TestSetupConflictExplained a pre-flight conflict shows inline and leaves the
// working state, so the user can pick another engine.
func TestSetupConflictExplained(t *testing.T) {
	s := NewSetup(nil).(setupScreen)
	s.working = true
	scr, _ := s.Update(setupConflictMsg{detail: "port 5432 is busy"})
	got := scr.(setupScreen)
	if got.working {
		t.Error("a conflict should end the working state")
	}
	if !strings.Contains(got.View(100, 30), "port 5432 is busy") {
		t.Errorf("conflict detail not shown:\n%s", got.View(100, 30))
	}
}

// TestSetupDoneRescans a successful provision pins context and replaces with a
// fresh detect so the new container appears on home.
func TestSetupDoneRescans(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, cmd := NewSetup(nil).Update(setupDoneMsg{name: "postgres18", engine: "postgres"})
	msg := runCmd(t, cmd)
	rep, ok := msg.(ReplaceMsg)
	if !ok {
		t.Fatalf("want ReplaceMsg, got %T", msg)
	}
	if _, ok := rep.Screen.(detectScreen); !ok {
		t.Errorf("want a detect rescan, got %T", rep.Screen)
	}
}

// TestSetupErrorRoutesToErrorScreen a docker failure routes to the error screen.
func TestSetupErrorRoutesToErrorScreen(t *testing.T) {
	_, cmd := NewSetup(nil).Update(setupErrMsg{err: &docker.DockerError{Title: "boom"}})
	msg := runCmd(t, cmd)
	rep, ok := msg.(ReplaceMsg)
	if !ok {
		t.Fatalf("want ReplaceMsg, got %T", msg)
	}
	if _, ok := rep.Screen.(errorScreen); !ok {
		t.Errorf("want errorScreen, got %T", rep.Screen)
	}
}
