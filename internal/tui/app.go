// Package tui is the terminal UI. It depends on the db and docker packages (via
// the screens sub-package); they never depend on it. app.go owns the root
// model: the tab set, the screen stack inside each tab, the global keys, the
// help bar, and the navigation plumbing that screens drive by returning messages
// from package screens.
package tui

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/config"
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

	// showKeys is the F1 catalogue of every binding; keysOffset scrolls it. It
	// lives on the root rather than on a screen because it is global — it has to
	// open over whatever is showing, including a screen's own overlay.
	showKeys   bool
	keysOffset int

	width, height int

	// theme follows the active Omarchy theme when that's the palette in use.
	theme themeWatch
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

// Init starts the first screen (detection) running, plus the theme poll when
// DBWiz is following the Omarchy theme.
func (m model) Init() tea.Cmd { return tea.Batch(m.top().Init(), m.theme.tick()) }

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
	case themeTickMsg:
		// Styles are read at render time, so re-applying the palette is enough;
		// the view redraws after this Update.
		m.theme = m.theme.refresh()
		return m, m.theme.tick()
	}

	return m.delegate(msg)
}

// handleGlobalKey processes the keys the root owns before any screen sees them:
// quit, the help toggle, and tab management. The bool reports whether the key
// was consumed; when false the caller delegates it to the active screen.
func (m model) handleGlobalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if key.Matches(msg, screens.Keys.Quit) {
		return m, tea.Quit, true
	}
	// The catalogue is modal: while it's open it owns every key but quit, so a
	// stray press can't act on the screen hidden behind it.
	if m.showKeys {
		return m.handleKeysOverlay(msg)
	}
	switch {
	case key.Matches(msg, screens.Keys.KeyList):
		m.showKeys, m.keysOffset = true, 0
		return m, nil, true
	case key.Matches(msg, screens.Keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil, true
	case key.Matches(msg, screens.Keys.NewTab):
		return m.openTab()
	case key.Matches(msg, screens.Keys.CloseTab):
		return m.closeTab()
	}
	// A plain digit jumps to that tab — but only with several tabs open and when
	// the active screen isn't taking text input, so a number still types into the
	// SQL editor or a path field. Chosen over ctrl+tab / alt+digit because bare
	// digits and ctrl-letters are what survives tmux.
	if len(m.tabs) > 1 && !m.capturingText() {
		if n, ok := tabDigit(msg); ok {
			if n >= 1 && n <= len(m.tabs) {
				return m.switchTab(n - 1)
			}
			return m, nil, true // swallow an out-of-range tab number
		}
	}
	return m, nil, false
}

// capturingText reports whether the active screen is currently taking free text,
// so the root can leave printable keys (digits) alone for it.
func (m model) capturingText() bool {
	if ti, ok := m.top().(screens.TextInputer); ok {
		return ti.CapturesText()
	}
	return false
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
	h := m.help
	if m.theme.path != "" {
		// Following the desktop theme: recolor the help bar too, which otherwise
		// keeps bubbles' own grays.
		h.Styles = styles.Help()
	}
	helpBar := h.View(flatKeys{bindings: m.helpBindings()})

	var header string
	// The help bar is one row normally, but "?" expands it into a stacked list of
	// every binding this screen offers. Reserving a hard-coded 1 there pushed the
	// bar off the bottom of the window and the list came out truncated, so measure
	// what it actually renders.
	reserved := lipgloss.Height(helpBar)
	if len(m.tabs) > 1 {
		header = m.tabBar() + "\n"
		reserved += 2 // tab bar row + its trailing newline occupy one visible row
	}

	bodyHeight := max(m.height-reserved, 1)
	body := m.top().View(m.width, bodyHeight)
	if m.showKeys {
		body = m.keysView(m.width, bodyHeight)
	}

	v := tea.NewView(header + body + "\n" + styles.Hint.Render(helpBar))
	v.AltScreen = true // full-window program; restores the terminal on exit
	// A stable title lets window managers find the DBWiz window — Omarchy's
	// launch-or-focus matches on it to focus a running DBWiz instead of opening
	// a second one.
	v.WindowTitle = "DBWiz"
	return v
}

// helpBindings assembles the help bar: the active screen's bindings, then the
// global keys. Tab-switching keys appear only once a second tab exists, so the
// single-target experience stays uncluttered; "new tab" is always offered.
func (m model) helpBindings() []key.Binding {
	b := append([]key.Binding{}, m.top().Help()...)
	if m.showKeys {
		// The catalogue owns the screen; advertise only the way out of it.
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "scroll")),
			screens.Keys.Back, screens.Keys.Quit,
		}
	}
	b = append(b, screens.Keys.NewTab)
	if len(m.tabs) > 1 {
		b = append(b, screens.Keys.SwitchTab, screens.Keys.CloseTab)
	}
	return append(b, screens.Keys.Help, screens.Keys.KeyList, screens.Keys.Quit)
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

// flatKeys adapts a flat binding slice to the help.KeyMap interface so the root
// can render whatever set the active screen exposes. (It is not the app's
// keymap — that lives in internal/keymap.)
type flatKeys struct{ bindings []key.Binding }

func (k flatKeys) ShortHelp() []key.Binding  { return k.bindings }
func (k flatKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.bindings} }

// handleKeysOverlay drives the F1 catalogue while it is open. Everything it
// doesn't use is swallowed rather than passed through, which is what makes it
// modal.
func (m model) handleKeysOverlay(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	page := max(m.height-keysChrome, 4)
	switch msg.String() {
	case "esc", "q", "f1":
		m.showKeys = false
	case "up", "k":
		m.keysOffset = max(0, m.keysOffset-1)
	case "down", "j":
		m.keysOffset++
	case "pgup":
		m.keysOffset = max(0, m.keysOffset-page)
	case "pgdown":
		m.keysOffset += page
	case "home":
		m.keysOffset = 0
	}
	return m, nil, true
}

// tabDigit reports whether msg is a bare digit 1–9 (no modifiers) and returns
// its value, so the root can jump straight to a tab by number. It ignores any
// modified digit (ctrl/alt) so those stay available to screens.
func tabDigit(msg tea.KeyPressMsg) (int, bool) {
	if msg.Mod != 0 {
		return 0, false
	}
	if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return int(s[0] - '0'), true
	}
	return 0, false
}

// Run starts the Bubble Tea program and blocks until the user exits. It is the
// single entry point main.go calls when dbwiz is invoked with no subcommand.
// Before starting it folds in the user's opt-in config (v2 3.3): a theme and row
// limit, plus any saved manual targets for the home menu. A malformed config is
// warned about, not fatal — the app still launches with defaults.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbwiz: "+err.Error())
	}
	watch := applyTheme(cfg.Theme)
	screens.ApplyConfig(cfg)

	m := newModel()
	m.theme = watch
	p := tea.NewProgram(m)
	_, err = p.Run()
	return err
}
