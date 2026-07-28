package screens

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/remote"
)

// manualConnectScreen collects a host/port/user/password/database by hand and
// connects to any reachable server database — no Docker involved (v3 3.1). It
// wraps the shared formModel with an engine choice field; on submit it hands the
// typed target to the connect screen, which prompts for a password only if the
// server rejects what was entered.
type manualConnectScreen struct {
	form formModel
}

// NewManualConnect builds the host/port entry form, focused on the host field so
// typing starts there with a sensible engine (Postgres) preselected above.
func NewManualConnect() Screen {
	host := textinput.New()
	host.Prompt = "› "
	host.Placeholder = "db.example.com or 127.0.0.1"

	port := textinput.New()
	port.Prompt = "› "
	port.Placeholder = "5432"

	user := textinput.New()
	user.Prompt = "› "
	user.Placeholder = "postgres"

	pass := textinput.New()
	pass.Prompt = "› "
	pass.Placeholder = "(optional — you'll be prompted if it's needed)"
	pass.EchoMode = textinput.EchoPassword

	dbn := textinput.New()
	dbn.Prompt = "› "
	dbn.Placeholder = "(optional — defaults to the engine's maintenance db)"

	sshIn := textinput.New()
	sshIn.Prompt = "› "
	sshIn.Placeholder = "(optional — user@host to tunnel through, e.g. deploy@1.2.3.4)"

	f := formModel{
		title:  "Connect by host and port",
		note:   "For any reachable Postgres, MySQL, or MariaDB — no Docker needed. With an SSH host set, Host/Port are the database as seen from that host (usually 127.0.0.1). This connection isn't saved; add a [[target]] to config.toml to keep it.",
		submit: "connect",
		fields: []formField{
			{key: "engine", label: "Engine", kind: fieldChoice, choices: []string{"postgres", "mysql", "mariadb"}},
			{key: "host", label: "Host", kind: fieldText, input: host},
			{key: "port", label: "Port", kind: fieldText, input: port},
			{key: "user", label: "User", kind: fieldText, input: user},
			{key: "password", label: "Password", kind: fieldText, input: pass},
			{key: "database", label: "Database", kind: fieldText, input: dbn},
			{key: "ssh", label: "SSH tunnel", kind: fieldText, input: sshIn},
		},
		validate: validateManualConnect,
	}
	f.focus = 1 // start on the host field, not the engine choice above it
	_ = (&f).syncFocus()
	return manualConnectScreen{form: f}
}

func (s manualConnectScreen) Init() tea.Cmd { return textinput.Blink }

// CapturesText is always true: this screen is a form, so a digit typed into the
// port (or any field) must never be stolen as a tab switch. Satisfies
// screens.TextInputer.
func (s manualConnectScreen) CapturesText() bool { return true }

func (s manualConnectScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	var (
		res formResult
		cmd tea.Cmd
	)
	s.form, res, cmd = s.form.update(kp)
	switch res {
	case formCanceled:
		return s, Pop()
	case formSubmitted:
		return s, s.connect()
	}
	return s, cmd
}

// connect turns the completed form into a manual connect. Engine, port, and any
// SSH host are already validated, so mapping/parsing them is expected to succeed.
func (s manualConnectScreen) connect() tea.Cmd {
	kind, ok := manualKind(s.form.choice("engine"))
	if !ok {
		kind = db.KindPostgres
	}
	port, _ := strconv.Atoi(s.form.value("port"))
	base := db.Target{
		Host:     s.form.value("host"),
		Port:     port,
		User:     s.form.value("user"),
		Password: s.form.value("password"),
		Database: s.form.value("database"),
	}
	var ssh *remote.SSHSpec
	if raw := s.form.value("ssh"); raw != "" {
		if spec, err := remote.ParseSSH(raw); err == nil {
			ssh = &spec
		}
	}
	return Push(NewConnectManualEntry(base.Host, kind, base, ssh))
}

func (s manualConnectScreen) View(width, height int) string {
	return s.form.View(width)
}

func (s manualConnectScreen) Help() []key.Binding {
	return s.form.Help()
}

// validateManualConnect requires a host, a valid port, and a user; an empty
// password is allowed (DBWiz tries without one, then prompts if the server asks).
func validateManualConnect(f formModel) (errText, warnText string) {
	if f.value("host") == "" {
		return "enter a host", ""
	}
	p := f.value("port")
	if p == "" {
		return "enter a port", ""
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return "port must be a number between 1 and 65535", ""
	}
	if f.value("user") == "" {
		return "enter a user", ""
	}
	if raw := f.value("ssh"); raw != "" {
		if _, err := remote.ParseSSH(raw); err != nil {
			return "SSH tunnel must be user@host or user@host:port", ""
		}
	}
	if f.value("password") == "" {
		return "", "no password entered — DBWiz will try without one, then prompt if it's needed"
	}
	return "", ""
}
