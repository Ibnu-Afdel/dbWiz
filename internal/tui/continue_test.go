package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/screens"
)

// TestHomeOffersContinueForRunningContainer verifies the home menu surfaces the
// last-used Docker target — and only when it's present and running — then routes
// straight to connecting when chosen.
func TestHomeOffersContinueForRunningContainer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := state.SetLastDocker("pg", "postgres"); err != nil {
		t.Fatal(err)
	}

	running := []docker.Container{{Name: "pg", Engine: docker.EnginePostgres, State: docker.StateRunning}}
	m := newModelWith(screens.NewHomeContinuing(running))
	m = pump(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})

	got := view(m)
	if !strings.Contains(got, "Continue where you left off") || !strings.Contains(got, "pg (postgres)") {
		t.Fatalf("home should offer the continue row:\n%s", got)
	}

	// The continue row is first, so a bare Enter resumes it → connect screen.
	m = pump(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := view(m); !strings.Contains(got, "Connecting to pg") {
		t.Fatalf("selecting continue should route to connect:\n%s", got)
	}
}

// TestHomeHidesContinueWhenTargetGone verifies a remembered container that isn't
// in the current scan (or is stopped) produces no continue row — never a dead
// link.
func TestHomeHidesContinueWhenTargetGone(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := state.SetLastDocker("pg", "postgres"); err != nil {
		t.Fatal(err)
	}

	// Only a *stopped* container by that name is present.
	stopped := []docker.Container{{Name: "pg", Engine: docker.EnginePostgres, State: docker.StateStopped}}
	m := newModelWith(screens.NewHomeContinuing(stopped))
	m = pump(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	if strings.Contains(view(m), "Continue where you left off") {
		t.Errorf("continue row should be hidden when the target isn't running:\n%s", view(m))
	}
}

// TestHomeOffersContinueForSQLiteFile verifies a remembered SQLite file that
// still exists is offered by its base name.
func TestHomeOffersContinueForSQLiteFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "dev.sqlite")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.SetLastSQLite(f); err != nil {
		t.Fatal(err)
	}

	m := newModelWith(screens.NewHomeContinuing(nil))
	m = pump(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	got := view(m)
	if !strings.Contains(got, "Continue where you left off") || !strings.Contains(got, "dev.sqlite") {
		t.Fatalf("home should offer the SQLite file to continue:\n%s", got)
	}
}
