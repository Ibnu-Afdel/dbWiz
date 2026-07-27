package screens

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// detectScreen shows a spinner while an async docker scan runs. It owns no
// result state: the moment the scan returns it replaces itself with the home
// menu (or the error screen), so Back never lands on a stale spinner.
type detectScreen struct {
	spinner spinner.Model
}

// NewDetect builds the first screen the app shows. The root starts here.
func NewDetect() Screen {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	s.Style = styles.Selected
	return detectScreen{spinner: s}
}

func (s detectScreen) Init() tea.Cmd {
	return tea.Batch(s.spinner.Tick, detectCmd())
}

func (s detectScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case detectDoneMsg:
		if len(msg.containers) == 0 {
			// docker succeeded but nothing matched — the empty-state screen,
			// which offers rescan and the SQLite route so it's never a dead end.
			return s, Replace(emptyStateError())
		}
		return s, Replace(NewHomeContinuing(msg.containers))
	case detectErrMsg:
		return s, Replace(NewErrorFromDocker(msg.err, retryRescan))
	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s detectScreen) View(width, height int) string {
	body := s.spinner.View() + " " + styles.Subtitle.Render("Scanning for database containers…")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}

func (s detectScreen) Help() []key.Binding { return nil }

// emptyStateError is the no-containers screen: the plain-language empty state
// plus a rescan retry and an [o] escape hatch to open a SQLite file, so a user
// without any Docker databases is never dead-ended.
func emptyStateError() Screen {
	return NewErrorFromDocker(docker.ErrNoContainers(), retryRescan).(errorScreen).
		withAlt(altSpec{key: "o", label: "open a SQLite file", cmd: Push(NewSQLiteOpen())})
}
