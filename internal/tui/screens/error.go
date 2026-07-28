package screens

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// retrySpec describes the optional retry action an error screen offers: the
// key's help label and the command it fires. A zero retrySpec (nil cmd) means
// the screen shows no retry key — only back.
type retrySpec struct {
	label string
	cmd   tea.Cmd
}

// retryRescan re-runs detection from scratch. It is the retry offered on
// detection-side failures and the empty state, where "try again" means "scan
// again".
var retryRescan = retrySpec{label: "rescan", cmd: Replace(NewDetect())}

// altSpec is an optional secondary action an error screen can offer beyond
// back/retry — currently the "open a SQLite file" escape hatch on the
// no-containers empty state, so a Docker-less user is never dead-ended.
type altSpec struct {
	key   string
	label string
	cmd   tea.Cmd
}

// errorScreen is the single full-screen error renderer. Both docker and db
// failures flow through it: they share the Title/Detail/Hint shape, so the
// screen is driven by that text plus the typed kind is already baked into it by
// the classify functions. It never dead-ends — every instance offers Back, and
// most offer a retry.
type errorScreen struct {
	title  string
	detail string
	hint   string
	info   string // wrapped underlying error, revealed with [i]
	retry  retrySpec
	alts   []altSpec // optional secondary actions (empty = none)

	showInfo bool
}

// withAlt attaches an optional secondary action to an error screen (e.g. the
// SQLite escape hatch on the empty state). It appends, so a screen can offer more
// than one — the no-containers state offers both "open a SQLite file" and "set up
// a server". Kept as a builder so the shared constructors stay two-argument.
func (s errorScreen) withAlt(a altSpec) errorScreen {
	s.alts = append(s.alts, a)
	return s
}

// NewErrorFromDocker builds the error screen from a typed docker failure.
func NewErrorFromDocker(e *docker.DockerError, retry retrySpec) Screen {
	return errorScreen{
		title:  e.Title,
		detail: e.Detail,
		hint:   e.Hint,
		info:   errText(e.Err),
		retry:  retry,
	}
}

// NewErrorFromDB builds the error screen from a typed db failure (used for
// connection-time failures; query errors render inline in the query pane, not
// here).
func NewErrorFromDB(e *db.DBError, retry retrySpec) Screen {
	return errorScreen{
		title:  e.Title,
		detail: e.Detail,
		hint:   e.Hint,
		info:   errText(e.Err),
		retry:  retry,
	}
}

func (s errorScreen) Init() tea.Cmd { return nil }

func (s errorScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	switch key.String() {
	case "b", "esc":
		return s, Pop()
	case "r":
		if s.retry.cmd != nil {
			return s, s.retry.cmd
		}
	case "i":
		if s.info != "" {
			s.showInfo = !s.showInfo
		}
	default:
		for _, a := range s.alts {
			if a.cmd != nil && key.String() == a.key {
				return s, a.cmd
			}
		}
	}
	return s, nil
}

func (s errorScreen) View(width, height int) string {
	lines := []string{
		styles.ErrorTitle.Render(s.title),
		"",
		styles.Subtitle.Render(s.detail),
	}
	if s.hint != "" {
		lines = append(lines, "", styles.Hint.Render(s.hint))
	}
	if s.showInfo && s.info != "" {
		lines = append(lines, "", styles.Hint.Render(s.info))
	}
	box := styles.ErrorBox.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (s errorScreen) Help() []key.Binding {
	b := []key.Binding{Keys.Back}
	if s.retry.cmd != nil {
		b = append(b, key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", s.retry.label),
		))
	}
	if s.info != "" {
		b = append(b, Keys.Info)
	}
	for _, a := range s.alts {
		if a.cmd != nil {
			b = append(b, key.NewBinding(
				key.WithKeys(a.key),
				key.WithHelp(a.key, a.label),
			))
		}
	}
	return b
}

// errText renders a wrapped error for the [i] details view, or "" when there is
// nothing underlying to show.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
