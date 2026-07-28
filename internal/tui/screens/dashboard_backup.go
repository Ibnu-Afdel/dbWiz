package screens

import (
	"context"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/backup"
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// backupPhase walks the dump/restore flow: choose the action, enter a file path,
// run it, then show the outcome (v3 2.4).
type backupPhase int

const (
	backupChoose backupPhase = iota
	backupFile
	backupRunning
	backupDone
)

// backupState backs the dump/restore screen: which action, which database, the
// file path input, and the outcome.
type backupState struct {
	phase    backupPhase
	restore  bool // false = dump, true = restore
	database string
	input    textinput.Model
	result   string
	err      string
}

// backupDoneMsg reports a finished dump or restore: a success summary or an error.
type backupDoneMsg struct {
	summary string
	err     string
}

// startBackup opens the dump/restore screen for the database in context. It's a
// Docker-only, server-engine feature (it shells into the container's own tools),
// so SQLite and non-container targets get a plain notice instead.
func (s dashboardScreen) startBackup() (dashboardScreen, tea.Cmd) {
	if s.engine.Kind() == db.KindSQLite || s.container.Name == "" {
		s.notice, s.noticeErr = "Dump/restore needs a Docker Postgres or MySQL/MariaDB container.", true
		return s, nil
	}
	database := s.currentDB
	if s.focus == focusDatabases {
		if d, ok := s.selectedDatabase(); ok {
			database = d.Name
		}
	}
	if database == "" {
		s.notice, s.noticeErr = "Pick a database to back up first.", true
		return s, nil
	}
	s.backup = backupState{phase: backupChoose, database: database}
	s.mode = modeBackup
	return s, nil
}

// updateBackup drives the flow one phase at a time.
func (s dashboardScreen) updateBackup(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	switch s.backup.phase {
	case backupChoose:
		switch msg.String() {
		case "esc":
			s.mode = modeBrowse
			return s, nil
		case "up", "down", "tab":
			s.backup.restore = !s.backup.restore
			return s, nil
		case "enter":
			return s.backupToFile()
		}
		return s, nil

	case backupFile:
		switch msg.String() {
		case "esc":
			s.mode = modeBrowse
			return s, nil
		case "enter":
			return s.backupRun()
		}
		var cmd tea.Cmd
		s.backup.input, cmd = s.backup.input.Update(msg)
		return s, cmd

	case backupDone:
		s.mode = modeBrowse // any key closes the result
		return s, nil
	}
	return s, nil // running: ignore keys until it finishes
}

// backupToFile advances to the file-path step, prefilling a sensible dump
// filename or an empty restore path.
func (s dashboardScreen) backupToFile() (dashboardScreen, tea.Cmd) {
	in := textinput.New()
	in.Prompt = "› "
	if s.backup.restore {
		in.Placeholder = "path to a .sql dump to restore"
	} else {
		in.SetValue(defaultDumpName(s.backup.database))
	}
	cmd := in.Focus()
	in.CursorEnd()
	s.backup.input = in
	s.backup.phase = backupFile
	return s, cmd
}

// backupRun kicks off the dump or restore against the resolved path.
func (s dashboardScreen) backupRun() (dashboardScreen, tea.Cmd) {
	path := expandTilde(strings.TrimSpace(s.backup.input.Value()))
	if path == "" {
		return s, nil
	}
	s.backup.phase = backupRunning
	s.working = true
	kind := s.engine.Kind()
	run := dumpRunCmd
	if s.backup.restore {
		run = restoreRunCmd
	}
	return s, tea.Batch(s.spinner.Tick,
		run(s.container.Name, kind, s.target.User, s.target.Password, s.backup.database, path))
}

// applyBackupDone records the outcome and shows it until the user dismisses it.
func (s dashboardScreen) applyBackupDone(msg backupDoneMsg) (dashboardScreen, tea.Cmd) {
	if s.mode != modeBackup {
		return s, nil
	}
	s.working = false
	s.backup.phase = backupDone
	s.backup.result, s.backup.err = msg.summary, msg.err
	return s, nil
}

// dumpRunCmd streams pg_dump/mysqldump output into path.
func dumpRunCmd(container string, kind db.Kind, user, password, database, path string) tea.Cmd {
	return func() tea.Msg {
		c, err := backup.Dump(kind, user, database, password)
		if err != nil {
			return backupDoneMsg{err: err.Error()}
		}
		f, err := os.Create(path)
		if err != nil {
			return backupDoneMsg{err: err.Error()}
		}
		defer f.Close()
		if err := docker.Exec(context.Background(), docker.ExecOptions{
			Container: container, Env: c.Env, Stdout: f, Args: c.Args,
		}); err != nil {
			return backupDoneMsg{err: err.Error()}
		}
		if err := f.Close(); err != nil {
			return backupDoneMsg{err: err.Error()}
		}
		return backupDoneMsg{summary: "Dumped " + database + " → " + path}
	}
}

// restoreRunCmd streams a dump file into psql/mysql over stdin.
func restoreRunCmd(container string, kind db.Kind, user, password, database, path string) tea.Cmd {
	return func() tea.Msg {
		c, err := backup.Restore(kind, user, database, password)
		if err != nil {
			return backupDoneMsg{err: err.Error()}
		}
		f, err := os.Open(path)
		if err != nil {
			return backupDoneMsg{err: err.Error()}
		}
		defer f.Close()
		if err := docker.Exec(context.Background(), docker.ExecOptions{
			Container: container, Env: c.Env, Stdin: f, Args: c.Args,
		}); err != nil {
			return backupDoneMsg{err: err.Error()}
		}
		return backupDoneMsg{summary: "Restored " + path + " → " + database}
	}
}

// backupView renders the current phase.
func (s dashboardScreen) backupView(width int) string {
	b := s.backup
	inner := clamp(width-8, 24, 100)
	var lines []string

	switch b.phase {
	case backupChoose:
		lines = append(lines,
			styles.Title.Render("Back up "+b.database),
			"",
			choiceLine("Dump to a file", !b.restore),
			choiceLine("Restore from a file (overwrites data)", b.restore),
			"",
			styles.Hint.Render("↑/↓ choose · enter next · esc cancel"))
	case backupFile:
		verb := "Dump to"
		if b.restore {
			verb = "Restore from"
		}
		lines = append(lines,
			styles.Title.Render(verb+" file — "+b.database),
			"",
			b.input.View(),
			"",
			styles.Hint.Render("enter run · esc cancel"))
	case backupRunning:
		action := "Dumping"
		if b.restore {
			action = "Restoring"
		}
		lines = append(lines, styles.Title.Render(action+" "+b.database+"…"), "", s.spinner.View()+" working — this can take a while on a large database")
	case backupDone:
		if b.err != "" {
			lines = append(lines, styles.ErrorTitle.Render("Backup failed"), "", styles.DangerText.Width(inner).Render(b.err))
		} else {
			lines = append(lines, styles.SuccessText.Render("✓ "+b.result))
		}
		lines = append(lines, "", styles.Hint.Render("press any key to close"))
	}
	return styles.OverlayBox.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// choiceLine renders one selectable option, marked when selected.
func choiceLine(label string, selected bool) string {
	if selected {
		return styles.Selected.Render("▸ " + label)
	}
	return styles.Item.Render("  " + label)
}

// defaultDumpName suggests a timestamped filename for a dump so successive dumps
// don't clobber each other.
func defaultDumpName(database string) string {
	return database + "-" + time.Now().Format("20060102-150405") + ".sql"
}
