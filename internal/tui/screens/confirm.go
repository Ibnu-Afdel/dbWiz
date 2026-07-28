package screens

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// confirmResult is what a key press did to a confirm prompt.
type confirmResult int

const (
	confirmPending  confirmResult = iota // still waiting (input edited, or enter with no match)
	confirmAborted                       // esc — caller should cancel
	confirmAccepted                      // enter with an exact match — caller should proceed
)

// confirmModel is the reusable type-the-exact-name destructive confirm. It is a
// component, not a Screen: the owning screen embeds it, routes keys to update,
// and acts on the returned result. Accept is gated on the typed text exactly
// matching target, so a destructive action can't fire on a stray Enter; esc
// always aborts. Callers style around it via the danger palette baked in here.
type confirmModel struct {
	title   string
	message string
	target  string // the exact string the user must type to enable accept
	input   textinput.Model
	remote  bool // adds a REMOTE caution banner for a non-local target (v3 3.4)
}

// newConfirm builds a confirm prompt requiring target to be typed verbatim.
func newConfirm(title, message, target string) confirmModel {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = target
	in.Focus()
	return confirmModel{title: title, message: message, target: target, input: in}
}

// onRemote flags the confirm as acting on a non-local target, adding the REMOTE
// caution banner. Typing the exact name is already the strong gate here, so the
// banner is the "extra" the remote rails add (v3 3.4).
func (c confirmModel) onRemote(remote bool) confirmModel {
	c.remote = remote
	return c
}

func (c confirmModel) Init() tea.Cmd { return textinput.Blink }

// update handles one key. It returns the (possibly edited) model, the result,
// and any command from the embedded input. The caller checks the result:
// Accepted runs the destructive action, Aborted cancels, Pending keeps showing.
func (c confirmModel) update(msg tea.KeyPressMsg) (confirmModel, confirmResult, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return c, confirmAborted, nil
	case "enter":
		if c.matched() {
			return c, confirmAccepted, nil
		}
		return c, confirmPending, nil // no match — Enter is inert, not destructive
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return c, confirmPending, cmd
}

// matched reports whether the typed text equals the required target exactly.
func (c confirmModel) matched() bool { return c.input.Value() == c.target }

func (c confirmModel) View(width int) string {
	confirmLine := styles.Hint.Render("enter disabled until the name matches · esc to cancel")
	if c.matched() {
		confirmLine = styles.DangerText.Render("enter to confirm") + styles.Hint.Render(" · esc to cancel")
	}
	lines := []string{styles.ErrorTitle.Render(c.title), ""}
	if c.remote {
		lines = append(lines, styles.DangerBadge.Render(" REMOTE ")+" "+styles.DangerText.Render("This is a non-local database."), "")
	}
	lines = append(lines,
		styles.Subtitle.Render(c.message),
		styles.Hint.Render("Type ")+styles.DangerText.Render(c.target)+styles.Hint.Render(" to confirm:"),
		c.input.View(),
		"",
		confirmLine,
	)
	return styles.ErrorBox.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (c confirmModel) Help() []key.Binding {
	return []key.Binding{Keys.Select, Keys.Back}
}
