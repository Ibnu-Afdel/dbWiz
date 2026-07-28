package screens

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/config"
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/remote"
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

	// Manual (saved, non-Docker) target support (v2 3.3). When manual is set the
	// screen skips the container credential ladder and connects straight to
	// manualBase (host/port/user from config), prompting for the password. manualSSH,
	// when non-nil, first opens an SSH tunnel and connects through it (v3 3.2).
	manual     bool
	manualName string
	manualKind db.Kind
	manualBase db.Target
	manualSSH  *remote.SSHSpec

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
	s := baseConnect()
	s.container = c
	s.create = create
	return s
}

// NewConnectManual starts connecting to a saved manual target (v2 3.3). The
// engine string is already validated by config, so mapping it is expected to
// succeed; an unknown engine falls back to Postgres rather than failing to open
// the screen. A saved target carries no password, so this always drops to the
// prompt.
func NewConnectManual(mt config.ManualTarget) Screen {
	kind, ok := manualKind(mt.Engine)
	if !ok {
		kind = db.KindPostgres
	}
	// A saved target's ssh string is format-checked by config.Valid, so a parse
	// error here is unexpected; on one, connect directly rather than fail to open.
	var spec *remote.SSHSpec
	if mt.SSH != "" {
		if s, err := remote.ParseSSH(mt.SSH); err == nil {
			spec = &s
		}
	}
	return NewConnectManualEntry(mt.Name, kind, db.Target{Host: mt.Host, Port: mt.Port, User: mt.User, Database: mt.Database}, spec)
}

// NewConnectManualEntry starts connecting to a manual (non-Docker) target the
// caller has fully specified — the interactive host/port form (v3 3.1) or a saved
// target (v2 3.3). base may already carry a password (from the form); if it does
// and the server accepts it, the user never sees the prompt, otherwise the screen
// drops to the masked prompt as usual. When ssh is non-nil the connection is
// tunnelled through it (v3 3.2).
func NewConnectManualEntry(name string, kind db.Kind, base db.Target, ssh *remote.SSHSpec) Screen {
	s := baseConnect()
	s.manual = true
	s.manualName = name
	s.manualKind = kind
	s.manualBase = base
	s.manualSSH = ssh
	// A synthetic container so the spinner/prompt text and the dashboard label read
	// the target's name like any other connection.
	s.container = manualContainer(name, kind, base.Port)
	return s
}

func baseConnect() connectScreen {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = styles.Selected

	in := textinput.New()
	in.EchoMode = textinput.EchoPassword
	in.Prompt = "Password: "
	in.Placeholder = "password"

	return connectScreen{mode: modeConnecting, spinner: sp, input: in}
}

// fresh rebuilds a clean connect screen of the same kind (manual or container),
// so a retry from the error screen restarts the right flow.
func (s connectScreen) fresh() Screen {
	r := baseConnect()
	r.container, r.create = s.container, s.create
	r.manual, r.manualName, r.manualKind, r.manualBase = s.manual, s.manualName, s.manualKind, s.manualBase
	r.manualSSH = s.manualSSH
	return r
}

func (s connectScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.attempt(db.Target{}, false))
}

// attempt is the connect command for this screen — the manual path for saved
// targets, the container credential ladder otherwise.
func (s connectScreen) attempt(prompted db.Target, hasPrompt bool) tea.Cmd {
	if s.manual {
		return connectManualCmd(s.manualName, s.manualKind, s.manualBase, s.manualSSH, prompted, hasPrompt)
	}
	return connectCmd(s.container, prompted, hasPrompt)
}

// CapturesText is true while the masked password prompt is up, so a digit typed
// into a password isn't stolen as a tab switch. Satisfies screens.TextInputer.
func (s connectScreen) CapturesText() bool { return s.mode == modePrompting }

func (s connectScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case connectedMsg:
		// Remember this container so a later launch can offer to continue here
		// (v2 Step 1.2). Best-effort: a cache write must never block connecting.
		// Manual targets aren't Docker containers, so they aren't remembered as a
		// continue target (the state cache models only Docker/SQLite).
		if !s.manual {
			rememberDocker(msg.container)
		}
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
			cmd:   Replace(s.fresh()),
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
		return s, tea.Batch(s.spinner.Tick, s.attempt(s.target, true))
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
