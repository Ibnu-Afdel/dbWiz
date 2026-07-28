package screens

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"

	"github.com/Ibnu-Afdel/dbwiz/internal/config"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// savedItem adapts a saved manual target to the list's DefaultItem shape: Title
// is the target name, Description carries engine/host/port/user, and FilterValue
// filters by name.
type savedItem struct{ t config.ManualTarget }

func (i savedItem) Title() string       { return i.t.Name }
func (i savedItem) FilterValue() string { return i.t.Name }
func (i savedItem) Description() string {
	return fmt.Sprintf("%s · %s:%d · %s", i.t.Engine, i.t.Host, i.t.Port, i.t.User)
}

// savedScreen lets the user pick among 2+ saved manual targets (v2 3.3). With
// exactly one the home menu connects directly and never opens this. Enter
// connects to the selection; esc goes back.
type savedScreen struct {
	list    list.Model
	targets []config.ManualTarget
}

// NewSavedPicker builds the picker over the given saved targets.
func NewSavedPicker(targets []config.ManualTarget) Screen {
	items := make([]list.Item, len(targets))
	for i, t := range targets {
		items[i] = savedItem{t: t}
	}
	l := list.New(items, list.NewDefaultDelegate(), 80, 20)
	l.Title = "Pick a saved target"
	l.SetShowHelp(false) // the app renders its own help bar
	l.SetShowStatusBar(false)
	return savedScreen{list: l, targets: targets}
}

func (s savedScreen) Init() tea.Cmd { return nil }

// CapturesText is true while the list's type-to-filter input is active, so a
// digit filters instead of switching tabs. Satisfies screens.TextInputer.
func (s savedScreen) CapturesText() bool { return s.list.SettingFilter() }

func (s savedScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.list.SetSize(msg.Width-4, msg.Height-4)
		return s, nil
	case tea.KeyPressMsg:
		if s.list.SettingFilter() {
			break
		}
		switch msg.String() {
		case "esc":
			return s, Pop()
		case "enter":
			if it, ok := s.list.SelectedItem().(savedItem); ok {
				return s, Push(NewConnectManual(it.t))
			}
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

func (s savedScreen) View(width, height int) string {
	return styles.Screen.Render(s.list.View())
}

func (s savedScreen) Help() []key.Binding {
	return []key.Binding{Keys.Up, Keys.Down, Keys.Select, Keys.Back}
}

// savedRoute picks the next screen for the "saved target" home choice: exactly
// one connects straight through; 2+ opens the picker. It is only reachable when
// there is at least one saved target.
func savedRoute(targets []config.ManualTarget) tea.Cmd {
	if len(targets) == 1 {
		return Push(NewConnectManual(targets[0]))
	}
	return Push(NewSavedPicker(targets))
}
