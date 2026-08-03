package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/keymap"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// keysChrome is how many rows of the overlay aren't catalogue: the heading, the
// blank line under it, the blank line and hint above the bottom, and the box's
// two borders.
const keysChrome = 6

// keysView renders the F1 catalogue: every key DBWiz answers to, grouped, with a
// sentence each. It is a separate thing from the "?" footer on purpose — "?"
// answers "what can I press right now", this answers "what can this app do".
func (m model) keysView(width, height int) string {
	inner := clampInt(width-10, 40, 96)
	rows := max(height-keysChrome, 4)

	lines := keysCatalogueLines(inner)
	offset := clampInt(m.keysOffset, 0, max(0, len(lines)-rows))
	end := min(len(lines), offset+rows)

	body := []string{styles.Title.Render("All keys"), ""}
	body = append(body, lines[offset:end]...)

	hint := "esc close"
	if len(lines) > rows {
		hint = "↑/↓ scroll · esc close"
	}
	body = append(body, "", styles.Hint.Render(hint+"  ·  ? shows only the keys that work where you are"))

	box := styles.OverlayBox.Render(lipgloss.JoinVertical(lipgloss.Left, body...))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// keysCatalogueLines flattens the grouped catalogue into display lines, wrapping
// each explanation under its own column so a long sentence never widens the box.
func keysCatalogueLines(width int) []string {
	keyWidth := 0
	for _, g := range keymap.Groups() {
		for _, e := range g.Entries {
			keyWidth = max(keyWidth, lipgloss.Width(e.Key()))
		}
	}
	aboutWidth := max(width-keyWidth-4, 20)

	var lines []string
	for i, g := range keymap.Groups() {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, styles.PaneTitle.Render(g.Title))
		for _, e := range g.Entries {
			row := lipgloss.JoinHorizontal(lipgloss.Top,
				styles.Selected.Width(keyWidth).Render(e.Key()),
				"  ",
				lipgloss.NewStyle().Width(aboutWidth).Render(e.About),
			)
			lines = append(lines, strings.Split(row, "\n")...)
		}
	}
	return lines
}

// clampInt keeps v within [lo, hi].
func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
