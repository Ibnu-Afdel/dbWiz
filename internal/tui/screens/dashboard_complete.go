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

// engineFunctions is functionsFor's per-Kind table (v5 3.3): the handful of
// functions someone writes constantly on that engine and nowhere else —
// COALESCE/NOW everywhere, but DATE_FORMAT only means something on
// MySQL/MariaDB and STRFTIME only on SQLite. Same "common-case, not
// exhaustive" scope as sqlKeywords, kept under 15 entries per engine on
// purpose — this is completion, not a function reference.
var engineFunctions = map[db.Kind][]string{
	db.KindPostgres: {
		"COALESCE", "NULLIF", "NOW", "EXTRACT", "ARRAY_AGG", "STRING_AGG",
		"GENERATE_SERIES", "TO_CHAR", "TO_TIMESTAMP", "CURRENT_TIMESTAMP",
		"CURRENT_DATE",
	},
	db.KindMySQL: {
		"IFNULL", "COALESCE", "CONCAT", "DATE_FORMAT", "GROUP_CONCAT",
		"CURDATE", "CURTIME", "UNIX_TIMESTAMP", "NOW",
	},
	db.KindMariaDB: {
		"IFNULL", "COALESCE", "CONCAT", "DATE_FORMAT", "GROUP_CONCAT",
		"CURDATE", "CURTIME", "UNIX_TIMESTAMP", "NOW",
	},
	db.KindSQLite: {
		"COALESCE", "IFNULL", "DATETIME", "STRFTIME", "JULIANDAY",
		"GROUP_CONCAT", "RANDOM",
	},
}

// functionsFor returns kind's function candidates, or nil for an unknown kind
// — completionCandidates treats that as "nothing to add", not an error.
func functionsFor(kind db.Kind) []string { return engineFunctions[kind] }

// completeMax caps the completion list so a no-prefix trigger over a wide schema
// stays a scannable picker rather than a wall.
const completeMax = 200

// prefetchColumnsMax bounds how many tables the background schema prefetch
// (v5 3.1) will describe after one table-list load, the same "bound the
// worst case on a schema with hundreds of tables" reasoning as completeMax.
// A schema past the cap still completes correctly for any table it actually
// visits — cacheColumns keeps firing from browsing regardless — and
// openComplete's on-demand fallback (fetchColumnsForCompleteCmd) covers a
// qualified reference to a table prefetch didn't reach.
const prefetchColumnsMax = 200

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
// deduped and sorted: this database's tables, every cached table's columns,
// engine-aware functions, then SQL keywords. An empty prefix returns
// everything (capped) — a bare trigger is a "what's here?" list.
//
// This is the *unqualified* candidate list — every table's columns mixed
// together. openComplete calls columnCandidates instead once a `table.`
// qualifier narrows it to one table (v5 3.2).
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
	// Prefetch (v5 3.1) means this now normally covers the whole database, not
	// just tables actually browsed.
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
	if s.engine != nil {
		for _, fn := range functionsFor(s.engine.Kind()) {
			add(fn, "function")
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

// columnCandidates is completionCandidates narrowed to one table's columns —
// what a recognized `table.` qualifier resolves to (v5 3.2). No tables,
// functions, or keywords mixed in: the whole point of qualifying is picking
// between two tables that share a column name, so the list should hold only
// what could actually follow the dot.
func columnCandidates(columns []string, prefix string) []completeItem {
	lower := strings.ToLower(prefix)
	items := make([]completeItem, 0, len(columns))
	for _, c := range columns {
		if lower != "" && !strings.HasPrefix(strings.ToLower(c), lower) {
			continue
		}
		items = append(items, completeItem{text: c, kind: "column"})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].text < items[j].text })
	return items
}

// qualifierBefore looks for a `.` immediately before the word starting at
// wordStart and, if there is one, returns the word before *that* — the
// qualifier a `table.column` completion needs to resolve. wordStartCol
// already stops at `.` (it's classPunct), so the qualifier is otherwise
// silently dropped; this is what recovers it.
func qualifierBefore(line []rune, wordStart int) (string, bool) {
	if wordStart == 0 || line[wordStart-1] != '.' {
		return "", false
	}
	dot := wordStart - 1
	qStart := wordStartCol(line, dot)
	if qStart == dot {
		return "", false
	}
	return string(line[qStart:dot]), true
}

// findTableExact looks up name against the current database's tables,
// case-insensitively — how a typed qualifier gets matched to a real table
// name (v5 3.2). A qualifier that doesn't match anything (a schema prefix
// like `public.users`, a decimal's fractional part, ...) falls back to
// today's unqualified completion in openComplete rather than showing an
// empty box.
func (s dashboardScreen) findTableExact(name string) (db.Table, bool) {
	for _, t := range s.tables {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return db.Table{}, false
}

// openComplete opens the autocomplete picker for the word prefix under the
// cursor (ctrl+space). With no candidates it shows a brief notice instead of
// an empty box. It records the prefix span so accepting a candidate replaces
// exactly that text — never the qualifier before a `.`, when there is one.
//
// A qualifier that names a known table narrows the list to that table's
// columns (v5 3.2): if they're already cached (prefetch, v5 3.1, or an
// earlier browse/describe) the list opens immediately; otherwise this
// describes just that one table and reopens once it lands — a single round
// trip, not the whole-schema prefetch, so a table prefetch didn't reach yet
// still completes without a wait longer than it has to be.
func (s dashboardScreen) openComplete() (dashboardScreen, tea.Cmd) {
	lines := strings.Split(s.editor.Value(), "\n")
	l := s.editor.Line()
	col := s.editor.Column()
	var line string
	if l < len(lines) {
		line = lines[l]
	}
	runes := []rune(line)
	start := wordStartCol(runes, col)
	prefix := string(runes[start:col])
	s.completeLine, s.completeStart, s.completeEnd = l, start, col

	if qualifier, ok := qualifierBefore(runes, start); ok {
		if table, found := s.findTableExact(qualifier); found {
			if cols, cached := s.columnCache[table.Name]; cached {
				return s.showCompleteList(columnCandidates(cols, prefix))
			}
			return s, fetchColumnsForCompleteCmd(s.engine, s.currentDB, table.Name, prefix)
		}
	}

	return s.showCompleteList(s.completionCandidates(prefix))
}

// showCompleteList opens items in the completion picker, or — for the common
// "typed something with no match" case — a brief notice instead of an empty
// box. Shared by openComplete's immediate path and completeColumnsMsg's
// delayed one (v5 3.2), so both end up in the exact same state.
func (s dashboardScreen) showCompleteList(items []completeItem) (dashboardScreen, tea.Cmd) {
	if len(items) == 0 {
		s.notice, s.noticeErr = "No completions for this prefix.", false
		return s, nil
	}
	w := clamp(s.width-8, 20, 60)
	h := clamp(s.height-8, 5, 18)
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
