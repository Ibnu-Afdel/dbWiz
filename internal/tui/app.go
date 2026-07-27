// Package tui is the terminal UI. It depends on the db and docker packages (via
// the screens sub-package); they never depend on it. app.go owns the root
// model: the screen stack, the global keys, the help bar, and the navigation
// plumbing that screens drive by returning messages from package screens.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"

	"github.com/Ibnu-Afdel/dbwiz/internal/tui/screens"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// model is the root application model. It keeps a stack of screens and delegates
// Init/Update/View to the one on top; navigation messages from screens push,
// replace, or pop that stack. The root itself only handles what's truly global:
// window size, the help toggle, and ctrl+c.
type model struct {
	stack []screens.Screen
	help  help.Model

	width, height int
}

func newModel() model {
	return newModelWith(screens.NewDetect())
}

// newModelWith seeds the root model with a specific starting screen. Production
// always starts on detection via newModel; tests use this to drive navigation
// from a known screen without shelling out to docker.
func newModelWith(start screens.Screen) model {
	return model{
		stack: []screens.Screen{start},
		help:  help.New(),
	}
}

// top returns the active screen (top of the stack).
func (m model) top() screens.Screen { return m.stack[len(m.stack)-1] }

// Init starts the first screen (detection) running.
func (m model) Init() tea.Cmd { return m.top().Init() }

// Update handles global concerns and navigation, then delegates everything else
// to the active screen. Navigation messages are intercepted here so screens
// never touch the stack directly.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		// Forward to the active screen so components (lists, inputs) resize too.
		return m.delegate(msg)

	case tea.KeyPressMsg:
		// Global keys first. ctrl+c always quits; ? toggles the full help.
		if key.Matches(msg, screens.Keys.Quit) {
			return m, tea.Quit
		}
		if key.Matches(msg, screens.Keys.Help) {
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		}
		return m.delegate(msg)

	case screens.PushMsg:
		m.stack = append(m.stack, msg.Screen)
		return m, m.enter(msg.Screen)
	case screens.ReplaceMsg:
		m.stack[len(m.stack)-1] = msg.Screen
		return m, m.enter(msg.Screen)
	case screens.PopMsg:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		return m, nil
	case screens.QuitMsg:
		return m, tea.Quit
	}

	return m.delegate(msg)
}

// delegate passes a message to the active screen and stores the (possibly new)
// screen it returns.
func (m model) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.top().Update(msg)
	m.stack[len(m.stack)-1] = next
	return m, cmd
}

// enter runs a newly-activated screen's Init and hands it the current window
// size, so a screen pushed after startup is laid out immediately rather than
// waiting for the next resize.
func (m model) enter(s screens.Screen) tea.Cmd {
	size := func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} }
	return tea.Batch(s.Init(), size)
}

// View renders the active screen with the help bar pinned at the bottom. The
// help bar shows the active screen's bindings plus the global ones, so it always
// reflects where the user is.
func (m model) View() tea.View {
	helpBar := m.help.View(keymap{bindings: append(m.top().Help(), screens.Keys.Help, screens.Keys.Quit)})

	// Reserve the last row for the help bar.
	bodyHeight := m.height - 1
	if bodyHeight < 1 {
		bodyHeight = m.height
	}
	body := m.top().View(m.width, bodyHeight)

	v := tea.NewView(body + "\n" + styles.Hint.Render(helpBar))
	v.AltScreen = true // full-window program; restores the terminal on exit
	return v
}

// keymap adapts a flat binding slice to the help.KeyMap interface so the root
// can render whatever set the active screen exposes.
type keymap struct{ bindings []key.Binding }

func (k keymap) ShortHelp() []key.Binding  { return k.bindings }
func (k keymap) FullHelp() [][]key.Binding { return [][]key.Binding{k.bindings} }

// Run starts the Bubble Tea program and blocks until the user exits. It is the
// single entry point main.go calls when dbwiz is invoked with no subcommand.
func Run() error {
	p := tea.NewProgram(newModel())
	_, err := p.Run()
	return err
}
