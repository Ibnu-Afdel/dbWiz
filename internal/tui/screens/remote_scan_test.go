package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/remote"
)

func typeScan(s remoteScanScreen, text string) remoteScanScreen {
	for _, r := range text {
		scr, _ := s.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		s = scr.(remoteScanScreen)
	}
	return s
}

// TestRemoteScanRenders shows the SSH host input.
func TestRemoteScanRenders(t *testing.T) {
	got := NewRemoteScan().View(100, 30)
	for _, want := range []string{"Scan a remote host over SSH", "SSH host", "over SSH"} {
		if !strings.Contains(got, want) {
			t.Errorf("remote scan view missing %q:\n%s", want, got)
		}
	}
}

// TestRemoteScanInvalidHost a malformed SSH host stays on the input with an error.
func TestRemoteScanInvalidHost(t *testing.T) {
	s := typeScan(NewRemoteScan().(remoteScanScreen), "deploy@host:99999")
	scr, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	rs := scr.(remoteScanScreen)
	if rs.phase != scanInput || rs.err == "" {
		t.Fatalf("bad host should stay on input with an error; phase=%d err=%q", rs.phase, rs.err)
	}
	if cmd != nil {
		t.Error("no scan should start for a bad host")
	}
}

// TestRemoteScanStartsScan a valid host kicks off the scan (spinner phase).
func TestRemoteScanStartsScan(t *testing.T) {
	s := typeScan(NewRemoteScan().(remoteScanScreen), "deploy@1.2.3.4")
	scr, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if scr.(remoteScanScreen).phase != scanScanning {
		t.Fatalf("valid host should enter scanning phase")
	}
	if cmd == nil {
		t.Error("scanning should kick off the detect command")
	}
}

// TestRemoteScanResultsAndConnect detected containers list; enter on a running
// one connects, a stopped one explains why it can't.
func TestRemoteScanResultsAndConnect(t *testing.T) {
	s := NewRemoteScan().(remoteScanScreen)
	scr, _ := s.Update(remoteDetectedMsg{
		spec: remote.SSHSpec{User: "deploy", Host: "1.2.3.4", Port: 22},
		containers: []docker.Container{
			{Name: "pg", Engine: docker.EnginePostgres, State: docker.StateRunning, HostPort: 5432},
			{Name: "my-old", Engine: docker.EngineMySQL, State: docker.StateStopped},
		},
	})
	rs := scr.(remoteScanScreen)
	if rs.phase != scanResults {
		t.Fatalf("detected should show results")
	}
	if got := rs.View(120, 30); !strings.Contains(got, "pg") || !strings.Contains(got, "my-old") {
		t.Errorf("results should list containers:\n%s", got)
	}

	// Running container (cursor 0): enter returns a connect command.
	if _, cmd := rs.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Error("enter on a running container should start a connect")
	}

	// Move to the stopped one: enter explains, no connect.
	down, _ := rs.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	rs = down.(remoteScanScreen)
	stopped, cmd := rs.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("a stopped container can't be connected")
	}
	if !strings.Contains(stopped.(remoteScanScreen).err, "stopped") {
		t.Errorf("stopped container should explain why; err=%q", stopped.(remoteScanScreen).err)
	}
}

// TestRemoteScanErrorReturnsToInput a scan failure goes back to the input with the
// message, so the user can fix the host and retry.
func TestRemoteScanErrorReturnsToInput(t *testing.T) {
	s := NewRemoteScan().(remoteScanScreen)
	s.phase = scanScanning
	scr, _ := s.Update(remoteScanErrMsg{err: errTest("host unreachable")})
	rs := scr.(remoteScanScreen)
	if rs.phase != scanInput || !strings.Contains(rs.err, "unreachable") {
		t.Fatalf("scan error should return to input with the message; phase=%d err=%q", rs.phase, rs.err)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
