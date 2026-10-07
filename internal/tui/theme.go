package tui

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/omarchy"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// themePollInterval is how often DBWiz checks whether the Omarchy theme changed.
// A stat every couple of seconds is negligible and needs no watcher or hook.
const themePollInterval = 2 * time.Second

// themeWatch follows the active Omarchy theme so `omarchy theme set` (or the
// theme picker) recolors a running DBWiz, like the rest of the desktop. The
// zero value watches nothing.
type themeWatch struct {
	path string    // the colors.toml being followed; "" when not following
	mod  time.Time // its mtime when last applied
}

// themeTickMsg drives the poll.
type themeTickMsg struct{}

// applyTheme applies the configured theme and returns the watch to keep, if
// any. An empty theme means "auto": the Omarchy palette on an Omarchy machine,
// the default palette elsewhere. "omarchy" asks for it explicitly; if the
// palette can't be read either way, the default palette is used rather than
// failing.
func applyTheme(name string) themeWatch {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "omarchy" || (name == "" && omarchy.Detect()) {
		if w, ok := applyOmarchyTheme(omarchy.ThemeColorsPath()); ok {
			return w
		}
	}
	styles.Apply(name)
	return themeWatch{}
}

// applyOmarchyTheme installs the palette at path and returns a watch on it.
func applyOmarchyTheme(path string) (themeWatch, bool) {
	if path == "" {
		return themeWatch{}, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return themeWatch{}, false
	}
	p, err := omarchy.ReadPalette(path)
	if err != nil {
		return themeWatch{}, false
	}
	styles.ApplyHex(styles.HexPalette{
		Accent:     p.Accent,
		Muted:      p.Muted,
		Text:       p.Foreground,
		Success:    p.Green,
		Danger:     p.Red,
		Warning:    p.Yellow,
		Background: p.Background,
	})
	return themeWatch{path: path, mod: info.ModTime()}, true
}

// tick schedules the next poll, or nothing when there's no theme to follow.
func (w themeWatch) tick() tea.Cmd {
	if w.path == "" {
		return nil
	}
	return tea.Tick(themePollInterval, func(time.Time) tea.Msg { return themeTickMsg{} })
}

// refresh re-applies the palette when the theme file changed. Omarchy swaps
// the whole theme directory in on `theme set`, so a changed mtime (or a file
// that briefly vanished mid-swap and came back) is the signal. A read that
// fails mid-swap keeps the current colors and tries again next tick.
func (w themeWatch) refresh() themeWatch {
	info, err := os.Stat(w.path)
	if err != nil || info.ModTime().Equal(w.mod) {
		return w
	}
	if next, ok := applyOmarchyTheme(w.path); ok {
		return next
	}
	return w
}
