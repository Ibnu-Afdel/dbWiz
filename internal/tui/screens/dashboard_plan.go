package screens

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/explain"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// planPhase walks the plan viewer: capture, then read (v4 2.4).
type planPhase int

const (
	planRunning planPhase = iota
	planReport
)

// planState backs the plan overlay: which statement was explained, whether it was
// measured, and the rendered report once it arrives.
type planState struct {
	phase     planPhase
	statement string
	analyzed  bool
	lines     []string // the rendered report, one line per entry, for scrolling
	offset    int      // first visible line
	err       string
	seq       int
}

// planDoneMsg carries a finished plan back to the dashboard.
type planDoneMsg struct {
	seq    int
	report string
	err    string
}

// planRows is how many report lines the overlay shows at once; the plan scrolls
// inside the box rather than taking over the screen.
const planRows = 18

// planTimeout bounds a capture. A plain EXPLAIN is nearly instant, but a measured
// one runs the statement, and the overlay has no cancel key — so it fails on its
// own rather than wedging the dashboard.
const planTimeout = 2 * time.Minute

// startPlan explains the statement in the editor. Measuring it (analyze) runs it,
// which is why it is never the first thing that happens: ⌥p plans, and the report
// then offers [a] to measure.
func (s dashboardScreen) startPlan(analyze bool) (dashboardScreen, tea.Cmd) {
	statement := strings.TrimSpace(s.editor.Value())
	if statement == "" {
		s.notice, s.noticeErr = "Write a statement in the editor first — ⌥p then shows how the engine would run it.", true
		return s, nil
	}
	if s.querying {
		s.notice, s.noticeErr = "A statement is still running — wait for it to finish before asking for a plan.", true
		return s, nil
	}
	return s.launchPlan(statement, analyze)
}

// launchPlan starts a capture for statement, superseding any earlier one.
func (s dashboardScreen) launchPlan(statement string, analyze bool) (dashboardScreen, tea.Cmd) {
	s.planSeq++
	s.plan = planState{
		phase:     planRunning,
		statement: statement,
		analyzed:  analyze,
		seq:       s.planSeq,
	}
	s.mode = modePlan
	s.working = true
	return s, tea.Batch(s.spinner.Tick, planCmd(s.engine, statement, analyze, s.planSeq))
}

// updatePlan drives the overlay.
func (s dashboardScreen) updatePlan(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	if s.plan.phase != planReport {
		return s, nil // capturing: ignore keys until the plan lands
	}
	switch msg.String() {
	case "esc", "q":
		s.mode = modeBrowse
		return s, nil
	case "a":
		// Measure it: the same statement, run for real. A statement that writes is
		// refused by the explain package, and the refusal shows in this overlay.
		return s.launchPlan(s.plan.statement, true)
	case "up", "k":
		if s.plan.offset > 0 {
			s.plan.offset--
		}
		return s, nil
	case "down", "j":
		if s.plan.offset < len(s.plan.lines)-planRows {
			s.plan.offset++
		}
		return s, nil
	case "pgup":
		s.plan.offset = max(0, s.plan.offset-planRows)
		return s, nil
	case "pgdown":
		s.plan.offset = min(max(0, len(s.plan.lines)-planRows), s.plan.offset+planRows)
		return s, nil
	}
	return s, nil
}

// applyPlanDone stores a finished plan. A report for a capture the user already
// left — or one superseded by pressing [a] — is dropped, the same way a stale
// query reply is.
func (s dashboardScreen) applyPlanDone(msg planDoneMsg) (dashboardScreen, tea.Cmd) {
	if s.mode != modePlan || msg.seq != s.planSeq {
		return s, nil
	}
	s.working = false
	s.plan.phase = planReport
	s.plan.err = msg.err
	s.plan.offset = 0
	s.plan.lines = nil
	if msg.report != "" {
		s.plan.lines = strings.Split(strings.TrimRight(msg.report, "\n"), "\n")
	}
	return s, nil
}

// planCmd captures the plan off the Update goroutine.
//
// Unlike the schema comparison, this never moves the connection: an EXPLAIN is
// just a statement, so it runs through the same pool the editor uses and leaves
// the current database exactly where it was.
func planCmd(engine db.Engine, statement string, analyze bool, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), planTimeout)
		defer cancel()

		plan, err := explain.Explain(ctx, engine, statement, explain.Options{Analyze: analyze})
		if err != nil {
			return planDoneMsg{seq: seq, err: err.Error()}
		}
		return planDoneMsg{seq: seq, report: explain.Render(plan)}
	}
}

// planView renders the current phase.
func (s dashboardScreen) planView(width int) string {
	p := s.plan
	inner := clamp(width-8, 40, 110)
	var lines []string

	switch p.phase {
	case planRunning:
		what := "Planning"
		if p.analyzed {
			what = "Running and measuring"
		}
		lines = append(lines,
			styles.Title.Render(what+" the statement…"),
			"",
			s.spinner.View()+" "+fitLine(singleLine(p.statement), inner))

	case planReport:
		if p.err != "" {
			lines = append(lines,
				styles.ErrorTitle.Render("Couldn't plan that statement"),
				"",
				styles.DangerText.Width(inner).Render(p.err),
				"",
				styles.Hint.Render("esc close"))
			break
		}
		lines = append(lines, styles.Title.Render("Query plan"), "")
		end := min(len(p.lines), p.offset+planRows)
		for _, line := range p.lines[p.offset:end] {
			lines = append(lines, fitLine(line, inner))
		}
		hint := "esc close"
		if len(p.lines) > planRows {
			hint = "↑/↓ scroll · esc close"
		}
		if !p.analyzed {
			// The measured plan is the useful one; it isn't the default because it
			// runs the statement, so the offer is made here instead.
			hint = "a run it and measure · " + hint
		}
		lines = append(lines, "", styles.Hint.Render(hint))
	}

	return styles.OverlayBox.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// singleLine flattens a multi-line statement for the one-line "planning…" label.
func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
