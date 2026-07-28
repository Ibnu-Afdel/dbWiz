package screens

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/list"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// sqlKeywords is the small set of SQL words the editor's autocomplete offers
// alongside schema names (v2 2.5). It's intentionally common-case, not
// exhaustive — enough that the frequent statement shapes complete, without
// turning the picker into a language reference.
var sqlKeywords = []string{
	"SELECT", "FROM", "WHERE", "INSERT", "INTO", "VALUES", "UPDATE", "SET",
	"DELETE", "CREATE", "TABLE", "DROP", "ALTER", "INDEX", "VIEW", "JOIN",
	"INNER", "LEFT", "RIGHT", "OUTER", "ON", "GROUP", "BY", "ORDER", "HAVING",
	"LIMIT", "OFFSET", "DISTINCT", "AS", "AND", "OR", "NOT", "NULL", "IS",
	"IN", "LIKE", "BETWEEN", "COUNT", "SUM", "AVG", "MIN", "MAX", "ASC", "DESC",
}

// completeMax caps the completion list so a no-prefix trigger over a wide schema
// stays a scannable picker rather than a wall.
const completeMax = 200

// cacheColumns records a table's column names (from a row preview) for the
// editor's autocomplete. Browsing already fetched these, so caching them costs
// nothing extra (v2 2.5).
func (s *dashboardScreen) cacheColumns(table string, columns []string) {
	if table == "" || len(columns) == 0 {
		return
	}
	if s.columnCache == nil {
		s.columnCache = map[string][]string{}
	}
	s.columnCache[table] = append([]string(nil), columns...)
}

// cacheColumnDefs is cacheColumns for a describe result, pulling the names out of
// the richer column definitions.
func (s *dashboardScreen) cacheColumnDefs(table string, cols []db.Column) {
	if len(cols) == 0 {
		return
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	s.cacheColumns(table, names)
}

// completeItem adapts a candidate to the list's DefaultItem shape. Description
// tags where it came from (table, column, or keyword) so an ambiguous name reads
// clearly.
type completeItem struct {
	text string
	kind string
}

func (i completeItem) Title() string       { return i.text }
func (i completeItem) FilterValue() string { return i.text }
func (i completeItem) Description() string { return i.kind }

// completeResult is what a key press did to the completion picker.
type completeResult int

const (
	completePending  completeResult = iota // handled internally (nav / filter typing)
	completeCanceled                       // esc
	completeAccepted                       // enter — insert the highlighted candidate
)

// completeModel is the schema-aware autocomplete picker (v2 2.5), wrapping
// bubbles/list like the other overlays so it reuses the same nav and fuzzy
// filter. It's built pre-filtered to the word prefix under the cursor.
type completeModel struct {
	list list.Model
}

func newCompleteModel(items []completeItem, width, height int) completeModel {
	li := make([]list.Item, len(items))
	for i, it := range items {
		li[i] = it
	}
	l := list.New(li, list.NewDefaultDelegate(), width, height)
	l.Title = "Complete"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	return completeModel{list: l}
}

func (m completeModel) update(msg tea.KeyPressMsg) (completeModel, completeResult, tea.Cmd) {
	if !m.list.SettingFilter() {
		switch msg.String() {
		case "esc":
			return m, completeCanceled, nil
		case "enter", "tab":
			return m, completeAccepted, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, completePending, cmd
}

func (m completeModel) selected() (completeItem, bool) {
	if it, ok := m.list.SelectedItem().(completeItem); ok {
		return it, true
	}
	return completeItem{}, false
}

func (m completeModel) settingFilter() bool { return m.list.SettingFilter() }

func (m completeModel) View(width int) string {
	return styles.Screen.Render(m.list.View())
}

// completionCandidates gathers the identifiers matching prefix (case-insensitive),
// deduped and sorted: this database's tables, every cached table's columns, then
// SQL keywords. An empty prefix returns everything (capped) — a bare trigger is a
// "what's here?" list.
func (s dashboardScreen) completionCandidates(prefix string) []completeItem {
	lower := strings.ToLower(prefix)
	seen := map[string]bool{}
	var items []completeItem

	add := func(text, kind string) {
		if text == "" || seen[text] {
			return
		}
		if lower != "" && !strings.HasPrefix(strings.ToLower(text), lower) {
			return
		}
		seen[text] = true
		items = append(items, completeItem{text: text, kind: kind})
	}

	for _, t := range s.tables {
		add(t.Name, "table")
	}
	// Cached columns, in a stable order so the list doesn't reshuffle per open.
	tables := make([]string, 0, len(s.columnCache))
	for t := range s.columnCache {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	for _, t := range tables {
		for _, c := range s.columnCache[t] {
			add(c, "column")
		}
	}
	for _, kw := range sqlKeywords {
		add(kw, "keyword")
	}

	sort.Slice(items, func(i, j int) bool { return items[i].text < items[j].text })
	if len(items) > completeMax {
		items = items[:completeMax]
	}
	return items
}

// openComplete opens the autocomplete picker for the word prefix under the cursor
// (ctrl+space). With no candidates it shows a brief notice instead of an empty
// box. It records the prefix span so accepting a candidate replaces exactly that
// text.
func (s dashboardScreen) openComplete() (dashboardScreen, tea.Cmd) {
	lines := strings.Split(s.editor.Value(), "\n")
	l := s.editor.Line()
	col := s.editor.Column()
	var line string
	if l < len(lines) {
		line = lines[l]
	}
	start := wordStartCol([]rune(line), col)
	prefix := string([]rune(line)[start:col])

	items := s.completionCandidates(prefix)
	if len(items) == 0 {
		s.notice, s.noticeErr = "No completions for this prefix.", false
		return s, nil
	}
	w := clamp(s.width-8, 20, 60)
	h := clamp(s.height-8, 5, 18)
	s.completeLine, s.completeStart, s.completeEnd = l, start, col
	s.completeList = newCompleteModel(items, w, h)
	s.mode = modeComplete
	return s, nil
}

// acceptCompletion replaces the recorded prefix span with word and puts the
// cursor after it. It keeps the editor focused and the vim mode untouched (so a
// completion mid-insert stays in insert), returning straight to editing.
func (s dashboardScreen) acceptCompletion(word string) (dashboardScreen, tea.Cmd) {
	lines := strings.Split(s.editor.Value(), "\n")
	if s.completeLine >= len(lines) {
		s.mode = modeBrowse
		return s, nil
	}
	runes := []rune(lines[s.completeLine])
	start := clamp(s.completeStart, 0, len(runes))
	end := clamp(s.completeEnd, start, len(runes))
	lines[s.completeLine] = string(runes[:start]) + word + string(runes[end:])
	s.editor.SetValue(strings.Join(lines, "\n"))
	moveCursorTo(&s.editor, s.completeLine, start+len([]rune(word)))
	s.mode = modeBrowse
	return s, s.editor.Focus()
}

// wordStartCol scans back from col over identifier characters to find where the
// current word begins, so autocomplete knows the prefix to match and replace.
func wordStartCol(line []rune, col int) int {
	if col > len(line) {
		col = len(line)
	}
	start := col
	for start > 0 && classOf(line[start-1]) == classWord {
		start--
	}
	return start
}
