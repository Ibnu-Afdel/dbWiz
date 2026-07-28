package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/screens"
)

// pump feeds a message into the model and then follows the navigation and
// resize messages its commands produce, feeding them back in so navigation
// (which flows through Push/Pop commands and tea.Batch) settles synchronously.
// It deliberately ignores self-perpetuating timer commands like the cursor
// blink and spinner tick — following those would recurse forever — since the
// navigation outcome, not animation, is what these tests assert. This drives the
// real Update path without teatest's async renderer.
func pump(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(model)
	for _, out := range follow(cmd) {
		m = pump(t, m, out)
	}
	return m
}

// follow executes a command, flattens tea.BatchMsg, and keeps only the messages
// worth replaying in a synchronous test: navigation and window-size. Everything
// else (blinks, ticks) is dropped so the pump terminates.
func follow(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	var out []tea.Msg
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			out = append(out, follow(c)...)
		}
	case screens.PushMsg, screens.ReplaceMsg, screens.PopMsg, screens.QuitMsg, tea.WindowSizeMsg:
		out = append(out, msg)
	}
	return out
}

func view(m model) string { return m.View().Content }

// TestHomeNavigatesToSQLiteAndBack drives the shell end-to-end from the home
// menu: it renders the detected inventory, opens the SQLite route (a push), and
// returns with esc (a pop) — proving the screen stack, key delegation, and the
// help bar all work together without any docker/db I/O.
func TestHomeNavigatesToSQLiteAndBack(t *testing.T) {
	home := screens.NewHome([]docker.Container{
		{Name: "pg-dev", Engine: docker.EnginePostgres, State: docker.StateRunning, HostPort: 5432, Source: docker.SourceOmarchy},
		{Name: "my-old", Engine: docker.EngineMySQL, State: docker.StateStopped},
	})
	m := newModelWith(home)
	m = pump(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})

	// Home renders: menu, inventory with the omarchy badge, and the stopped
	// container's start hint.
	got := view(m)
	for _, want := range []string{"DBWiz", "Use an existing database", "omarchy", "[s] start"} {
		if !strings.Contains(got, want) {
			t.Fatalf("home view missing %q:\n%s", want, got)
		}
	}

	// Help bar reflects the home screen (start key shown because one is stopped).
	if !strings.Contains(got, "start") || !strings.Contains(got, "rescan") {
		t.Errorf("help bar missing home bindings:\n%s", got)
	}

	// Down to "Open a SQLite file…" (Existing → Manual → Scan remote → Create →
	// Set up → SQLite) then select it (a push).
	m = pump(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = pump(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = pump(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = pump(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = pump(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = pump(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := view(m); !strings.Contains(got, "Open a SQLite file") || !strings.Contains(got, "Path:") {
		t.Fatalf("expected SQLite open screen after select:\n%s", got)
	}

	// Esc pops back to the home menu.
	m = pump(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if got := view(m); !strings.Contains(got, "Use an existing database") {
		t.Fatalf("esc did not return to home:\n%s", got)
	}
}

// TestHomeStartRoutesThroughSpinner verifies pressing [s] on a stopped container
// enters the starting state (spinner + label) rather than freezing.
func TestHomeStartRoutesThroughSpinner(t *testing.T) {
	home := screens.NewHome([]docker.Container{
		{Name: "my-old", Engine: docker.EngineMySQL, State: docker.StateStopped, HostPort: 3306},
	})
	m := newModelWith(home)
	// Only feed the key: draining the start command would run real docker, so we
	// assert on the state the key sets, not the async result.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = next.(model)
	next, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = next.(model)
	if got := view(m); !strings.Contains(got, "Starting my-old") {
		t.Fatalf("expected starting state after [s]:\n%s", got)
	}
}

// TestGlobalQuit verifies ctrl+c quits from any screen (handled by the root
// before delegation).
func TestGlobalQuit(t *testing.T) {
	m := newModelWith(screens.NewHome(nil))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c produced no command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("ctrl+c command produced no message")
	}
	// tea.Quit's message is unexported; a non-nil message from the quit path is
	// the observable contract here.
}
