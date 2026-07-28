package screens

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/provision"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// setupReadyTimeout bounds the wait for a freshly provisioned container to
// accept connections before the screen rescans anyway.
const setupReadyTimeout = 60 * time.Second

// Setup-screen messages. A conflict is reported (not run) so the user can pick
// another engine or resolve the clash; a success triggers a rescan so the new
// container shows up on the home menu.
type (
	setupDoneMsg     struct{ name, engine string }
	setupConflictMsg struct{ detail string }
	setupErrMsg      struct{ err *docker.DockerError }
)

// setupScreen provisions a new database server container the Omarchy-compatible
// way (v3 1.1): pick an engine, DBWiz pulls + runs it with dev-auth, waits for it
// to come up, then rescans so it lands on the home menu ready to connect. It
// pre-checks each engine against the current scan so a name/port clash is
// explained before anything runs.
type setupScreen struct {
	specs      []provision.Spec
	containers []docker.Container // the latest scan, for the conflict pre-check
	cursor     int

	working bool // a provision is in flight
	spinner spinner.Model
	notice  string // an inline conflict/hint line under the list
	err     string // an inline non-fatal error line
}

// NewSetup builds the provisioning menu from the current scan (used for the
// name/port conflict pre-check).
func NewSetup(containers []docker.Container) Screen {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = styles.Selected
	return setupScreen{specs: provision.Specs(), containers: containers, spinner: sp}
}

func (s setupScreen) Init() tea.Cmd { return nil }

func (s setupScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case setupDoneMsg:
		// Pin it as the next context and rescan so the home menu shows it running.
		_ = state.SetLastDocker(msg.name, msg.engine)
		return s, Replace(NewDetect())
	case setupConflictMsg:
		s.working = false
		s.notice = msg.detail
		return s, nil
	case setupErrMsg:
		return s, Replace(NewErrorFromDocker(msg.err, retrySpec{label: "back", cmd: Pop()}))
	case spinner.TickMsg:
		if !s.working {
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

func (s setupScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if s.working {
		return s, nil // ignore input while a provision is in flight
	}
	switch {
	case key.Matches(msg, Keys.Back):
		return s, Pop()
	case key.Matches(msg, Keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
		s.notice, s.err = "", ""
		return s, nil
	case key.Matches(msg, Keys.Down):
		if s.cursor < len(s.specs)-1 {
			s.cursor++
		}
		s.notice, s.err = "", ""
		return s, nil
	case key.Matches(msg, Keys.Select):
		return s.provision()
	}
	return s, nil
}

// provision starts creating the highlighted engine's container.
func (s setupScreen) provision() (Screen, tea.Cmd) {
	s.working = true
	s.notice, s.err = "", ""
	return s, tea.Batch(s.spinner.Tick, setupCmd(s.specs[s.cursor], s.containers))
}

// setupCmd runs the whole provision off the Update goroutine: the conflict
// pre-check, then the pull+run, then the readiness wait. A conflict short-circuits
// before anything is created, so "explain before running" holds.
func setupCmd(spec provision.Spec, containers []docker.Container) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if c := provision.Check(ctx, spec, containers); c != nil {
			return setupConflictMsg{detail: setupConflictText(spec, c)}
		}
		if _, err := provision.Run(ctx, spec); err != nil {
			return setupErrMsg{err: asDockerError(err)}
		}
		provision.WaitReady(ctx, spec.HostPort, setupReadyTimeout)
		return setupDoneMsg{name: spec.Name, engine: string(spec.Engine)}
	}
}

// setupConflictText renders a pre-flight conflict as a one-line explanation.
func setupConflictText(spec provision.Spec, c *provision.Conflict) string {
	switch c.Kind {
	case provision.NameTaken:
		return fmt.Sprintf("%s is already set up (%s) — start it from the home menu instead.", spec.Key, c.Detail)
	case provision.PortTaken:
		return fmt.Sprintf("Port %d is busy: %s. Stop it first (MySQL/MariaDB share 3306; Postgres/PostGIS share 5432).", spec.HostPort, c.Detail)
	}
	return c.Detail
}

func (s setupScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(styles.Title.Render("Set up a new database server"))
	b.WriteString("\n")
	b.WriteString(styles.Subtitle.Render("Omarchy-compatible: same names, ports, and dev auth. No sudo."))
	b.WriteString("\n")
	b.WriteString(styles.Hint.Render(rule(width)))
	b.WriteString("\n")

	labelW := 0
	for _, sp := range s.specs {
		if w := lipgloss.Width(setupLabel(sp)); w > labelW {
			labelW = w
		}
	}
	labelW += 2 // gap before the auth detail

	for i, sp := range s.specs {
		cursor := "  "
		label := styles.Item.Render(setupLabel(sp))
		if i == s.cursor {
			cursor = styles.Selected.Render("▸ ")
			label = styles.Selected.Render(setupLabel(sp))
		}
		detail := styles.Hint.Render(sp.Auth)
		b.WriteString(cursor + padRight(label, labelW) + detail + "\n")
	}

	b.WriteString(styles.Hint.Render(rule(width)))
	if s.notice != "" {
		b.WriteString("\n" + styles.WarningText.Render(s.notice))
	}
	if s.err != "" {
		b.WriteString("\n" + styles.DangerText.Render(s.err))
	}
	if s.working {
		b.WriteString("\n\n" + s.spinner.View() + " " +
			styles.Subtitle.Render("Pulling and starting "+s.specs[s.cursor].Name+"… (first pull can take a minute)"))
	}
	return styles.Screen.Render(b.String())
}

// setupLabel is the menu row for a spec: the engine keyword and the container
// name/image it will create.
func setupLabel(sp provision.Spec) string {
	return fmt.Sprintf("%s — %s", sp.Key, sp.Image)
}

func (s setupScreen) Help() []key.Binding {
	return []key.Binding{Keys.Up, Keys.Down, Keys.Select, Keys.Back}
}
