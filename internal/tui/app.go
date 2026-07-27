// Package tui is the terminal UI. It depends on the db and docker packages (via
// the screens sub-package); they never depend on it. app.go owns the root
// model: the tab set, the screen stack inside each tab, the global keys, the
// help bar, and the navigation plumbing that screens drive by returning messages
// from package screens.
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/tui/screens"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// tab is one open target: an independent screen stack with its own connection
// lifecycle. The user can hold several open at once (v2 Phase 1) and switch
// between them; each remembers where it was.
type tab struct {
	stack []screens.Screen
}

func (t tab) top() screens.Screen { return t.stack[len(t.stack)-1] }

// title labels the tab in the tab bar. A screen that knows its target (the
// dashboard) reports it via screens.Titled; anything else is still finding one,
// so it shows as "new".
func (t tab) title() string {
	if ti, ok := t.top().(screens.Titled); ok {
		if s := ti.Title(); s != "" {
			return s
		}
	}
	return "new"
}

// model is the root application model. It keeps a set of tabs, each a stack of
// screens, and delegates Init/Update/View to the active tab's top screen.
// Navigation messages from screens push, replace, or pop that stack; tab keys
// (handled globally) switch, open, and close whole tabs. The root itself only
// handles what's truly global: window size, the help toggle, tab management, and
// ctrl+c.
type model struct {
	tabs   []tab
	active int
	help   help.Model

	width, height int
}

func newModel() model {
	return newModelWith(screens.NewDetect())
}

// newModelWith seeds the root model with a single tab starting on a specific
// screen. Production always starts on detection via newModel; tests use this to
// drive navigation from a known screen without shelling out to docker.
func newModelWith(start screens.Screen) model {
	return model{
		tabs: []tab{{stack: []screens.Screen{start}}},
		help: help.New(),
	}
}

// top returns the active screen (top of the active tab's stack).
func (m model) top() screens.Screen { return m.tabs[m.active].top() }

// setTop stores a (possibly new) screen back onto the active tab's stack.
func (m *model) setTop(s screens.Screen) {
	st := m.tabs[m.active].stack
	st[len(st)-1] = s
}

// Init starts the first screen (detection) running.
func (m model) Init() tea.Cmd { return m.top().Init() }

// Update handles global concerns and navigation, then delegates everything else
// to the active screen. Navigation and tab messages are intercepted here so
// screens never touch the stack or the tab set directly.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		// Forward to the active screen so components (lists, inputs) resize too.
		return m.delegate(msg)

	case tea.KeyPressMsg:
		if next, cmd, handled := m.handleGlobalKey(msg); handled {
			return next, cmd
		}
		return m.delegate(msg)

	case screens.PushMsg:
		m.tabs[m.active].stack = append(m.tabs[m.active].stack, msg.Screen)
		return m, m.enter(msg.Screen)
	case screens.ReplaceMsg:
		m.setTop(msg.Screen)
		return m, m.enter(msg.Screen)
	case screens.PopMsg:
		if st := m.tabs[m.active].stack; len(st) > 1 {
			m.tabs[m.active].stack = st[:len(st)-1]
		}
		return m, nil
	case screens.QuitMsg:
		return m, tea.Quit
	}

	return m.delegate(msg)
}

// handleGlobalKey processes the keys the root owns before any screen sees them:
// quit, the help toggle, and tab management. The bool reports whether the key
// was consumed; when false the caller delegates it to the active screen.
func (m model) handleGlobalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, screens.Keys.Quit):
		return m, tea.Quit, true
	case key.Matches(msg, screens.Keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil, true
	case key.Matches(msg, screens.Keys.NewTab):
		return m.openTab()
	case key.Matches(msg, screens.Keys.CloseTab):
		return m.closeTab()
	case key.Matches(msg, screens.Keys.NextTab):
		return m.switchTab((m.active + 1) % len(m.tabs))
	}
	// alt+1…9 jumps directly to a tab by position.
	if n, ok := altDigit(msg); ok && n >= 1 && n <= len(m.tabs) {
		return m.switchTab(n - 1)
	}
	return m, nil, false
}

// openTab appends a fresh detection tab and switches to it, so a second target
// can be found and connected without disturbing the first.
func (m model) openTab() (tea.Model, tea.Cmd, bool) {
	start := screens.NewDetect()
	m.tabs = append(m.tabs, tab{stack: []screens.Screen{start}})
	m.active = len(m.tabs) - 1
	return m, m.enter(start), true
}

// closeTab tears down the active tab, releasing any live connection it holds,
// and moves focus to the neighbouring tab. Closing the last remaining tab quits
// the app.
func (m model) closeTab() (tea.Model, tea.Cmd, bool) {
	if len(m.tabs) == 1 {
		return m, tea.Quit, true
	}
	for _, s := range m.tabs[m.active].stack {
		if c, ok := s.(screens.Closer); ok {
			_ = c.Close()
		}
	}
	m.tabs = append(m.tabs[:m.active], m.tabs[m.active+1:]...)
	if m.active >= len(m.tabs) {
		m.active = len(m.tabs) - 1
	}
	// Re-lay-out the newly focused tab's screen for the current window size.
	return m, m.resize(), true
}

// switchTab focuses tab i and re-lays-out its screen for the current window
// size (background tabs miss resize messages while inactive).
func (m model) switchTab(i int) (tea.Model, tea.Cmd, bool) {
	if i < 0 || i >= len(m.tabs) || i == m.active {
		return m, nil, true
	}
	m.active = i
	return m, m.resize(), true
}

// delegate passes a message to the active screen and stores the (possibly new)
// screen it returns.
func (m model) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.top().Update(msg)
	m.setTop(next)
	return m, cmd
}

// enter runs a newly-activated screen's Init and hands it the current window
// size, so a screen pushed after startup is laid out immediately rather than
// waiting for the next resize.
func (m model) enter(s screens.Screen) tea.Cmd {
	return tea.Batch(s.Init(), m.resize())
}

// resize replays the current window size as a command, used when a screen
// becomes active (new tab, tab switch) and needs to lay itself out.
func (m model) resize() tea.Cmd {
	w, h := m.width, m.height
	return func() tea.Msg { return tea.WindowSizeMsg{Width: w, Height: h} }
}

// View renders the tab bar (only when more than one tab is open), the active
// screen, and the help bar pinned at the bottom.
func (m model) View() tea.View {
	helpBar := m.help.View(keymap{bindings: m.helpBindings()})

	var header string
	reserved := 1 // help bar row
	if len(m.tabs) > 1 {
		header = m.tabBar() + "\n"
		reserved += 2 // tab bar row + its trailing newline occupy one visible row
	}

	bodyHeight := m.height - reserved
	if bodyHeight < 1 {
		bodyHeight = m.height
	}
	body := m.top().View(m.width, bodyHeight)

	v := tea.NewView(header + body + "\n" + styles.Hint.Render(helpBar))
	v.AltScreen = true // full-window program; restores the terminal on exit
	return v
}

// helpBindings assembles the help bar: the active screen's bindings, then the
// global keys. Tab-switching keys appear only once a second tab exists, so the
// single-target experience stays uncluttered; "new tab" is always offered.
func (m model) helpBindings() []key.Binding {
	b := append([]key.Binding{}, m.top().Help()...)
	b = append(b, screens.Keys.NewTab)
	if len(m.tabs) > 1 {
		b = append(b, screens.Keys.NextTab, screens.Keys.CloseTab)
	}
	return append(b, screens.Keys.Help, screens.Keys.Quit)
}

// tabBar renders the row of open tabs, the active one highlighted, each prefixed
// with its 1-based number so alt+N is discoverable.
func (m model) tabBar() string {
	cells := make([]string, len(m.tabs))
	for i, t := range m.tabs {
		label := fmt.Sprintf(" %d %s ", i+1, t.title())
		if i == m.active {
			cells[i] = styles.TabActive.Render(label)
		} else {
			cells[i] = styles.TabInactive.Render(label)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// keymap adapts a flat binding slice to the help.KeyMap interface so the root
// can render whatever set the active screen exposes.
type keymap struct{ bindings []key.Binding }

func (k keymap) ShortHelp() []key.Binding  { return k.bindings }
func (k keymap) FullHelp() [][]key.Binding { return [][]key.Binding{k.bindings} }

// altDigit reports whether msg is alt+<digit> and returns the digit, so the root
// can jump straight to a tab by number.
func altDigit(msg tea.KeyPressMsg) (int, bool) {
	s := msg.String()
	if after, ok := strings.CutPrefix(s, "alt+"); ok && len(after) == 1 && after[0] >= '1' && after[0] <= '9' {
		return int(after[0] - '0'), true
	}
	return 0, false
}

// Run starts the Bubble Tea program and blocks until the user exits. It is the
// single entry point main.go calls when dbwiz is invoked with no subcommand.
func Run() error {
	p := tea.NewProgram(newModel())
	_, err := p.Run()
	return err
}
