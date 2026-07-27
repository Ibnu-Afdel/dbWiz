package screens

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

type (
	// sqliteOpenedMsg carries a live SQLite engine after a successful open.
	sqliteOpenedMsg struct {
		engine db.Engine
		target db.Target
		path   string
	}
	// sqliteErrMsg is a typed failure (bad path / not a SQLite file) shown
	// inline; the screen stays put so the user can fix the path.
	sqliteErrMsg struct{ err *db.DBError }
)

// sqliteOpenScreen takes a path to a SQLite file, expands ~, offers Tab
// completion and a recent-files list, and validates by actually opening the
// file through the engine. Failures render inline rather than routing to the
// error screen — a mistyped path is an expected event here.
type sqliteOpenScreen struct {
	input     textinput.Model
	recents   []string // recently opened files, persisted across sessions (Step 1.3)
	recentIdx int      // -1 while editing the input; else an index into recents
	errMsg    string
}

// NewSQLiteOpen builds the path-input screen, seeding the recent-files list from
// the persisted state cache so files opened in earlier sessions are one keypress
// away (Step 1.3).
func NewSQLiteOpen() Screen {
	in := textinput.New()
	in.Prompt = "Path: "
	in.Placeholder = "~/project/dev.sqlite"
	in.Focus()
	return sqliteOpenScreen{input: in, recents: state.Load().RecentSQLite, recentIdx: -1}
}

func (s sqliteOpenScreen) Init() tea.Cmd { return textinput.Blink }

// CapturesText is always true here: the whole screen is a path input, so a digit
// belongs in the path, never a tab switch. Satisfies screens.TextInputer.
func (s sqliteOpenScreen) CapturesText() bool { return true }

func (s sqliteOpenScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case sqliteOpenedMsg:
		rememberSQLite(msg.path) // persist last + recent across sessions (Step 1.2/1.3)
		return s, Push(NewDashboard(msg.engine, msg.target, docker.Container{
			Name:   filepath.Base(msg.path),
			Engine: docker.EngineUnknown,
		}))
	case sqliteErrMsg:
		s.errMsg = plainError(msg.err)
		return s, nil
	case tea.KeyPressMsg:
		return s.handleKey(msg)
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

func (s sqliteOpenScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return s, Pop()
	case "enter":
		path := strings.TrimSpace(s.input.Value())
		if path == "" {
			s.errMsg = "Enter a path to a SQLite file."
			return s, nil
		}
		s.errMsg = ""
		return s, openSQLiteCmd(path)
	case "tab":
		if c := completePath(s.input.Value()); c != "" {
			s.input.SetValue(c)
			s.input.CursorEnd()
		}
		return s, nil
	case "up":
		return s.cycleRecent(-1), nil
	case "down":
		return s.cycleRecent(+1), nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	s.recentIdx = -1 // typing returns focus to the input
	return s, cmd
}

// cycleRecent moves the highlight through the recent-files list and mirrors the
// selection into the input so Enter opens it.
func (s sqliteOpenScreen) cycleRecent(delta int) sqliteOpenScreen {
	if len(s.recents) == 0 {
		return s
	}
	s.recentIdx += delta
	switch {
	case s.recentIdx < 0:
		s.recentIdx = -1
		return s
	case s.recentIdx >= len(s.recents):
		s.recentIdx = len(s.recents) - 1
	}
	s.input.SetValue(s.recents[s.recentIdx])
	s.input.CursorEnd()
	return s
}

func (s sqliteOpenScreen) View(width, height int) string {
	lines := []string{
		styles.Title.Render("Open a SQLite file"),
		"",
		s.input.View(),
	}
	if s.errMsg != "" {
		lines = append(lines, "", styles.DangerText.Render(s.errMsg))
	}
	if len(s.recents) > 0 {
		lines = append(lines, "", styles.Hint.Render("Recent:"))
		for i, r := range s.recents {
			style := styles.Item
			marker := "  "
			if i == s.recentIdx {
				style = styles.Selected
				marker = styles.Selected.Render("▸ ")
			}
			lines = append(lines, marker+style.Render(r))
		}
	}
	lines = append(lines, "", styles.Hint.Render("tab complete · ↑/↓ recent · enter open · esc back"))
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return styles.Screen.Render(body)
}

func (s sqliteOpenScreen) Help() []key.Binding {
	return []key.Binding{Keys.Select, Keys.Back}
}

// openSQLiteCmd validates a path by opening it through the engine, off the
// Update goroutine. The engine's own magic-header check rejects non-SQLite
// files, surfacing as a typed DBError.
func openSQLiteCmd(path string) tea.Cmd {
	return func() tea.Msg {
		expanded := expandTilde(path)
		engine, err := db.New(db.KindSQLite)
		if err != nil {
			return sqliteErrMsg{err: asDBError(err)}
		}
		target := db.Target{Path: expanded}

		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()

		if err := engine.Connect(ctx, target); err != nil {
			_ = engine.Close()
			return sqliteErrMsg{err: asDBError(err)}
		}
		return sqliteOpenedMsg{engine: engine, target: target, path: expanded}
	}
}

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// completePath does basic Tab completion: it completes the current input to the
// longest common prefix of the matching directory entries, appending a slash
// when the single match is a directory. It returns "" when there's nothing to
// add.
func completePath(in string) string {
	if in == "" {
		return ""
	}
	p := expandTilde(in)
	dir := filepath.Dir(p)
	base := filepath.Base(p)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var matches []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), base) {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) == 0 {
		return ""
	}
	common := longestCommonPrefix(matches)
	completed := filepath.Join(dir, common)
	if len(matches) == 1 {
		if info, err := os.Stat(completed); err == nil && info.IsDir() {
			completed += string(os.PathSeparator)
		}
	}
	if completed == p {
		return ""
	}
	return completed
}

func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

// plainError pulls the user-facing text out of a typed DB error for inline
// display.
func plainError(e *db.DBError) string {
	if e.Detail != "" {
		return e.Detail
	}
	return e.Title
}
