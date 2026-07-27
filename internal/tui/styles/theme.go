// Package styles holds the DBWiz color palette and shared Lip Gloss styles.
// Screens must pull styles from here — no inline colors anywhere else, so the
// whole app re-themes from this one file.
package styles

import "charm.land/lipgloss/v2"

// Palette. Kept small and named by role, not by hue, so the theme can change in
// one place. Colors are ANSI-256 indices: they resolve against the terminal's
// own palette, so they adapt to light and dark themes without us swapping
// values per background.
var (
	Accent  = lipgloss.Color("39")  // interactive / focused
	Success = lipgloss.Color("42")  // completed / healthy / running
	Danger  = lipgloss.Color("196") // destructive / errors
	Warning = lipgloss.Color("214") // caution
	Muted   = lipgloss.Color("245") // secondary / de-emphasized text
	Text    = lipgloss.Color("252") // primary body text
)

// Shared styles. Every screen composes its view from these; a screen that needs
// a new visual role adds it here rather than styling inline.
var (
	// Title is the app/screen heading.
	Title = lipgloss.NewStyle().Bold(true).Foreground(Accent)

	// Subtitle is a secondary heading (e.g. a screen's one-line purpose).
	Subtitle = lipgloss.NewStyle().Foreground(Text)

	// Hint is de-emphasized helper text (e.g. the "press q to quit" line).
	Hint = lipgloss.NewStyle().Foreground(Muted)

	// Selected marks the focused row in a menu or list.
	Selected = lipgloss.NewStyle().Bold(true).Foreground(Accent)

	// Item is an unfocused, selectable row.
	Item = lipgloss.NewStyle().Foreground(Text)

	// Running / Stopped label detected containers by state.
	Running = lipgloss.NewStyle().Bold(true).Foreground(Success)
	Stopped = lipgloss.NewStyle().Foreground(Muted)

	// Badge tags a container with provenance (e.g. the Omarchy badge).
	Badge = lipgloss.NewStyle().Foreground(Warning)

	// DangerText / SuccessText / WarningText are inline colored spans.
	DangerText  = lipgloss.NewStyle().Foreground(Danger)
	SuccessText = lipgloss.NewStyle().Foreground(Success)
	WarningText = lipgloss.NewStyle().Foreground(Warning)

	// ErrorTitle / ErrorBox render the full-screen error renderer's heading and
	// bordered body.
	ErrorTitle = lipgloss.NewStyle().Bold(true).Foreground(Danger)
	ErrorBox   = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Danger).
			Padding(0, 2)

	// OverlayBox is the neutral bordered modal used by non-error overlays (the
	// cell-detail inspector), so it reads as informational, not a failure.
	OverlayBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Accent).
			Padding(0, 2)

	// Screen is the outer padding every screen renders inside.
	Screen = lipgloss.NewStyle().Padding(1, 2)

	// Pane / PaneFocused are the bordered boxes the dashboard's three panes
	// render inside. The focused pane borders in Accent so focus is obvious at a
	// glance; the rest border in Muted. Horizontal padding only — vertical space
	// is precious, so panes hug their content top-to-bottom.
	Pane = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Muted).
		Padding(0, 1)
	PaneFocused = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Accent).
			Padding(0, 1)

	// PaneTitle heads each pane; the focused pane's title brightens to Accent via
	// PaneTitleFocused so the eye finds the active pane even in a screenshot.
	PaneTitle        = lipgloss.NewStyle().Bold(true).Foreground(Muted)
	PaneTitleFocused = lipgloss.NewStyle().Bold(true).Foreground(Accent)

	// TableHeader marks the column-header row of the results grid; NullText marks
	// a SQL NULL cell so it reads distinctly from an empty string. TableSelected
	// highlights the cell under the results cursor (reverse video, so it stands out
	// on any palette).
	TableHeader   = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	NullText      = lipgloss.NewStyle().Faint(true).Foreground(Muted).Italic(true)
	TableSelected = lipgloss.NewStyle().Reverse(true)
)
