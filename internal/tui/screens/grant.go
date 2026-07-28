package screens

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// grantResult is what a key press asked the dashboard to do with the grant
// overlay. The overlay never touches the engine itself: it records intent and
// the dashboard fires the matching async command (protocol #4).
type grantResult int

const (
	grantPending  grantResult = iota
	grantCanceled             // esc on the database step closed the whole overlay
	grantPickDB               // a database was chosen — load that user's grants for it
	grantToggle               // a privilege was toggled — apply the grant/revoke
)

// grantStep is which of the overlay's two panes is showing.
type grantStep int

const (
	grantStepDB     grantStep = iota // choose a database
	grantStepMatrix                  // toggle privileges for the chosen database
)

// grantModel is the per-user grant matrix (v2 3.1). Step one picks a database;
// step two shows the engine's database-scope privilege columns with the user's
// current holdings, each toggled with space (grant if unchecked, revoke if
// checked). It is a component embedded by the dashboard, which owns the async
// load/apply and feeds results back via setHeld/setErr.
type grantModel struct {
	subject  string         // the user grants apply to
	privs    []db.Privilege // the engine's database-scope privilege columns
	items    []string       // databases to choose from
	dbCursor int

	step       grantStep
	database   string                // the chosen database (step two)
	held       map[db.Privilege]bool // privileges the user currently holds on database
	privCursor int
	loading    bool        // a grant load/apply is in flight — keys are ignored
	err        *db.DBError // a failed load/apply, shown inline

	// pendingPriv/pendingGrant carry the last toggle for the dashboard to apply.
	pendingPriv  db.Privilege
	pendingGrant bool
}

// newGrant builds the matrix for a user over the given databases and the
// engine's database-scope privilege set.
func newGrant(subject string, privs []db.Privilege, databases []string) grantModel {
	return grantModel{subject: subject, privs: privs, items: databases}
}

// update handles one key. While a round-trip is in flight all keys are ignored
// so a toggle can't race its own reload.
func (g grantModel) update(msg tea.KeyPressMsg) (grantModel, grantResult) {
	if g.loading {
		return g, grantPending
	}
	if g.step == grantStepDB {
		return g.updateDB(msg)
	}
	return g.updateMatrix(msg)
}

// updateDB drives the database picker: esc cancels the overlay, enter descends
// into the matrix for the highlighted database (and asks the dashboard to load
// its grants).
func (g grantModel) updateDB(msg tea.KeyPressMsg) (grantModel, grantResult) {
	switch msg.String() {
	case "esc":
		return g, grantCanceled
	case "up", "k":
		g.dbCursor = wrapCursor(g.dbCursor-1, len(g.items))
		return g, grantPending
	case "down", "j":
		g.dbCursor = wrapCursor(g.dbCursor+1, len(g.items))
		return g, grantPending
	case "enter":
		if len(g.items) == 0 {
			return g, grantPending
		}
		g.database = g.items[g.dbCursor]
		g.step, g.privCursor, g.held, g.err, g.loading = grantStepMatrix, 0, nil, nil, true
		return g, grantPickDB
	}
	return g, grantPending
}

// updateMatrix drives the privilege toggles: esc steps back to the database
// list, space/enter toggles the highlighted privilege for this user+database.
func (g grantModel) updateMatrix(msg tea.KeyPressMsg) (grantModel, grantResult) {
	switch msg.String() {
	case "esc":
		g.step, g.err = grantStepDB, nil
		return g, grantPending
	case "up", "k":
		g.privCursor = wrapCursor(g.privCursor-1, len(g.privs))
		return g, grantPending
	case "down", "j":
		g.privCursor = wrapCursor(g.privCursor+1, len(g.privs))
		return g, grantPending
	case " ", "space", "enter", "x":
		if len(g.privs) == 0 {
			return g, grantPending
		}
		p := g.privs[g.privCursor]
		g.pendingPriv, g.pendingGrant, g.loading = p, !g.held[p], true
		return g, grantToggle
	}
	return g, grantPending
}

// setHeld records the user's current privileges on the chosen database and
// clears the in-flight state — called by the dashboard on a grants-loaded reply.
func (g grantModel) setHeld(held []db.Privilege) grantModel {
	m := make(map[db.Privilege]bool, len(held))
	for _, p := range held {
		m[p] = true
	}
	g.held, g.loading, g.err = m, false, nil
	return g
}

// setErr records a failed load/apply inline and clears the in-flight state.
func (g grantModel) setErr(err *db.DBError) grantModel {
	g.err, g.loading = err, false
	return g
}

func (g grantModel) View(width int) string {
	if g.step == grantStepDB {
		return g.viewDB()
	}
	return g.viewMatrix()
}

func (g grantModel) viewDB() string {
	lines := []string{
		styles.Title.Render("Grants — " + g.subject),
		"",
		styles.Subtitle.Render("Pick a database to see and change what this user can do in it."),
		"",
	}
	if len(g.items) == 0 {
		lines = append(lines, styles.Hint.Render("(no databases)"))
	}
	for i, it := range g.items {
		marker, label := "  ", styles.Item.Render(it)
		if i == g.dbCursor {
			marker, label = styles.Selected.Render("▸ "), styles.Selected.Render(it)
		}
		lines = append(lines, marker+label)
	}
	lines = append(lines, "", styles.Hint.Render("enter open · ↑/↓ move · esc cancel"))
	return styles.Screen.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (g grantModel) viewMatrix() string {
	lines := []string{
		styles.Title.Render("Grants — " + g.subject + " on " + g.database),
		"",
	}
	switch {
	case g.loading && g.held == nil:
		lines = append(lines, styles.Hint.Render("loading grants…"))
	case g.err != nil:
		lines = append(lines, styles.ErrorTitle.Render(adminErrText(g.err)))
	default:
		lines = append(lines, styles.Subtitle.Render("space toggles a privilege · granted privileges are checked."), "")
		for i, p := range g.privs {
			box := "[ ]"
			if g.held[p] {
				box = "[x]"
			}
			row := box + " " + string(p)
			marker, label := "  ", styles.Item.Render(row)
			if i == g.privCursor {
				marker, label = styles.Selected.Render("▸ "), styles.Selected.Render(row)
			}
			lines = append(lines, marker+label)
		}
	}
	lines = append(lines, "", styles.Hint.Render("space toggle · ↑/↓ move · esc back"))
	return styles.Screen.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (g grantModel) Help() []key.Binding {
	if g.step == grantStepDB {
		return []key.Binding{Keys.Select, Keys.Up, Keys.Down, Keys.Back}
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		Keys.Up, Keys.Down, Keys.Back,
	}
}
