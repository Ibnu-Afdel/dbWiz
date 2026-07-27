package screens

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// grantResult is what a key press did to the grant picker.
type grantResult int

const (
	grantPending  grantResult = iota
	grantCanceled             // esc
	grantChosen               // enter (grant) or x (revoke) on a highlighted item
)

// grantModel is the pick-the-other-side list for v1's coarse grant/revoke (Step
// 6.6). It is invoked for a fixed subject (a user) and lists the databases to
// grant/revoke ALL on; enter grants, x revokes. It is a component embedded by
// the dashboard.
type grantModel struct {
	subject string   // the user the grant/revoke applies to
	items   []string // databases to choose from
	cursor  int
	grant   bool // last action chosen: true = GRANT, false = REVOKE
}

// newGrant builds the picker for a user over the given databases.
func newGrant(subject string, databases []string) grantModel {
	return grantModel{subject: subject, items: databases}
}

// update handles one key: esc cancels; ↑/↓ move; enter chooses GRANT and x
// chooses REVOKE on the highlighted database. On a choice it returns grantChosen
// and the caller reads choice()/granting.
func (g grantModel) update(msg tea.KeyPressMsg) (grantModel, grantResult) {
	switch msg.String() {
	case "esc":
		return g, grantCanceled
	case "up", "k":
		g.cursor = wrapCursor(g.cursor-1, len(g.items))
		return g, grantPending
	case "down", "j":
		g.cursor = wrapCursor(g.cursor+1, len(g.items))
		return g, grantPending
	case "enter":
		if len(g.items) == 0 {
			return g, grantPending
		}
		g.grant = true
		return g, grantChosen
	case "x":
		if len(g.items) == 0 {
			return g, grantPending
		}
		g.grant = false
		return g, grantChosen
	}
	return g, grantPending
}

// choice returns the highlighted database, or "" when the list is empty.
func (g grantModel) choice() string {
	if g.cursor < 0 || g.cursor >= len(g.items) {
		return ""
	}
	return g.items[g.cursor]
}

func (g grantModel) View(width int) string {
	lines := []string{
		styles.Title.Render("Grant / revoke — " + g.subject),
		"",
		styles.Subtitle.Render("Pick a database, then grant or revoke ALL for this user."),
		"",
	}
	if len(g.items) == 0 {
		lines = append(lines, styles.Hint.Render("(no databases)"))
	}
	for i, it := range g.items {
		marker := "  "
		label := styles.Item.Render(it)
		if i == g.cursor {
			marker = styles.Selected.Render("▸ ")
			label = styles.Selected.Render(it)
		}
		lines = append(lines, marker+label)
	}
	lines = append(lines, "", styles.Hint.Render("enter grant · x revoke · ↑/↓ move · esc cancel"))
	return styles.Screen.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (g grantModel) Help() []key.Binding {
	return []key.Binding{
		Keys.Select,
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "revoke")),
		Keys.Back,
	}
}
