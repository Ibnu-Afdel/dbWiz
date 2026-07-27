package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/screens"
)

// closableEngine wraps the shared fake so a test can observe that closing a tab
// releases its connection. Close flips the pointed-at flag.
type closableEngine struct {
	*teatestEngine
	closed *bool
}

func (e closableEngine) Close() error { *e.closed = true; return nil }

// dashboardTab builds a model seeded with a live dashboard tab whose engine
// records when it is closed.
func dashboardTab(t *testing.T) (model, *bool) {
	t.Helper()
	closed := new(bool)
	eng := closableEngine{teatestEngine: &teatestEngine{caps: db.Capabilities{MultipleDatabases: true}}, closed: closed}
	target := db.Target{Host: "127.0.0.1", Port: 5432, User: "postgres", Database: "appdb"}
	m := newModelWith(screens.NewDashboard(eng, target, docker.Container{Name: "pg"}))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(model), closed
}

// ctrlKey builds a ctrl+<r> key press the way the root matches its tab bindings.
func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// send feeds a message into the root and returns the updated model, discarding
// the command (async work is not what these synchronous tab tests assert).
func send(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

// TestNewTabOpensAndShowsBar verifies ctrl+t adds a second tab, switches to it
// (a fresh detection screen), and reveals the tab bar with both tabs numbered.
func TestNewTabOpensAndShowsBar(t *testing.T) {
	m, _ := dashboardTab(t)
	if len(m.tabs) != 1 {
		t.Fatalf("expected 1 tab to start, got %d", len(m.tabs))
	}

	next, _ := m.Update(ctrlKey('t'))
	m = next.(model)

	if len(m.tabs) != 2 {
		t.Fatalf("ctrl+t should add a tab, got %d", len(m.tabs))
	}
	if m.active != 1 {
		t.Fatalf("ctrl+t should switch to the new tab, active=%d", m.active)
	}
	got := view(m)
	// Tab bar shows both tabs: the dashboard's target name and the new tab.
	for _, want := range []string{"1 pg", "2 new"} {
		if !strings.Contains(got, want) {
			t.Errorf("tab bar missing %q:\n%s", want, got)
		}
	}
}

// TestSingleTabHasNoTabBar verifies the tab bar stays hidden with one tab, so the
// single-target experience is exactly v1's.
func TestSingleTabHasNoTabBar(t *testing.T) {
	m, _ := dashboardTab(t)
	if strings.Contains(view(m), "1 pg") {
		t.Errorf("a single tab should not render the numbered tab bar:\n%s", view(m))
	}
}

// TestDigitSwitchesTab verifies a bare number key jumps directly to a tab by
// position — the tmux-friendly switch (no modifier, no extended-keys protocol).
func TestDigitSwitchesTab(t *testing.T) {
	m, _ := dashboardTab(t)
	m = send(m, ctrlKey('t')) // now 2 tabs, active=1 (detect)

	m = send(m, tea.KeyPressMsg{Code: '1', Text: "1"})
	if m.active != 0 {
		t.Fatalf("pressing 1 should focus tab 0, active=%d", m.active)
	}
	if got := view(m); !strings.Contains(got, "Navigator") {
		t.Errorf("pressing 1 should show the dashboard tab, got:\n%s", got)
	}
}

// TestDigitIgnoredWithSingleTab verifies a digit is left alone when only one tab
// is open, so the single-target experience never steals number keys from a
// screen.
func TestDigitIgnoredWithSingleTab(t *testing.T) {
	m, _ := dashboardTab(t)
	_, _, handled := m.handleGlobalKey(tea.KeyPressMsg{Code: '1', Text: "1"})
	if handled {
		t.Error("with one tab open, a digit should pass through to the screen")
	}
}

// TestDigitTypesIntoEditor verifies that when the SQL editor has focus, a digit
// is left for the editor rather than switching tabs — even with several tabs
// open.
func TestDigitTypesIntoEditor(t *testing.T) {
	m, _ := dashboardTab(t)
	m = send(m, ctrlKey('t'))                          // 2 tabs, active=1 (detect)
	m = send(m, tea.KeyPressMsg{Code: '1', Text: "1"}) // back to the dashboard tab
	m = send(m, tea.KeyPressMsg{Code: 'e', Text: "e"}) // focus the SQL editor

	if !m.capturingText() {
		t.Fatal("editor focus should report capturing text")
	}
	_, _, handled := m.handleGlobalKey(tea.KeyPressMsg{Code: '2', Text: "2"})
	if handled {
		t.Error("a digit typed into the focused editor must not switch tabs")
	}
}

// TestCloseTabReleasesConnection verifies ctrl+w tears down the active tab and
// closes the engine it owned, leaving the remaining tab focused.
func TestCloseTabReleasesConnection(t *testing.T) {
	m, closed := dashboardTab(t)
	m = send(m, ctrlKey('t'))                          // open a second (detect) tab
	m = send(m, tea.KeyPressMsg{Code: '1', Text: "1"}) // focus the dashboard tab

	m = send(m, ctrlKey('w'))

	if !*closed {
		t.Error("closing the dashboard tab should have closed its engine")
	}
	if len(m.tabs) != 1 {
		t.Fatalf("ctrl+w should leave 1 tab, got %d", len(m.tabs))
	}
	if got := view(m); !strings.Contains(got, "Scanning") {
		t.Errorf("after closing the dashboard tab, the detect tab should be active:\n%s", got)
	}
}

// TestCloseLastTabQuits verifies ctrl+w on the only tab quits the app rather than
// leaving an empty window.
func TestCloseLastTabQuits(t *testing.T) {
	m := newModelWith(screens.NewHome(nil))
	_, cmd := m.Update(ctrlKey('w'))
	if cmd == nil {
		t.Fatal("ctrl+w on the last tab should quit, got nil command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+w on the last tab should produce tea.QuitMsg, got %T", cmd())
	}
}

// TestOutOfRangeDigitIsSwallowed verifies a tab number with no matching tab is
// consumed (not passed to the screen) rather than doing something surprising.
func TestOutOfRangeDigitIsSwallowed(t *testing.T) {
	m, _ := dashboardTab(t)
	m = send(m, ctrlKey('t')) // 2 tabs

	before := m.active
	_, _, handled := m.handleGlobalKey(tea.KeyPressMsg{Code: '9', Text: "9"})
	if !handled {
		t.Error("an out-of-range tab digit should be swallowed")
	}
	if m.active != before {
		t.Errorf("out-of-range digit should not change the active tab")
	}
}
