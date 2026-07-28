package screens

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/config"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// homeChoice is a menu row on the home screen. The constant order is the
// canonical on-screen order; which rows actually appear is decided per-visit in
// the choices slice (the continue row shows only when there's something to
// resume).
type homeChoice int

const (
	choiceContinue homeChoice = iota
	choiceExisting
	choiceSaved
	choiceManual
	choiceCreate
	choiceSetup
	choiceSQLite
	choiceDoctor
	choiceRescan
)

func (c homeChoice) label() string {
	switch c {
	case choiceContinue:
		return "Continue where you left off"
	case choiceExisting:
		return "Use an existing database"
	case choiceSaved:
		return "Connect to a saved target…"
	case choiceManual:
		return "Connect to a database by host/port…"
	case choiceCreate:
		return "Create a new database…"
	case choiceSetup:
		return "Set up a new database server…"
	case choiceSQLite:
		return "Open a SQLite file…"
	case choiceDoctor:
		return "Run a health check…"
	case choiceRescan:
		return "Rescan containers"
	}
	return ""
}

// continueTarget is the resolved "pick up where you left off" option: the
// remembered target and the command that reconnects to it.
type continueTarget struct {
	target state.Target
	route  tea.Cmd
}

// homeScreen is the route-classifying menu shown after detection. It lists the
// detected containers inline and can start a stopped one without leaving the
// screen (async, with a spinner), then re-scans. When the last session's target
// is still reachable it offers a continue row at the top.
type homeScreen struct {
	containers []docker.Container
	cont       *continueTarget
	saved      []config.ManualTarget // saved manual targets from config (v2 3.3)
	choices    []homeChoice
	cursor     int

	starting bool // a docker start is in flight
	spinner  spinner.Model
}

// NewHome builds the home menu from a completed scan, without a continue row.
func NewHome(containers []docker.Container) Screen {
	return newHome(containers, nil)
}

// NewHomeContinuing is NewHome that also resolves the last-used target from the
// state cache and, if it's still reachable, offers it as the first menu row —
// the production entry point the detect screen uses (v2 Step 1.2).
func NewHomeContinuing(containers []docker.Container) Screen {
	return newHome(containers, resolveContinue(containers))
}

func newHome(containers []docker.Container, cont *continueTarget) Screen {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = styles.Selected
	saved := SavedTargets()
	choices := []homeChoice{}
	if cont != nil {
		choices = append(choices, choiceContinue)
	}
	choices = append(choices, choiceExisting)
	// The saved-target row appears only when config actually defines one, so the
	// menu stays lean for the Docker-only case (v2 3.3).
	if len(saved) > 0 {
		choices = append(choices, choiceSaved)
	}
	// Manual host/port entry is always available — it's the escape hatch for any
	// database DBWiz can't discover through Docker (v3 3.1).
	choices = append(choices, choiceManual)
	choices = append(choices, choiceCreate, choiceSetup, choiceSQLite)
	// The doctor row appears only when there's a container to diagnose (v3 1.3).
	if len(containers) > 0 {
		choices = append(choices, choiceDoctor)
	}
	choices = append(choices, choiceRescan)
	return homeScreen{containers: containers, cont: cont, saved: saved, choices: choices, spinner: sp}
}

// resolveContinue turns the remembered last target into a selectable option, but
// only when it's actually reachable now: a Docker target must be present and
// running in this scan; a SQLite target's file must still exist. Otherwise the
// row is omitted rather than offering a dead link.
func resolveContinue(containers []docker.Container) *continueTarget {
	last := state.Load().Last
	if last == nil {
		return nil
	}
	switch last.Kind {
	case state.KindDocker:
		for _, c := range containers {
			if c.Name == last.Container && c.State == docker.StateRunning {
				return &continueTarget{target: *last, route: Push(NewConnect(c))}
			}
		}
	case state.KindSQLite:
		if _, err := os.Stat(last.Path); err == nil {
			return &continueTarget{target: *last, route: openSQLiteCmd(last.Path)}
		}
	}
	return nil
}

func (s homeScreen) Init() tea.Cmd { return nil }

func (s homeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case startedMsg:
		// Re-scan so the freshly started container shows as running.
		return s, Replace(NewDetect())
	case startErrMsg:
		return s, Replace(NewErrorFromDocker(msg.err, retryRescan))
	case spinner.TickMsg:
		if !s.starting {
			return s, nil
		}
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd
	case tea.KeyPressMsg:
		return s.handleKey(msg)
	}
	return s, nil
}

func (s homeScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if s.starting {
		return s, nil // ignore input while a start is in flight
	}
	switch {
	case key.Matches(msg, Keys.Quit) || msg.String() == "q":
		return s, Quit()
	case key.Matches(msg, Keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil
	case key.Matches(msg, Keys.Down):
		if s.cursor < len(s.choices)-1 {
			s.cursor++
		}
		return s, nil
	case key.Matches(msg, Keys.Rescan):
		return s, Replace(NewDetect())
	case key.Matches(msg, Keys.Start):
		return s.startStopped()
	case key.Matches(msg, Keys.Select):
		return s.choose()
	}
	return s, nil
}

// choose acts on the highlighted menu row.
func (s homeScreen) choose() (Screen, tea.Cmd) {
	switch s.choices[s.cursor] {
	case choiceContinue:
		if s.cont != nil {
			return s, s.cont.route
		}
	case choiceExisting:
		return s, existingRoute(s.containers)
	case choiceSaved:
		return s, savedRoute(s.saved)
	case choiceManual:
		return s, Push(NewManualConnect())
	case choiceCreate:
		return s, createRoute(s.containers)
	case choiceSetup:
		return s, Push(NewSetup(s.containers))
	case choiceSQLite:
		return s, Push(NewSQLiteOpen())
	case choiceDoctor:
		return s, doctorRoute(s.containers)
	case choiceRescan:
		return s, Replace(NewDetect())
	}
	return s, nil
}

// startStopped kicks off starting the first stopped container. With none
// stopped it does nothing (the key isn't advertised in that case).
func (s homeScreen) startStopped() (Screen, tea.Cmd) {
	c, ok := firstStopped(s.containers)
	if !ok {
		return s, nil
	}
	s.starting = true
	return s, tea.Batch(s.spinner.Tick, startCmd(c))
}

func (s homeScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(styles.Title.Render("DBWiz"))
	b.WriteString("\n")
	b.WriteString(styles.Hint.Render(rule(width)))
	b.WriteString("\n")

	for i, c := range s.choices {
		b.WriteString(s.renderChoice(i, c))
		b.WriteString("\n")
	}

	b.WriteString(styles.Hint.Render(rule(width)))
	b.WriteString("\n")
	b.WriteString(s.renderInventory())

	if s.starting {
		if c, ok := firstStopped(s.containers); ok {
			b.WriteString("\n\n")
			b.WriteString(s.spinner.View())
			b.WriteString(" ")
			b.WriteString(styles.Subtitle.Render("Starting " + c.Name + "…"))
		}
	}
	return styles.Screen.Render(b.String())
}

func (s homeScreen) renderChoice(i int, c homeChoice) string {
	cursor := "  "
	label := styles.Item.Render(c.label())
	if i == s.cursor {
		cursor = styles.Selected.Render("▸ ")
		label = styles.Selected.Render(c.label())
	}
	return cursor + padRight(label, 32) + styles.Hint.Render(s.choiceDetail(c))
}

// choiceDetail is the dimmed parenthetical after each menu row.
func (s homeScreen) choiceDetail(c homeChoice) string {
	switch c {
	case choiceContinue:
		if s.cont != nil {
			return "(" + s.cont.target.Label() + ")"
		}
	case choiceExisting:
		running := countRunning(s.containers)
		if running == 0 {
			return "(no running containers — start one below)"
		}
		return fmt.Sprintf("(%d running)", running)
	case choiceSaved:
		return fmt.Sprintf("(%d saved)", len(s.saved))
	case choiceManual:
		return "(any reachable Postgres/MySQL — no Docker)"
	case choiceCreate:
		return "(create + connect in one flow)"
	case choiceSetup:
		return "(pull + run a container, Omarchy-style)"
	case choiceSQLite:
		return "(path input / recent)"
	case choiceDoctor:
		return "(daemon · port · auth · query)"
	case choiceRescan:
		return "(r)"
	}
	return ""
}

// renderInventory lists every detected container, running ones bold and stopped
// ones dimmed with the start hint, plus an Omarchy badge where it applies.
func (s homeScreen) renderInventory() string {
	if len(s.containers) == 0 {
		return styles.Hint.Render("No containers.")
	}
	var lines []string
	stopped := 0
	for _, c := range s.containers {
		var row string
		badge := ""
		if c.Source == docker.SourceOmarchy {
			badge = " " + styles.Badge.Render("[omarchy]")
		}
		meta := fmt.Sprintf("%s · %s", c.Engine, portLabel(c.HostPort))
		if c.State == docker.StateRunning {
			row = styles.Running.Render("● "+c.Name) + styles.Hint.Render("  "+meta) + badge
		} else {
			stopped++
			row = styles.Stopped.Render("○ "+c.Name) + styles.Hint.Render("  "+meta+"  [s] start") + badge
		}
		lines = append(lines, row)
	}
	summary := fmt.Sprintf("%d container(s) found · %d stopped", len(s.containers), stopped)
	lines = append(lines, "", styles.Hint.Render(summary))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (s homeScreen) Help() []key.Binding {
	b := []key.Binding{Keys.Up, Keys.Down, Keys.Select}
	if _, ok := firstStopped(s.containers); ok {
		b = append(b, Keys.Start)
	}
	b = append(b, Keys.Rescan, key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")))
	return b
}

// existingRoute picks the right next screen for the "use existing" choice:
// nothing running is a friendly error, exactly one running skips straight to
// connecting, and 2+ opens the picker.
func existingRoute(containers []docker.Container) tea.Cmd {
	running := runningContainers(containers)
	switch len(running) {
	case 0:
		return Push(NewErrorFromDocker(&docker.DockerError{
			Kind:   docker.DockerErrNoContainers,
			Title:  "No running database containers",
			Detail: "There's nothing to connect to yet. Start a stopped container from the home screen, or rescan.",
			Hint:   "press [b] to go back and [s] to start one",
		}, retrySpec{}))
	case 1:
		return Push(NewConnect(running[0]))
	default:
		return Push(NewPicker(running))
	}
}

// createRoute is existingRoute for the "create new database" choice: same
// container selection, but the connection lands on the create-database form so
// the user goes from launch to a fresh database in a few keystrokes (Step 6.7).
func createRoute(containers []docker.Container) tea.Cmd {
	running := runningContainers(containers)
	switch len(running) {
	case 0:
		return Push(NewErrorFromDocker(&docker.DockerError{
			Kind:   docker.DockerErrNoContainers,
			Title:  "No running database containers",
			Detail: "Start a database container first, then create a new database inside it.",
			Hint:   "press [b] to go back and [s] to start one",
		}, retrySpec{}))
	case 1:
		return Push(NewConnectCreating(running[0]))
	default:
		return Push(NewPickerCreating(running))
	}
}

// doctorRoute picks which container the health check runs against: the pinned
// last-used one if it's in this scan, otherwise the first running container,
// otherwise the first detected (the doctor works on a stopped one too — reporting
// that is the point). With nothing detected it's a friendly error.
func doctorRoute(containers []docker.Container) tea.Cmd {
	c, ok := primaryContainer(containers)
	if !ok {
		return Push(NewErrorFromDocker(&docker.DockerError{
			Kind:   docker.DockerErrNoContainers,
			Title:  "Nothing to check",
			Detail: "No database container was detected, so there's nothing to run a health check against.",
			Hint:   "press [b] to go back, or set up a server first",
		}, retrySpec{}))
	}
	return Push(NewDoctor(c))
}

// primaryContainer chooses the most likely target of a whole-inventory action:
// the pinned last-used container if present, else the first running one, else the
// first detected.
func primaryContainer(cs []docker.Container) (docker.Container, bool) {
	if len(cs) == 0 {
		return docker.Container{}, false
	}
	if last := state.Load().Last; last != nil && last.Kind == state.KindDocker {
		for _, c := range cs {
			if c.Name == last.Container {
				return c, true
			}
		}
	}
	for _, c := range cs {
		if c.State == docker.StateRunning {
			return c, true
		}
	}
	return cs[0], true
}

// --- small helpers over the container slice ---

func runningContainers(cs []docker.Container) []docker.Container {
	var out []docker.Container
	for _, c := range cs {
		if c.State == docker.StateRunning {
			out = append(out, c)
		}
	}
	return out
}

func countRunning(cs []docker.Container) int { return len(runningContainers(cs)) }

func firstStopped(cs []docker.Container) (docker.Container, bool) {
	for _, c := range cs {
		if c.State == docker.StateStopped {
			return c, true
		}
	}
	return docker.Container{}, false
}

func portLabel(port int) string {
	if port == 0 {
		return "no host port"
	}
	return fmt.Sprintf(":%d", port)
}

// rule draws a horizontal divider that fits the screen, accounting for the
// screen's horizontal padding.
func rule(width int) string {
	return strings.Repeat("─", max(width-4, 8))
}

// padRight pads a (possibly styled) string with spaces to a visible width.
func padRight(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
