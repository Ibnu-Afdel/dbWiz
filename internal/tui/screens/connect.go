package screens

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// connectMode is what the connect screen is currently doing.
type connectMode int

const (
	modeConnecting connectMode = iota // ladder in flight, spinner showing
	modePrompting                     // asking for a password (graceful last rung)
)

// connectScreen runs the credential ladder for a chosen container and, if every
// non-interactive rung is rejected, drops to a masked password prompt. It
// replaces itself with the dashboard on success, so Back from the dashboard
// returns to whatever opened the connect (picker or home), never to the spinner.
type connectScreen struct {
	container docker.Container
	mode      connectMode
	create    bool // land on the create-database form instead of the browser (Step 6.7)

	spinner spinner.Model
	input   textinput.Model

	target    db.Target // resolved target awaiting a password
	submitted bool      // a prompted attempt is in flight (distinguishes a retry)
	errMsg    string    // shown above the prompt after a rejected attempt
}

// NewConnect starts connecting to a container. It immediately runs the ladder;
// Omarchy/recovered credentials mean the user often never sees this screen.
func NewConnect(c docker.Container) Screen { return newConnect(c, false) }

// NewConnectCreating is NewConnect that lands on the create-database form once
// connected — the home "Create new database" route.
func NewConnectCreating(c docker.Container) Screen { return newConnect(c, true) }

func newConnect(c docker.Container, create bool) Screen {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = styles.Selected

	in := textinput.New()
	in.EchoMode = textinput.EchoPassword
	in.Prompt = "Password: "
	in.Placeholder = "password"

	return connectScreen{container: c, mode: modeConnecting, create: create, spinner: sp, input: in}
}

func (s connectScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, connectCmd(s.container, db.Target{}, false))
}

// CapturesText is true while the masked password prompt is up, so a digit typed
// into a password isn't stolen as a tab switch. Satisfies screens.TextInputer.
func (s connectScreen) CapturesText() bool { return s.mode == modePrompting }

func (s connectScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case connectedMsg:
		// Remember this container so a later launch can offer to continue here
		// (v2 Step 1.2). Best-effort: a cache write must never block connecting.
		rememberDocker(msg.container)
		if s.create {
			return s, Replace(NewDashboardCreating(msg.engine, msg.target, msg.container))
		}
		return s, Replace(NewDashboard(msg.engine, msg.target, msg.container))
	case connectAuthMsg:
		s.target = msg.target
		if s.submitted {
			s.errMsg = "Authentication failed — check the password and try again."
		}
		s.submitted = false
		s.mode = modePrompting
		s.input.Reset()
		return s, s.input.Focus()
	case connectErrMsg:
		return s, Replace(NewErrorFromDB(asDBError(msg.err), retrySpec{
			label: "retry",
			cmd:   Replace(newConnect(s.container, s.create)),
		}))
	case spinner.TickMsg:
		if s.mode != modeConnecting {
			return s, nil
		}
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd
	case tea.KeyPressMsg:
		if s.mode == modePrompting {
			return s.updatePrompt(msg)
		}
	}
	return s, nil
}

func (s connectScreen) updatePrompt(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return s, Pop()
	case "enter":
		s.target.Password = s.input.Value()
		s.submitted = true
		s.mode = modeConnecting
		s.errMsg = ""
		return s, tea.Batch(s.spinner.Tick, connectCmd(s.container, s.target, true))
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

func (s connectScreen) View(width, height int) string {
	var body string
	switch s.mode {
	case modeConnecting:
		body = s.spinner.View() + " " + styles.Subtitle.Render("Connecting to "+s.container.Name+"…")
	case modePrompting:
		lines := []string{
			styles.Title.Render("Password needed"),
			"",
			styles.Subtitle.Render(fmt.Sprintf("Couldn't authenticate to %s as %q automatically.",
				s.container.Name, s.target.User)),
		}
		if s.errMsg != "" {
			lines = append(lines, "", styles.DangerText.Render(s.errMsg))
		}
		lines = append(lines, "", s.input.View(), "", styles.Hint.Render("enter to connect · esc to cancel"))
		body = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}

func (s connectScreen) Help() []key.Binding {
	if s.mode == modePrompting {
		return []key.Binding{Keys.Select, Keys.Back}
	}
	return nil
}

// asDBError recovers a typed *db.DBError, wrapping anything else so the error
// screen always has plain-language text.
func asDBError(err error) *db.DBError {
	var de *db.DBError
	if errors.As(err, &de) {
		return de
	}
	return &db.DBError{
		Kind:   db.DBErrInternal,
		Title:  "Couldn't connect",
		Detail: err.Error(),
		Hint:   "press [r] to retry or [b] to go back",
		Err:    err,
	}
}
