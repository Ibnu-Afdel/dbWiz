// Package styles holds the DBWiz color palette and shared Lip Gloss styles.
// Screens must pull styles from here — no inline colors anywhere else, so the
// whole app re-themes from this one file.
//
// The shared styles are rebuilt from the palette by rebuild(), which init() runs
// once at startup and Apply() re-runs when the user picks a theme in config (v2
// 3.3). Reassigning a palette var alone isn't enough — a Lip Gloss style copies
// the color when it's built — so any theme change goes through Apply.
package styles

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette. Kept small and named by role, not by hue, so the theme can change in
// one place. Colors are ANSI-256 indices: they resolve against the terminal's
// own palette, so they adapt to light and dark themes without us swapping
// values per background. A theme (see themes) reassigns these, then rebuild()
// re-derives the styles below.
var (
	Accent  = lipgloss.Color("39")  // interactive / focused
	Success = lipgloss.Color("42")  // completed / healthy / running
	Danger  = lipgloss.Color("196") // destructive / errors
	Warning = lipgloss.Color("214") // caution
	Muted   = lipgloss.Color("245") // secondary / de-emphasized text
	Text    = lipgloss.Color("252") // primary body text
)

// theme is a named palette variant. Only the accent and text tones vary; the
// semantic colors (success/danger/warning) stay put so their meaning is stable
// across themes.
type theme struct {
	accent color.Color
	muted  color.Color
	text   color.Color
}

// themes are the palettes config's `theme = "..."` can select. An unknown name
// falls back to "default" (Apply never errors).
var themes = map[string]theme{
	"default":       {accent: lipgloss.Color("39"), muted: lipgloss.Color("245"), text: lipgloss.Color("252")},
	"high-contrast": {accent: lipgloss.Color("45"), muted: lipgloss.Color("250"), text: lipgloss.Color("255")},
	"warm":          {accent: lipgloss.Color("208"), muted: lipgloss.Color("245"), text: lipgloss.Color("252")},
}

// Apply selects a named theme (case-insensitive) and rebuilds the shared styles.
// An empty or unknown name applies the default palette, so a bad config value
// degrades to the standard look rather than an error. It is called once at
// startup from the TUI entry point, before the program runs.
func Apply(name string) {
	th, ok := themes[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		th = themes["default"]
	}
	Accent, Muted, Text = th.accent, th.muted, th.text
	rebuild()
}

// Shared styles. Every screen composes its view from these; a screen that needs
// a new visual role adds it here rather than styling inline. They are assigned
// by rebuild() so a theme change re-derives them from the current palette.
var (
	Title    lipgloss.Style // the app/screen heading
	Subtitle lipgloss.Style // a secondary heading (e.g. a screen's one-line purpose)
	Hint     lipgloss.Style // de-emphasized helper text (e.g. the "press q to quit" line)
	Selected lipgloss.Style // the focused row in a menu or list
	Item     lipgloss.Style // an unfocused, selectable row

	Running     lipgloss.Style // a detected running container
	Stopped     lipgloss.Style // a detected stopped container
	Badge       lipgloss.Style // provenance tag (e.g. the Omarchy badge)
	DangerBadge lipgloss.Style // loud inverse tag for a non-local target (REMOTE)

	DangerText  lipgloss.Style // inline colored spans
	SuccessText lipgloss.Style
	WarningText lipgloss.Style

	ErrorTitle lipgloss.Style // the full-screen error renderer's heading
	ErrorBox   lipgloss.Style // its bordered body
	OverlayBox lipgloss.Style // the neutral bordered modal (cell-detail inspector)

	Screen lipgloss.Style // the outer padding every screen renders inside

	Pane        lipgloss.Style // the dashboard's three bordered panes
	PaneFocused lipgloss.Style // the focused pane borders in Accent

	PaneTitle        lipgloss.Style // heads each pane
	PaneTitleFocused lipgloss.Style // the focused pane's title brightens to Accent

	TabActive   lipgloss.Style // the tab bar (v2 Phase 1)
	TabInactive lipgloss.Style

	TableHeader   lipgloss.Style // the results grid's column-header row
	NullText      lipgloss.Style // a SQL NULL cell, distinct from an empty string
	TableSelected lipgloss.Style // the cell under the results cursor (reverse video)
)

// rebuild re-derives every shared style from the current palette. Called once by
// init and again by Apply on a theme change.
func rebuild() {
	Title = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	Subtitle = lipgloss.NewStyle().Foreground(Text)
	Hint = lipgloss.NewStyle().Foreground(Muted)
	Selected = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	Item = lipgloss.NewStyle().Foreground(Text)

	Running = lipgloss.NewStyle().Bold(true).Foreground(Success)
	Stopped = lipgloss.NewStyle().Foreground(Muted)
	Badge = lipgloss.NewStyle().Foreground(Warning)
	// DangerBadge is inverse (danger background, light text) so REMOTE reads as a
	// warning label, not just colored text.
	DangerBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(Danger)

	DangerText = lipgloss.NewStyle().Foreground(Danger)
	SuccessText = lipgloss.NewStyle().Foreground(Success)
	WarningText = lipgloss.NewStyle().Foreground(Warning)

	ErrorTitle = lipgloss.NewStyle().Bold(true).Foreground(Danger)
	ErrorBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Danger).
		Padding(0, 2)
	OverlayBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Accent).
		Padding(0, 2)

	Screen = lipgloss.NewStyle().Padding(1, 2)

	Pane = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Muted).
		Padding(0, 1)
	PaneFocused = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Accent).
		Padding(0, 1)

	PaneTitle = lipgloss.NewStyle().Bold(true).Foreground(Muted)
	PaneTitleFocused = lipgloss.NewStyle().Bold(true).Foreground(Accent)

	TabActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(Accent)
	TabInactive = lipgloss.NewStyle().Foreground(Muted)

	TableHeader = lipgloss.NewStyle().Bold(true).Foreground(Accent)
	NullText = lipgloss.NewStyle().Faint(true).Foreground(Muted).Italic(true)
	TableSelected = lipgloss.NewStyle().Reverse(true)
}

func init() { rebuild() }
