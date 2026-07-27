package screens

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// containerItem adapts a detected container to the list's DefaultItem shape:
// Title is the name, Description carries engine/image/port/state plus an Omarchy
// badge, and FilterValue lets the user type-to-filter by name.
type containerItem struct{ c docker.Container }

func (i containerItem) Title() string       { return i.c.Name }
func (i containerItem) FilterValue() string { return i.c.Name }
func (i containerItem) Description() string {
	badge := ""
	if i.c.Source == docker.SourceOmarchy {
		badge = "  [omarchy]"
	}
	return fmt.Sprintf("%s · %s · %s · %s%s",
		i.c.Engine, i.c.Image, portLabel(i.c.HostPort), stateLabel(i.c.State), badge)
}

// pickerScreen lets the user choose among 2+ running containers for the "use
// existing" route. Exactly-one is auto-skipped upstream, so this screen always
// has real choices. Enter connects to the selection; esc goes back.
type pickerScreen struct {
	list       list.Model
	containers []docker.Container
	create     bool // route the selection to the create-database flow (Step 6.7)
}

// NewPicker builds the picker over the given (running) containers for the browse
// route.
func NewPicker(containers []docker.Container) Screen { return newPicker(containers, false) }

// NewPickerCreating builds the picker for the home "Create new database" route:
// the chosen container lands on the create-database form.
func NewPickerCreating(containers []docker.Container) Screen { return newPicker(containers, true) }

func newPicker(containers []docker.Container, create bool) Screen {
	items := make([]list.Item, len(containers))
	for i, c := range containers {
		items[i] = containerItem{c: c}
	}
	l := list.New(items, list.NewDefaultDelegate(), 80, 20)
	l.Title = "Pick a database container"
	l.SetShowHelp(false) // the app renders its own help bar
	l.SetShowStatusBar(false)
	return pickerScreen{list: l, containers: containers, create: create}
}

func (s pickerScreen) Init() tea.Cmd { return nil }

func (s pickerScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.list.SetSize(msg.Width-4, msg.Height-4)
		return s, nil
	case tea.KeyPressMsg:
		// Let the filter input consume keys when the user is typing.
		if s.list.SettingFilter() {
			break
		}
		switch msg.String() {
		case "esc":
			return s, Pop()
		case "enter":
			if it, ok := s.list.SelectedItem().(containerItem); ok {
				if s.create {
					return s, Push(NewConnectCreating(it.c))
				}
				return s, Push(NewConnect(it.c))
			}
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

func (s pickerScreen) View(width, height int) string {
	return styles.Screen.Render(s.list.View())
}

func (s pickerScreen) Help() []key.Binding {
	return []key.Binding{Keys.Up, Keys.Down, Keys.Select, Keys.Back}
}

func stateLabel(st docker.ContainerState) string {
	if st == docker.StateRunning {
		return "running"
	}
	return "stopped"
}
