package screens

import "charm.land/bubbles/v2/key"

// KeyMap is the central keymap for the whole app. Every screen draws the
// bindings it shows in the help bar from this one value, so the help text and
// the actual handlers never drift apart. Screens expose the subset relevant to
// their focus via their Help method; the root renders it with bubbles/help.
type KeyMap struct {
	// Global.
	Quit key.Binding
	Help key.Binding

	// Navigation shared across screens.
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Back   key.Binding

	// Home-menu / detection actions.
	Rescan key.Binding
	Start  key.Binding

	// Error-screen actions.
	Retry key.Binding
	Info  key.Binding

	// Dashboard / browser actions.
	Focus   key.Binding // cycle focus between panes
	Refresh key.Binding // reload the focused list

	// Admin actions.
	Create key.Binding // create a database/user (by focused section)
	Delete key.Binding // delete the selected database/user (type-to-confirm)
	Grant  key.Binding // grant/revoke a user on a database

	// Query pillar.
	Edit    key.Binding // focus the SQL editor from anywhere on the dashboard
	Run     key.Binding // execute the statement in the editor
	Cancel  key.Binding // cancel a running query
	History key.Binding // cycle previous statements into the editor
}

// Keys is the single instance every screen and the root model share.
var Keys = KeyMap{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Rescan: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "rescan"),
	),
	Start: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "start"),
	),
	Retry: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "retry"),
	),
	Info: key.NewBinding(
		key.WithKeys("i"),
		key.WithHelp("i", "details"),
	),
	Focus: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "focus"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "refresh"),
	),
	Create: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "create"),
	),
	Delete: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "delete"),
	),
	Grant: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "grant"),
	),
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit SQL"),
	),
	// Run has several bindings on purpose: ctrl+enter is ideal on terminals that
	// disambiguate it, but under tmux/multiplexers without extended-keys it
	// collapses to a plain Enter (a newline), so ctrl+r and alt+enter are the
	// reliable fallbacks that always survive, with F5 for good measure.
	Run: key.NewBinding(
		key.WithKeys("ctrl+enter", "ctrl+r", "alt+enter", "f5"),
		key.WithHelp("^R/F5", "run"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	History: key.NewBinding(
		key.WithKeys("ctrl+p", "ctrl+n"),
		key.WithHelp("^p/^n", "history"),
	),
}
