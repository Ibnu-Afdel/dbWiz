package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/screens"
)

// TestRootModelStartsOnDetect verifies the app enters on the detection screen:
// its scanning message renders once the model knows the terminal size. This is
// checked synchronously — without running Init, so no docker subprocess is
// spawned and the test is deterministic regardless of the host's containers.
func TestRootModelStartsOnDetect(t *testing.T) {
	m := newModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := next.(model).View().Content
	if !strings.Contains(got, "Scanning") {
		t.Fatalf("expected the detect screen to render, got:\n%s", got)
	}
}

// representativeScreens seeds one screen at each stage of the route map. The
// engine-backed dashboard uses the shared teatest fake so no server is needed.
func representativeScreens() map[string]screens.Screen {
	eng := &teatestEngine{caps: db.Capabilities{Users: true, Grants: true, MultipleDatabases: true}}
	target := db.Target{Host: "127.0.0.1", Port: 5432, User: "postgres", Database: "appdb"}
	return map[string]screens.Screen{
		"detect":      screens.NewDetect(),
		"home":        screens.NewHome([]docker.Container{{Name: "pg", Engine: docker.EnginePostgres, State: docker.StateRunning}}),
		"sqlite_open": screens.NewSQLiteOpen(),
		"picker": screens.NewPicker([]docker.Container{
			{Name: "a", Engine: docker.EnginePostgres, State: docker.StateRunning},
			{Name: "b", Engine: docker.EngineMySQL, State: docker.StateRunning},
		}),
		"dashboard": screens.NewDashboard(eng, target, docker.Container{Name: "pg"}),
	}
}

// TestCtrlCQuitsFromEveryScreen is the anti-dead-end invariant (Phase 8.5): the
// root handles the global quit key before delegating, so ctrl+c ends the program
// from any screen — even a transient one, or one busy enough that its help bar
// truncates the exit hints. This is the guarantee that makes the app impossible
// to get stuck in, independent of terminal width.
func TestCtrlCQuitsFromEveryScreen(t *testing.T) {
	for name, start := range representativeScreens() {
		m := newModelWith(start)
		m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		_, cmd := m2.(model).Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Errorf("%s: ctrl+c should quit, got nil command", name)
			continue
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s: ctrl+c should produce tea.QuitMsg", name)
		}
	}
}

// TestHelpBarShowsQuitWhenItFits verifies the root appends the global help/quit
// keys to the help bar: on a screen whose own bindings leave room, quit is
// visible at a glance (busier screens elide it behind the "…"/? full-help, but
// ctrl+c still works — see TestCtrlCQuitsFromEveryScreen).
func TestHelpBarShowsQuitWhenItFits(t *testing.T) {
	m := newModelWith(screens.NewSQLiteOpen())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got := next.(model).View().Content
	if !strings.Contains(got, "quit") {
		t.Errorf("sqlite_open help bar should advertise quit, got:\n%s", got)
	}
}
