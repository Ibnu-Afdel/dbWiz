package screens

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/connect"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/health"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// doctorDoneMsg carries a finished health report back to the screen.
type doctorDoneMsg struct{ report health.Report }

// doctorScreen is the TUI "doctor": it runs the same connectivity self-check as
// `dbwiz health` against a container and shows each probe's result (v3 1.3). It's
// most useful exactly when connecting fails — it works on a stopped container and
// a dead daemon, reporting that rather than dead-ending.
type doctorScreen struct {
	container docker.Container
	running   bool
	report    health.Report
	done      bool
	spinner   spinner.Model
}

// NewDoctor builds the doctor for a container and starts the check on Init.
func NewDoctor(c docker.Container) Screen {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = styles.Selected
	return doctorScreen{container: c, running: true, spinner: sp}
}

func (s doctorScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, doctorCmd(s.container))
}

func (s doctorScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case doctorDoneMsg:
		s.running = false
		s.done = true
		s.report = msg.report
		return s, nil
	case spinner.TickMsg:
		if !s.running {
			return s, nil
		}
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, Keys.Back):
			return s, Pop()
		case key.Matches(msg, Keys.Retry) && !s.running:
			s.running, s.done = true, false
			return s, tea.Batch(s.spinner.Tick, doctorCmd(s.container))
		}
	}
	return s, nil
}

// doctorCmd resolves the container's connection details (the credential ladder)
// and runs the health check off the Update goroutine.
func doctorCmd(c docker.Container) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		kind, ok := connect.KindOf(c.Engine)
		if !ok {
			return doctorDoneMsg{report: health.Report{Checks: []health.Check{
				{Name: "Engine", Status: health.Fail, Detail: "DBWiz can't drive this engine"},
			}}}
		}
		target := connect.Target(ctx, c, kind)
		port := target.Port
		if port == 0 {
			port = c.HostPort
		}
		rep := health.Run(ctx, health.Target{Container: c.Name, Port: port, Kind: kind, DBTarget: target})
		return doctorDoneMsg{report: rep}
	}
}

func (s doctorScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(styles.Title.Render("Health check — " + s.container.Name))
	b.WriteString("\n")
	b.WriteString(styles.Hint.Render(rule(width)))
	b.WriteString("\n")

	if s.running {
		b.WriteString(s.spinner.View() + " " + styles.Subtitle.Render("Running checks…"))
		return styles.Screen.Render(b.String())
	}

	for _, c := range s.report.Checks {
		mark, style := doctorMark(c.Status)
		row := style.Render(mark+" "+c.Name) + "  " + styles.Hint.Render(c.Detail)
		b.WriteString(row + "\n")
	}
	b.WriteString(styles.Hint.Render(rule(width)))
	b.WriteString("\n")
	if s.report.OK() {
		b.WriteString(styles.SuccessText.Render("All checks passed — this target is reachable."))
	} else {
		b.WriteString(styles.DangerText.Render("Something's wrong — see the failed check above."))
	}
	return styles.Screen.Render(b.String())
}

// doctorMark maps a status to a glyph and the style that colors its row.
func doctorMark(st health.Status) (string, lipgloss.Style) {
	switch st {
	case health.OK:
		return "✓", styles.SuccessText
	case health.Fail:
		return "✗", styles.DangerText
	case health.Warn:
		return "!", styles.WarningText
	default: // Skip
		return "–", styles.Hint
	}
}

func (s doctorScreen) Help() []key.Binding {
	b := []key.Binding{}
	if s.done {
		b = append(b, Keys.Retry)
	}
	return append(b, Keys.Back)
}
