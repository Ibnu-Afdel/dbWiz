package screens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/connect"
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/remote"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// remoteScanTimeout bounds a remote docker command (ps/inspect) plus its SSH
// handshake, so an unreachable server surfaces as an error instead of hanging.
const remoteScanTimeout = 30 * time.Second

// remoteScanPhase is where the remote-scan screen is: entering the SSH host,
// scanning it, or showing the detected containers.
type remoteScanPhase int

const (
	scanInput remoteScanPhase = iota
	scanScanning
	scanResults
)

// remoteScanScreen runs the v1 detection story on a remote server (v3 3.3): the
// user names an SSH host, DBWiz runs `docker ps` there over SSH (DOCKER_HOST=
// ssh://…), lists the database containers, and connects to a chosen one through
// an SSH tunnel to its published port — reusing the manual-connect path, so the
// dashboard opens with the REMOTE rails.
type remoteScanScreen struct {
	phase      remoteScanPhase
	input      textinput.Model
	spec       remote.SSHSpec
	spinner    spinner.Model
	err        string
	containers []docker.Container
	cursor     int
}

// NewRemoteScan builds the SSH-host entry screen.
func NewRemoteScan() Screen {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "user@host or user@host:port"
	in.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = styles.Selected
	return remoteScanScreen{phase: scanInput, input: in, spinner: sp}
}

func (s remoteScanScreen) Init() tea.Cmd { return textinput.Blink }

// CapturesText is true while the SSH host is being typed, so a digit in a port
// isn't stolen as a tab switch. Satisfies screens.TextInputer.
func (s remoteScanScreen) CapturesText() bool { return s.phase == scanInput }

// remoteDetectedMsg carries the containers a remote scan found, tagged with the
// host they came from (needed to tunnel to a pick). remoteScanErrMsg is a failure
// of the scan or a connect prepare, shown inline.
type (
	remoteDetectedMsg struct {
		spec       remote.SSHSpec
		containers []docker.Container
	}
	remoteScanErrMsg struct{ err error }
)

func (s remoteScanScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case remoteDetectedMsg:
		s.phase = scanResults
		s.spec = msg.spec
		s.containers = msg.containers
		s.cursor = 0
		return s, nil
	case remoteScanErrMsg:
		// Return to the input so the user can fix the host and retry.
		s.phase = scanInput
		s.err = msg.err.Error()
		return s, s.input.Focus()
	case spinner.TickMsg:
		if s.phase != scanScanning {
			return s, nil
		}
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd
	case tea.KeyPressMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s remoteScanScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch s.phase {
	case scanInput:
		switch msg.String() {
		case "esc":
			return s, Pop()
		case "enter":
			spec, err := remote.ParseSSH(s.input.Value())
			if err != nil {
				s.err = err.Error()
				return s, nil
			}
			s.spec, s.err = spec, ""
			s.phase = scanScanning
			return s, tea.Batch(s.spinner.Tick, detectRemoteCmd(spec))
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return s, cmd
	case scanScanning:
		return s, nil // input ignored while the scan is in flight
	case scanResults:
		switch {
		case key.Matches(msg, Keys.Back):
			// Back to the input to scan a different host.
			s.phase = scanInput
			return s, s.input.Focus()
		case key.Matches(msg, Keys.Up):
			if s.cursor > 0 {
				s.cursor--
			}
			return s, nil
		case key.Matches(msg, Keys.Down):
			if s.cursor < len(s.containers)-1 {
				s.cursor++
			}
			return s, nil
		case key.Matches(msg, Keys.Select):
			return s.connectSelected()
		}
	}
	return s, nil
}

// connectSelected connects to the highlighted container. Only a running one with
// a published host port is reachable through a tunnel; otherwise it explains why.
func (s remoteScanScreen) connectSelected() (Screen, tea.Cmd) {
	if s.cursor >= len(s.containers) {
		return s, nil
	}
	c := s.containers[s.cursor]
	if c.State != docker.StateRunning {
		s.err = c.Name + " is stopped — start it on the server first."
		return s, nil
	}
	if c.HostPort == 0 {
		s.err = c.Name + " publishes no host port, so there's nothing to tunnel to."
		return s, nil
	}
	return s, connectRemoteCmd(s.spec, c)
}

// detectRemoteCmd runs `docker ps` on the remote host over SSH and returns the
// database containers it finds.
func detectRemoteCmd(spec remote.SSHSpec) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), remoteScanTimeout)
		defer cancel()
		cs, err := docker.DetectRemote(ctx, spec.DockerHost())
		if err != nil {
			return remoteScanErrMsg{err: err}
		}
		return remoteDetectedMsg{spec: spec, containers: cs}
	}
}

// connectRemoteCmd recovers a remote container's port and credentials over SSH,
// then hands off to the manual-connect path with the SSH spec attached — so the
// connection is tunnelled to the container's published port and lands on the
// dashboard with the REMOTE rails. Credential recovery is best-effort: on failure
// the engine defaults apply and the password prompt covers the rest.
func connectRemoteCmd(spec remote.SSHSpec, c docker.Container) tea.Cmd {
	return func() tea.Msg {
		kind, ok := connect.KindOf(c.Engine)
		if !ok {
			return remoteScanErrMsg{err: errors.New("unsupported engine for " + c.Name)}
		}
		port := c.HostPort
		var creds docker.Creds
		ctx, cancel := context.WithTimeout(context.Background(), remoteScanTimeout)
		defer cancel()
		if p, ic, err := docker.InspectRemote(ctx, spec.DockerHost(), c.Name, c.Engine); err == nil {
			if port == 0 {
				port = p
			}
			creds = ic
		}
		// The container's published port lives on the remote host's loopback; the
		// tunnel reaches it as 127.0.0.1:port over SSH.
		base := db.Target{Host: "127.0.0.1", Port: port, User: creds.User, Password: creds.Password, Database: creds.Database}
		connect.ApplyEngineDefaults(&base, kind)
		specCopy := spec
		return PushMsg{Screen: NewConnectManualEntry(c.Name, kind, base, &specCopy)}
	}
}

func (s remoteScanScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(styles.Title.Render("Scan a remote host over SSH"))
	b.WriteString("\n")

	switch s.phase {
	case scanInput:
		b.WriteString(styles.Subtitle.Render("Runs docker ps on the server over SSH and lists its database containers."))
		b.WriteString("\n")
		b.WriteString(styles.Hint.Render(rule(width)))
		b.WriteString("\n\n")
		b.WriteString(styles.Subtitle.Render("SSH host"))
		b.WriteString("\n")
		b.WriteString(s.input.View())
		b.WriteString("\n\n")
		b.WriteString(styles.Hint.Render("enter to scan · esc to go back"))
		if s.err != "" {
			b.WriteString("\n\n" + styles.DangerText.Render("✗ "+s.err))
		}
	case scanScanning:
		b.WriteString("\n")
		b.WriteString(s.spinner.View() + " " + styles.Subtitle.Render("Scanning "+s.spec.String()+" over SSH…"))
	case scanResults:
		b.WriteString(styles.Subtitle.Render(s.spec.String()))
		b.WriteString("\n")
		b.WriteString(styles.Hint.Render(rule(width)))
		b.WriteString("\n")
		b.WriteString(s.resultsBody())
		if s.err != "" {
			b.WriteString("\n" + styles.DangerText.Render("✗ "+s.err))
		}
	}
	return styles.Screen.Render(b.String())
}

// resultsBody lists the detected containers, running ones selectable.
func (s remoteScanScreen) resultsBody() string {
	if len(s.containers) == 0 {
		return styles.Hint.Render("No database containers found on this host.")
	}
	var lines []string
	for i, c := range s.containers {
		cursor := "  "
		if i == s.cursor {
			cursor = styles.Selected.Render("▸ ")
		}
		meta := fmt.Sprintf("%s · %s", c.Engine, portLabel(c.HostPort))
		var row string
		if c.State == docker.StateRunning {
			row = styles.Running.Render("● "+c.Name) + styles.Hint.Render("  "+meta)
		} else {
			row = styles.Stopped.Render("○ "+c.Name) + styles.Hint.Render("  "+meta+"  (stopped)")
		}
		lines = append(lines, cursor+row)
	}
	lines = append(lines, "", styles.Hint.Render("enter connect (tunnels over SSH) · esc scan another host"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (s remoteScanScreen) Help() []key.Binding {
	if s.phase == scanResults {
		return []key.Binding{Keys.Up, Keys.Down, Keys.Select, Keys.Back}
	}
	return []key.Binding{Keys.Select, Keys.Back}
}
