package screens

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// runCmd executes a command and returns the message it produced, failing if the
// command is nil.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil command")
	}
	return cmd()
}

func running(name string) docker.Container {
	return docker.Container{Name: name, Engine: docker.EnginePostgres, State: docker.StateRunning, HostPort: 5432}
}

func stopped(name string) docker.Container {
	return docker.Container{Name: name, Engine: docker.EnginePostgres, State: docker.StateStopped}
}

// TestExistingRoute checks the "use existing" branch picks the right next screen
// by running-container count: none → error, one → connect, 2+ → picker.
func TestExistingRoute(t *testing.T) {
	// None running.
	msg := runCmd(t, existingRoute([]docker.Container{stopped("pg")}))
	push, ok := msg.(PushMsg)
	if !ok {
		t.Fatalf("want PushMsg, got %T", msg)
	}
	if _, ok := push.Screen.(errorScreen); !ok {
		t.Errorf("none running: want errorScreen, got %T", push.Screen)
	}

	// Exactly one running → connect (auto-skip picker).
	msg = runCmd(t, existingRoute([]docker.Container{running("pg"), stopped("old")}))
	push = msg.(PushMsg)
	if _, ok := push.Screen.(connectScreen); !ok {
		t.Errorf("one running: want connectScreen, got %T", push.Screen)
	}

	// Two running → picker.
	msg = runCmd(t, existingRoute([]docker.Container{running("pg"), running("my")}))
	push = msg.(PushMsg)
	if _, ok := push.Screen.(pickerScreen); !ok {
		t.Errorf("two running: want pickerScreen, got %T", push.Screen)
	}
}

// TestHomeInventoryHelpers covers the small container-slice helpers the home
// screen uses to decide what to show and start.
func TestHomeInventoryHelpers(t *testing.T) {
	cs := []docker.Container{running("a"), stopped("b"), running("c")}
	if n := countRunning(cs); n != 2 {
		t.Errorf("countRunning = %d, want 2", n)
	}
	if got, ok := firstStopped(cs); !ok || got.Name != "b" {
		t.Errorf("firstStopped = (%v,%v), want (b,true)", got.Name, ok)
	}
	if _, ok := firstStopped([]docker.Container{running("a")}); ok {
		t.Error("firstStopped found a stopped container where there is none")
	}
}

// TestErrorScreenBackAndRetry verifies the error screen never dead-ends: b/esc
// pop, and r fires the retry command when one is configured.
func TestErrorScreenBackAndRetry(t *testing.T) {
	retried := false
	s := errorScreen{title: "boom", retry: retrySpec{label: "retry", cmd: func() tea.Msg {
		retried = true
		return nil
	}}}

	// Back pops.
	_, cmd := s.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if _, ok := runCmd(t, cmd).(PopMsg); !ok {
		t.Error("b should pop")
	}

	// Retry fires the configured command.
	_, cmd = s.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	runCmd(t, cmd)
	if !retried {
		t.Error("r should fire the retry command")
	}
}

// TestEmptyStateOffersSQLite verifies the no-containers empty state is never a
// dead end: it exposes an [o] escape hatch that opens the SQLite path input, and
// advertises it in the help bar.
func TestEmptyStateOffersSQLite(t *testing.T) {
	s, ok := emptyStateError().(errorScreen)
	if !ok {
		t.Fatalf("emptyStateError should be an errorScreen")
	}

	// The [o] alt action opens the SQLite screen.
	_, cmd := s.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	msg := runCmd(t, cmd)
	push, ok := msg.(PushMsg)
	if !ok {
		t.Fatalf("o should push a screen, got %T", msg)
	}
	if _, ok := push.Screen.(sqliteOpenScreen); !ok {
		t.Errorf("o should open the SQLite screen, got %T", push.Screen)
	}

	// It's advertised in the help bar alongside back and rescan.
	var hasO bool
	for _, b := range s.Help() {
		for _, k := range b.Keys() {
			if k == "o" {
				hasO = true
			}
		}
	}
	if !hasO {
		t.Error("help bar should advertise the [o] SQLite escape hatch")
	}
}

// TestErrorScreenInfoToggle verifies [i] only toggles when there's underlying
// detail to show.
func TestErrorScreenInfoToggle(t *testing.T) {
	s := Screen(errorScreen{title: "boom", info: "stack details"})
	s, _ = s.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if !s.(errorScreen).showInfo {
		t.Error("i should reveal info when present")
	}

	noInfo := Screen(errorScreen{title: "boom"})
	noInfo, _ = noInfo.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if noInfo.(errorScreen).showInfo {
		t.Error("i should be a no-op with no info")
	}
}
