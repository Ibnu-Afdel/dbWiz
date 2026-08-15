package screens

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/list"

	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// findItem adapts one navigator name (a database, table, or user) to the list's
// DefaultItem shape. There's nothing to describe beyond the name itself, so the
// delegate is told to skip the description line (see newFindModel) — that halves
// the vertical space per row, which is the point on a database with hundreds of
// tables.
type findItem string

func (i findItem) Title() string       { return string(i) }
func (i findItem) FilterValue() string { return string(i) }
func (i findItem) Description() string { return "" }

// findModel is the type-to-filter jump list opened over whichever navigator pane
// (databases/tables/users) has focus (v5 1.1). Scrolling a cursor through a list
// of hundreds of tables one row at a time is the exact complaint this answers, so
// it wraps bubbles/list the same way the history and saved-query overlays do —
// but single-line per item, since a name is all there is to show.
type findModel struct {
	list list.Model
}

// newFindModel builds the overlay from the focused pane's current names, titled
// for whichever pane that is so the overlay never reads as generic.
func newFindModel(names []string, title string, width, height int) findModel {
	items := make([]list.Item, len(names))
	for i, n := range names {
		items[i] = findItem(n)
	}
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	l := list.New(items, d, width, height)
	l.Title = title
	l.SetShowHelp(false) // the app renders its own help bar
	l.SetShowStatusBar(false)
	return findModel{list: l}
}

// startFind opens the jump list over the focused navigator pane's names. It's a
// silent no-op anywhere else (the editor, results, an already-empty pane) — the
// key just does nothing rather than opening an empty box.
func (s dashboardScreen) startFind() (dashboardScreen, tea.Cmd) {
	var title string
	var names []string
	switch s.focus {
	case focusDatabases:
		title = "Find a database"
		for _, d := range s.databases {
			names = append(names, d.Name)
		}
	case focusTables:
		title = "Find a table"
		for _, t := range s.tables {
			names = append(names, t.Name)
		}
	case focusUsers:
		title = "Find a user"
		for _, u := range s.users {
			names = append(names, u.Name)
		}
	default:
		return s, nil
	}
	if len(names) == 0 {
		return s, nil
	}
	w := clamp(s.width-8, 20, 100)
	h := clamp(s.height-8, 5, 24)
	s.findList = newFindModel(names, title, w, h)
	s.findTarget = s.focus
	s.mode = modeFind
	return s, nil
}

// updateFind handles one key: while the filter input is active every key belongs
// to the list; only once filtering has settled do esc cancel and enter jump — the
// same two-stage flow as the history and saved-query overlays.
func (s dashboardScreen) updateFind(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	if !s.findList.settingFilter() {
		switch msg.String() {
		case "esc":
			s.mode = modeBrowse
			return s, nil
		case "enter":
			s.mode = modeBrowse
			if it, ok := s.findList.list.SelectedItem().(findItem); ok {
				return s.applyFindPick(string(it))
			}
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.findList.list, cmd = s.findList.list.Update(msg)
	return s, cmd
}

// applyFindPick moves the target pane's real cursor onto the picked name and
// runs the same action Enter would on it there — switching database (and loading
// its tables), previewing a table's rows, or just landing on a user, since the
// users pane has no select action of its own.
func (s dashboardScreen) applyFindPick(name string) (dashboardScreen, tea.Cmd) {
	switch s.findTarget {
	case focusDatabases:
		for i, d := range s.databases {
			if d.Name == name {
				s.dbCursor = i
				break
			}
		}
		s.focus = focusDatabases
		return s.selectFocused()
	case focusTables:
		for i, t := range s.tables {
			if t.Name == name {
				s.tblCursor = i
				break
			}
		}
		s.focus = focusTables
		return s.selectFocused()
	case focusUsers:
		for i, u := range s.users {
			if u.Name == name {
				s.userCursor = i
				break
			}
		}
		s.focus = focusUsers
	}
	return s, nil
}

// settingFilter reports whether the filter text input is currently capturing
// keys, so the dashboard can tell the root to leave digits for it (CapturesText).
func (m findModel) settingFilter() bool { return m.list.SettingFilter() }

func (m findModel) View(width int) string {
	return styles.Screen.Render(m.list.View())
}
