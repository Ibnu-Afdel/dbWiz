// Package migrations answers "what migration is this database on?" from the
// database alone (v4 Phase 3).
//
// Every migration tool records what it has applied in an ordinary table. This
// package recognises those tables — by name and by column shape — and reads them
// back as a plain report. That is deliberately the *whole* extent of it: DBWiz
// never reads a project's migration files, never runs a migration tool, and
// never behaves differently because it thinks it is inside a Laravel or Rails
// app. Recognising a published table convention from the outside is the same
// class of act as reading DB_DATABASE out of a .env file, and creates a
// dependency in neither direction (D8).
//
// The consequence is worth stating in the report and is stated there: DBWiz can
// say what has been applied, and cannot say what is pending, because pending
// lives on disk where DBWiz is not looking.
//
// Dependency direction is tui/cmd → migrations → db. It never imports docker or
// tui, and like internal/schema and internal/explain before it, it needs no new
// Engine method.
package migrations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// DefaultLimit is how many of the most recent migrations a report shows when the
// caller doesn't say. Enough to recognise where a database is; short enough to
// read in one glance.
const DefaultLimit = 10

// Source is the narrow slice of db.Engine this package needs. Reads go through
// ExecMutation rather than Query because it is the method that selects the
// database first (Postgres reconnects its pool to it) — "the ledger in *that*
// database" only means something when the connection is pointed at it. The
// browser's filter and COUNT(*) reads use it the same way.
type Source interface {
	Kind() db.Kind
	ListTables(ctx context.Context, database string) ([]db.Table, error)
	DescribeTable(ctx context.Context, database, table string) ([]db.Column, error)
	ExecMutation(ctx context.Context, database, sql string) (db.Result, error)
}

// Entry is one recorded migration.
type Entry struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	At   string `json:"applied_at,omitempty"`
	// Trouble is set when the ledger records that this migration didn't apply
	// cleanly — dirty, failed, rolled back, never finished.
	Trouble string `json:"trouble,omitempty"`
}

// Ledger is one migration table found in a database, and what it says.
type Ledger struct {
	// Tool is the tool that owns this layout, empty when only the shape matched.
	Tool string `json:"tool,omitempty"`
	// Table is the table's own name, as the server spells it; Schema is set only
	// where the engine has schemas and the table isn't in the default one.
	Table  string `json:"table"`
	Schema string `json:"schema,omitempty"`
	// Guessed marks a ledger recognised by shape rather than by a known layout.
	Guessed bool `json:"guessed,omitempty"`
	// Pointer marks a ledger that records the single version the database is at,
	// rather than a history of everything applied.
	Pointer bool `json:"pointer,omitempty"`

	// Applied is the exact number of rows in the ledger.
	Applied int `json:"applied"`
	// Latest holds the most recent entries first, capped by the read's limit.
	Latest []Entry `json:"latest,omitempty"`

	// conv and the resolved column names are what Read needs; they are the
	// server's own spellings, not the catalogue's.
	conv    Convention
	idCol   string
	nameCol string
	atCol   string
	order   []string
	flags   []Flag
}

// Ref is the ledger's display name: "schema.table" where a schema applies.
func (l Ledger) Ref() string {
	if l.Schema == "" {
		return l.Table
	}
	return l.Schema + "." + l.Table
}

// Label names the tool, or says plainly that no known tool was recognised.
func (l Ledger) Label() string {
	if l.Tool == "" {
		return "unrecognised layout"
	}
	return l.Tool
}

// Trouble reports whether any entry read back didn't apply cleanly.
func (l Ledger) Trouble() bool {
	for _, e := range l.Latest {
		if e.Trouble != "" {
			return true
		}
	}
	return false
}

// Status is the whole answer for one database.
type Status struct {
	Database string   `json:"database,omitempty"`
	Engine   string   `json:"engine"`
	Ledgers  []Ledger `json:"ledgers"`
}

// Trouble reports whether any ledger recorded a migration that didn't apply
// cleanly — the condition `dbwiz migrations` exits non-zero on.
func (s Status) Trouble() bool {
	for _, l := range s.Ledgers {
		if l.Trouble() {
			return true
		}
	}
	return false
}

// Detect finds every migration ledger in database and reads each one's state.
// limit caps the entries read per ledger; zero takes DefaultLimit and a negative
// limit reads them all.
func Detect(ctx context.Context, src Source, database string, limit int) (Status, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	found, err := Find(ctx, src, database)
	if err != nil {
		return Status{}, err
	}
	status := Status{Database: database, Engine: src.Kind().String()}
	for _, l := range found {
		read, err := Read(ctx, src, database, l, limit)
		if err != nil {
			return Status{}, err
		}
		status.Ledgers = append(status.Ledgers, read)
	}
	return status, nil
}

// Find locates the migration ledgers in database without reading a single row —
// it costs one table listing plus a describe per candidate table. Keeping it
// separate from Read is what lets a caller ask the cheap question ("is anything
// managing this database?") on its own.
//
// The shape pass runs only when no known layout matched: a Laravel app with an
// unrelated data_migrations table should get a clean Laravel answer rather than
// two entries and a hedge.
func Find(ctx context.Context, src Source, database string) ([]Ledger, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tables, err := src.ListTables(ctx, database)
	if err != nil {
		return nil, fmt.Errorf("listing tables in %q: %w", database, err)
	}

	// MySQL reports the database as every table's schema; that's one namespace,
	// not a subdivision, so it is dropped exactly as internal/schema drops it.
	present := make(map[string]bool, len(tables))
	for _, t := range tables {
		present[strings.ToLower(t.Name)] = true
	}

	var known, shaped []Ledger
	for _, t := range tables {
		schemaName := t.Schema
		if schemaName == database {
			schemaName = ""
		}
		lower := strings.ToLower(t.Name)
		candidate := looksLikeLedger(lower)
		for _, c := range catalog {
			if c.Table == lower {
				candidate = true
				break
			}
		}
		if !candidate {
			continue
		}

		// The describe is always fully qualified where a schema exists, since two
		// schemas can hold a table of the same name and an unqualified describe
		// would merge their columns. What the ledger *keeps* is narrower: "public"
		// is the default search_path entry, so carrying it would only add noise to
		// the report and to the read, exactly as MySQL's database-as-schema does.
		ref := t.Name
		if schemaName != "" {
			ref = schemaName + "." + t.Name
		}
		keep := schemaName
		if keep == "public" {
			keep = ""
		}
		cols, err := src.DescribeTable(ctx, database, ref)
		if err != nil {
			return nil, fmt.Errorf("describing table %q in %q: %w", ref, database, err)
		}
		names := make([]string, 0, len(cols))
		for _, c := range cols {
			names = append(names, c.Name)
		}

		if conv, ok := matchCatalog(lower, names, present); ok {
			known = append(known, newLedger(conv, t.Name, keep, names, false))
			continue
		}
		if conv, ok := shapeConvention(t.Name, names); ok && looksLikeLedger(lower) {
			shaped = append(shaped, newLedger(conv, t.Name, keep, names, true))
		}
	}

	if len(known) > 0 {
		return known, nil
	}
	return shaped, nil
}

// matchCatalog returns the first catalogue entry describing this table, if any.
func matchCatalog(lowerTable string, cols []string, present map[string]bool) (Convention, bool) {
	for _, c := range catalog {
		if c.Table == lowerTable && c.matches(cols, present) {
			return c, true
		}
	}
	return Convention{}, false
}

// newLedger resolves a convention's column names against the table's real ones
// and drops what this table hasn't got — a catalogue entry may order by a column
// (goose's id) that a given server's version doesn't carry, and a ledger that
// can't be ordered is still worth counting.
func newLedger(c Convention, table, schema string, cols []string, guessed bool) Ledger {
	l := Ledger{
		Tool:    c.Tool,
		Table:   table,
		Schema:  schema,
		Guessed: guessed,
		Pointer: c.Pointer,
		conv:    c,
		idCol:   resolveColumn(cols, c.ID),
		nameCol: resolveColumn(cols, c.Name),
		atCol:   resolveColumn(cols, c.At),
	}
	if l.idCol == "" && len(cols) > 0 {
		l.idCol = cols[0] // never leave a report with nothing to show per row
	}
	for _, o := range c.Order {
		if real := resolveColumn(cols, o); real != "" {
			l.order = append(l.order, real)
		}
	}
	if len(l.order) == 0 && l.idCol != "" {
		l.order = []string{l.idCol}
	}
	for _, f := range c.Flags {
		if real := resolveColumn(cols, f.Column); real != "" {
			f.Column = real
			l.flags = append(l.flags, f)
		}
	}
	return l
}

// Read fills in a ledger's row count and its most recent entries.
//
// Two statements: an exact COUNT(*), then the newest rows by the convention's
// order columns. Counting separately rather than measuring the rows fetched is
// what keeps "42 applied, showing 10" honest without pulling 42 rows to find out.
func Read(ctx context.Context, src Source, database string, l Ledger, limit int) (Ledger, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit == 0 {
		limit = DefaultLimit
	}
	kind := src.Kind()
	ref := db.TableRef(kind, database, l.Schema, l.Table)

	count, err := src.ExecMutation(ctx, database, "SELECT COUNT(*) FROM "+ref)
	if err != nil {
		return l, fmt.Errorf("counting rows in %s: %w", l.Ref(), err)
	}
	l.Applied = int(firstInt(count))

	rows, err := src.ExecMutation(ctx, database, selectStatement(kind, ref, l.order, limit))
	if err != nil {
		return l, fmt.Errorf("reading %s: %w", l.Ref(), err)
	}
	l.Latest = l.entries(rows)
	return l, nil
}

// selectStatement builds the newest-first read. A ledger with no orderable column
// is read unordered rather than not at all — the count is still true, and the
// rows are still the ledger's.
func selectStatement(kind db.Kind, ref string, order []string, limit int) string {
	var b strings.Builder
	b.WriteString("SELECT * FROM ")
	b.WriteString(ref)
	if len(order) > 0 {
		parts := make([]string, len(order))
		for i, c := range order {
			parts[i] = db.QuoteIdent(kind, c) + " DESC"
		}
		b.WriteString(" ORDER BY ")
		b.WriteString(strings.Join(parts, ", "))
	}
	if limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", limit)
	}
	return b.String()
}

// entries turns the read rows into report entries, newest first.
func (l Ledger) entries(res db.Result) []Entry {
	at := index(res.Columns, l.atCol)
	id := index(res.Columns, l.idCol)
	name := index(res.Columns, l.nameCol)

	out := make([]Entry, 0, len(res.Rows))
	for _, row := range res.Rows {
		e := Entry{
			ID:   cellText(cell(row, id)),
			Name: cellText(cell(row, name)),
			At:   cellText(cell(row, at)),
		}
		if e.Name == e.ID {
			e.Name = "" // one column doing both jobs; don't print it twice
		}
		for _, f := range l.flags {
			if i := index(res.Columns, f.Column); i >= 0 && f.Bad(cell(row, i)) {
				e.Trouble = f.Says
				break
			}
		}
		out = append(out, e)
	}
	return out
}

// index finds a column by name (case-insensitively, since the driver may echo a
// different case than the catalog), returning -1 when absent or unasked-for.
func index(columns []string, name string) int {
	if name == "" {
		return -1
	}
	for i, c := range columns {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

// cell reads a row's cell by index, tolerating a short row or an absent column.
func cell(row []any, i int) any {
	if i < 0 || i >= len(row) {
		return nil
	}
	return row[i]
}

// firstInt reads the single number a COUNT(*) comes back as. The drivers differ
// on integer width and MySQL hands back a []byte, so every shape is accepted.
func firstInt(res db.Result) int64 {
	if len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
		return 0
	}
	switch t := res.Rows[0][0].(type) {
	case int64:
		return t
	case int32:
		return int64(t)
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case []byte:
		return parseInt(string(t))
	case string:
		return parseInt(t)
	default:
		return 0
	}
}

// parseInt reads a decimal integer, returning 0 for anything else.
func parseInt(s string) int64 {
	var n int64
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

// cellText renders a cell as report text. Timestamps arrive as time.Time from
// pgx and as text from the other drivers, so the one that is a real time is
// formatted to the minute — a ledger's seconds are noise.
func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	case time.Time:
		return t.Format("2006-01-02 15:04")
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(t)
	}
}

// --- the truthiness the row flags are written against -----------------------
//
// A boolean comes back as bool from pgx, as int64 from MySQL and SQLite (both
// store it as a small integer), and as []byte or string from some driver paths.
// The flags are written against these helpers rather than a type assertion so a
// dirty database reads as dirty on every engine.

// truthy reports whether a cell means "yes".
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case int64:
		return t != 0
	case int32:
		return t != 0
	case int:
		return t != 0
	case float64:
		return t != 0
	case []byte:
		return truthyText(string(t))
	case string:
		return truthyText(t)
	default:
		return false
	}
}

// truthyText reads the textual spellings of a boolean the drivers use.
func truthyText(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "t", "true", "y", "yes", "1", "\x01":
		return true
	default:
		return false
	}
}

// falsy is truthy's complement over a cell that is actually present — a NULL is
// "the tool never said", not "false", so it raises nothing.
func falsy(v any) bool { return v != nil && !truthy(v) }

// isNull reports a cell the ledger left empty.
func isNull(v any) bool { return v == nil }

// notNull reports a cell the ledger filled in.
func notNull(v any) bool { return v != nil }

// nonEmpty reports a cell holding actual text — an error column that was written
// to, as against one left blank.
func nonEmpty(v any) bool { return cellText(v) != "" }
