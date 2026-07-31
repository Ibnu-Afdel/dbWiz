package schema

import (
	"fmt"
	"sort"
	"strings"
)

// Status is what happened to a table or a column between two snapshots. Same is
// never reported — a Report carries only differences — but it exists so the
// comparison helpers can say "nothing changed" without a sentinel.
type Status int

const (
	Same Status = iota
	Added
	Removed
	Changed
)

func (s Status) String() string {
	switch s {
	case Same:
		return "same"
	case Added:
		return "added"
	case Removed:
		return "removed"
	case Changed:
		return "changed"
	}
	return "?"
}

// MarshalJSON writes a status as its name. `dbwiz schema diff --json` is meant
// to be read by a person as often as by a script, and "changed" survives a
// refactor of the constant order in a way that 3 does not.
func (s Status) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// marker is the one-character prefix the text report uses, in the diff(1)
// tradition so it reads without a legend.
func (s Status) marker() string {
	switch s {
	case Added:
		return "+"
	case Removed:
		return "-"
	case Changed:
		return "~"
	}
	return " "
}

// Attr is one changed property of a column, rendered as "was → now".
type Attr struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// ColumnDiff is one column's outcome within a changed table. Detail carries the
// column's full description for an added/removed column, and Attrs carries the
// specific properties that moved for a changed one.
type ColumnDiff struct {
	Column string `json:"column"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
	Attrs  []Attr `json:"attrs,omitempty"`
}

// TableDiff is one table's outcome. Columns is populated for a changed table;
// an added or removed table lists its columns as added/removed so the report
// shows what actually appeared or vanished.
type TableDiff struct {
	Table   string       `json:"table"`
	Status  Status       `json:"status"`
	Columns []ColumnDiff `json:"columns,omitempty"`
}

// Report is the whole comparison. Tables holds only differences, so an empty
// Tables means the two databases are structurally identical.
type Report struct {
	From    string      `json:"from"`
	To      string      `json:"to"`
	Warning string      `json:"warning,omitempty"`
	Tables  []TableDiff `json:"tables"`
}

// Identical reports whether the two snapshots have the same structure — the
// value `dbwiz schema diff` turns into its exit code.
func (r Report) Identical() bool { return len(r.Tables) == 0 }

// Counts totals the table-level outcomes, for the report's summary line.
func (r Report) Counts() (added, removed, changed int) {
	for _, t := range r.Tables {
		switch t.Status {
		case Added:
			added++
		case Removed:
			removed++
		case Changed:
			changed++
		}
	}
	return
}

// Diff compares two snapshots and reports what would have to change to turn
// `from` into `to`. It is pure: same inputs, same output, no I/O — which is what
// makes it safe to run from the TUI's Update path as well as the CLI.
//
// Tables are matched by qualified name and reported in sorted order. Within a
// changed table, columns are reported in `to`'s ordinal order (so an added
// column appears where it actually sits), with columns that only exist in
// `from` appended after, in `from`'s order.
//
// Column *position* alone is not treated as a change: reordering columns while
// keeping every definition identical is reported as no difference, because the
// noise of a "moved" line on every column after an insertion buries the real
// changes.
func Diff(from, to Snapshot) Report {
	r := Report{From: from.Label, To: to.Label}
	if from.Kind != to.Kind {
		r.Warning = fmt.Sprintf("comparing %s with %s — engines spell types differently, so expect type changes that aren't real", from.Kind, to.Kind)
	}

	fromTables := index(from)
	toTables := index(to)

	for _, name := range union(fromTables, toTables) {
		f, inFrom := fromTables[name]
		t, inTo := toTables[name]
		switch {
		case !inFrom:
			r.Tables = append(r.Tables, TableDiff{Table: name, Status: Added, Columns: allColumns(t, Added)})
		case !inTo:
			r.Tables = append(r.Tables, TableDiff{Table: name, Status: Removed, Columns: allColumns(f, Removed)})
		default:
			if cols := diffColumns(f, t); len(cols) > 0 {
				r.Tables = append(r.Tables, TableDiff{Table: name, Status: Changed, Columns: cols})
			}
		}
	}
	return r
}

// index maps a snapshot's tables by qualified name.
func index(s Snapshot) map[string]Table {
	m := make(map[string]Table, len(s.Tables))
	for _, t := range s.Tables {
		m[t.Qualified()] = t
	}
	return m
}

// union returns every table name in either side, sorted.
func union(a, b map[string]Table) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, m := range []map[string]Table{a, b} {
		for name := range m {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// allColumns lists every column of a wholly added or removed table under that
// same status, so the report shows the shape of what appeared or vanished.
func allColumns(t Table, status Status) []ColumnDiff {
	out := make([]ColumnDiff, 0, len(t.Columns))
	for _, c := range t.Columns {
		out = append(out, ColumnDiff{Column: c.Name, Status: status, Detail: describe(c)})
	}
	return out
}

// diffColumns compares the columns of two tables that exist on both sides.
func diffColumns(from, to Table) []ColumnDiff {
	var out []ColumnDiff
	for _, tc := range to.Columns {
		fc, ok := from.Column(tc.Name)
		if !ok {
			out = append(out, ColumnDiff{Column: tc.Name, Status: Added, Detail: describe(tc)})
			continue
		}
		if attrs := diffAttrs(fc, tc); len(attrs) > 0 {
			out = append(out, ColumnDiff{Column: tc.Name, Status: Changed, Attrs: attrs})
		}
	}
	for _, fc := range from.Columns {
		if _, ok := to.Column(fc.Name); !ok {
			out = append(out, ColumnDiff{Column: fc.Name, Status: Removed, Detail: describe(fc)})
		}
	}
	return out
}

// diffAttrs lists the properties that differ between two versions of a column.
func diffAttrs(from, to Column) []Attr {
	var out []Attr
	if from.Type != to.Type {
		out = append(out, Attr{"type", from.Type, to.Type})
	}
	if from.Nullable != to.Nullable {
		out = append(out, Attr{"nullable", nullability(from.Nullable), nullability(to.Nullable)})
	}
	if from.Key != to.Key {
		out = append(out, Attr{"key", keyLabel(from.Key), keyLabel(to.Key)})
	}
	return out
}

// describe renders a column the way a person would read it off a CREATE TABLE.
func describe(c Column) string {
	parts := []string{c.Type, nullability(c.Nullable)}
	if c.Key != "" {
		parts = append(parts, c.Key)
	}
	return strings.Join(parts, " ")
}

func nullability(nullable bool) string {
	if nullable {
		return "NULL"
	}
	return "NOT NULL"
}

// keyLabel names the absence of a key so a report never renders "PRI → ".
func keyLabel(key string) string {
	if key == "" {
		return "none"
	}
	return key
}

// Render turns a report into the plain-text form both the CLI and the TUI show.
// The header borrows diff(1)'s --- / +++ so it is recognisable at a glance, and
// every line is prefixed with its status marker rather than relying on colour —
// the output has to survive being piped to a file.
func Render(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", r.From, r.To)
	if r.Warning != "" {
		fmt.Fprintf(&b, "!   %s\n", r.Warning)
	}
	b.WriteString("\n")

	if r.Identical() {
		b.WriteString("No differences — both databases have the same structure.\n")
		return b.String()
	}

	for _, t := range r.Tables {
		fmt.Fprintf(&b, "%s table %s\n", t.Status.marker(), t.Table)
		width := columnWidth(t.Columns)
		for _, c := range t.Columns {
			fmt.Fprintf(&b, "  %s %-*s  %s\n", c.Status.marker(), width, c.Column, columnDetail(c))
		}
		b.WriteString("\n")
	}

	added, removed, changed := r.Counts()
	fmt.Fprintf(&b, "%s, %s, %s\n",
		plural(added, "table added", "tables added"),
		plural(removed, "table removed", "tables removed"),
		plural(changed, "table changed", "tables changed"))
	return b.String()
}

// columnDetail is the right-hand side of a column line: the full description for
// an added/removed column, or the list of properties that moved for a changed one.
func columnDetail(c ColumnDiff) string {
	if len(c.Attrs) == 0 {
		return c.Detail
	}
	parts := make([]string, 0, len(c.Attrs))
	for _, a := range c.Attrs {
		parts = append(parts, fmt.Sprintf("%s %s → %s", a.Name, a.From, a.To))
	}
	return strings.Join(parts, ", ")
}

// columnWidth is the longest column name in a table's diff, so the detail column
// lines up.
func columnWidth(cols []ColumnDiff) int {
	w := 0
	for _, c := range cols {
		if len(c.Column) > w {
			w = len(c.Column)
		}
	}
	return w
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
