// Package screens holds the individual TUI screens (detect, home, picker,
// sqlite_open, connect, error, dashboard), each a Bubble Tea sub-model owned by
// the root model in package tui. Screens depend on db and docker; the root
// depends on screens. Nothing here imports package tui, so navigation flows
// upward through the messages defined below rather than through a back-reference.
package screens

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
)

// Screen is one sub-model in the app. The root model keeps a stack of these and
// delegates the active one's Init/Update/View. A screen never mutates the stack
// directly; it requests navigation by returning one of the messages below,
// which the root interprets. Width and height are passed into View each frame so
// screens stay stateless about layout beyond what their embedded components need.
type Screen interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (Screen, tea.Cmd)
	View(width, height int) string
	// Help returns the screen-specific bindings shown in the help bar. The root
	// appends the global bindings (help, quit) itself.
	Help() []key.Binding
}

// Navigation messages. A screen returns one of these (via a command) to move
// around the stack; the root model is the only thing that acts on them.
type (
	// PushMsg puts Screen on top of the stack, keeping the current one beneath
	// it so Back returns here.
	PushMsg struct{ Screen Screen }

	// ReplaceMsg swaps the top of the stack for Screen without growing it — used
	// for one-way transitions like detect → home where Back should not return to
	// the spinner.
	ReplaceMsg struct{ Screen Screen }

	// PopMsg returns to the screen beneath the current one. At the bottom of the
	// stack it is a no-op, so a screen can always emit it safely.
	PopMsg struct{}

	// QuitMsg asks the app to exit. Screens use it for their own quit keys (e.g.
	// q on the home menu); ctrl+c is handled globally by the root.
	QuitMsg struct{}
)

// Push, Replace, Pop, and Quit wrap the navigation messages as commands so
// screens can return them directly from Update.
func Push(s Screen) tea.Cmd    { return func() tea.Msg { return PushMsg{Screen: s} } }
func Replace(s Screen) tea.Cmd { return func() tea.Msg { return ReplaceMsg{Screen: s} } }
func Pop() tea.Cmd             { return func() tea.Msg { return PopMsg{} } }
func Quit() tea.Cmd            { return func() tea.Msg { return QuitMsg{} } }
