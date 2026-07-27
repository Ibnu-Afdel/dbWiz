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

// TestAltDigitSwitchesTab verifies alt+N jumps directly to a tab by position.
func TestAltDigitSwitchesTab(t *testing.T) {
	m, _ := dashboardTab(t)
	m = send(m, ctrlKey('t')) // now 2 tabs, active=1 (detect)

	m = send(m, tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	if m.active != 0 {
		t.Fatalf("alt+1 should focus tab 0, active=%d", m.active)
	}
	if got := view(m); !strings.Contains(got, "Navigator") {
		t.Errorf("alt+1 should show the dashboard tab, got:\n%s", got)
	}
}

// TestCloseTabReleasesConnection verifies ctrl+w tears down the active tab and
// closes the engine it owned, leaving the remaining tab focused.
func TestCloseTabReleasesConnection(t *testing.T) {
	m, closed := dashboardTab(t)
	m = send(m, ctrlKey('t'))                                // open a second (detect) tab
	m = send(m, tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}) // focus the dashboard tab

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

// TestNextTabCycles verifies ctrl+tab wraps around the open tabs.
func TestNextTabCycles(t *testing.T) {
	m, _ := dashboardTab(t)
	m = send(m, ctrlKey('t')) // 2 tabs, active=1

	m = send(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl})
	if m.active != 0 {
		t.Fatalf("ctrl+tab from tab 1 of 2 should wrap to 0, active=%d", m.active)
	}
}
