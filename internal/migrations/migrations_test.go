package migrations

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// --- a fake engine ----------------------------------------------------------
//
// Source is three methods wide, which is what makes the whole package testable
// against a handful of literal tables instead of a live server. The fake answers
// them out of a map and records the SQL it was asked to run, so the statements
// this package composes are asserted directly.

type fakeTable struct {
	schema string
	cols   []string
	// rows are held newest-first, matching the order the read asks for.
	rows [][]any
}

type fakeSource struct {
	kind   db.Kind
	tables map[string]fakeTable
	ran    []string
	fail   error
}

func (f *fakeSource) Kind() db.Kind { return f.kind }

func (f *fakeSource) ListTables(_ context.Context, database string) ([]db.Table, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	var out []db.Table
	for name, t := range f.tables {
		schema := t.schema
		if schema == "" && (f.kind == db.KindMySQL || f.kind == db.KindMariaDB) {
			schema = database // MySQL reports the database as every table's schema
		}
		out = append(out, db.Table{Name: name, Schema: schema})
	}
	return out, nil
}

func (f *fakeSource) DescribeTable(_ context.Context, _, table string) ([]db.Column, error) {
	name := table
	if i := strings.Index(name, "."); i > 0 {
		name = name[i+1:]
	}
	t, ok := f.tables[name]
	if !ok {
		return nil, fmt.Errorf("no such table %q", table)
	}
	cols := make([]db.Column, 0, len(t.cols))
	for _, c := range t.cols {
		cols = append(cols, db.Column{Name: c, Type: "text", Nullable: true})
	}
	return cols, nil
}

// ExecMutation answers the two statements Read composes: an exact count, and a
// newest-first read. It honours the LIMIT so the cap can be asserted, and it
// looks the table up by the *quoted* reference the builder produced, which is how
// a mis-qualified read shows up as a failure here.
func (f *fakeSource) ExecMutation(_ context.Context, _, sql string) (db.Result, error) {
	f.ran = append(f.ran, sql)
	t, ok := f.tableFor(sql)
	if !ok {
		return db.Result{}, fmt.Errorf("fake: no table in %q", sql)
	}
	if strings.HasPrefix(sql, "SELECT COUNT(*)") {
		return db.Result{Columns: []string{"count"}, Rows: [][]any{{int64(len(t.rows))}}}, nil
	}
	rows := t.rows
	if i := strings.Index(sql, " LIMIT "); i >= 0 {
		n, err := strconv.Atoi(strings.TrimSpace(sql[i+len(" LIMIT "):]))
		if err != nil {
			return db.Result{}, err
		}
		if n < len(rows) {
			rows = rows[:n]
		}
	}
	return db.Result{Columns: t.cols, Rows: rows}, nil
}

// tableFor finds which table a composed statement reads, by matching the name
// inside the quoting the db layer applied.
func (f *fakeSource) tableFor(sql string) (fakeTable, bool) {
	for name, t := range f.tables {
		if strings.Contains(sql, name) {
			return t, true
		}
	}
	return fakeTable{}, false
}

func (f *fakeSource) lastSelect() string {
	for i := len(f.ran) - 1; i >= 0; i-- {
		if !strings.HasPrefix(f.ran[i], "SELECT COUNT(*)") {
			return f.ran[i]
		}
	}
	return ""
}

// laravelSource is the common case: a Laravel-managed Postgres database with
// three real tables alongside the ledger.
func laravelSource() *fakeSource {
	return &fakeSource{
		kind: db.KindPostgres,
		tables: map[string]fakeTable{
			"users":  {cols: []string{"id", "email"}},
			"orders": {cols: []string{"id", "total"}},
			"migrations": {
				cols: []string{"id", "migration", "batch"},
				rows: [][]any{
					{int64(3), "2026_07_14_120000_create_orders_table", int64(2)},
					{int64(2), "2024_10_12_100000_create_password_resets", int64(1)},
					{int64(1), "2024_10_12_000000_create_users_table", int64(1)},
				},
			},
		},
	}
}

// --- detection --------------------------------------------------------------

func TestFindRecognisesLaravel(t *testing.T) {
	found, err := Find(context.Background(), laravelSource(), "shop")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d ledgers, want 1: %+v", len(found), found)
	}
	l := found[0]
	if l.Tool != "Laravel" || l.Table != "migrations" {
		t.Errorf("got %q in %q, want Laravel in migrations", l.Tool, l.Table)
	}
	if l.Guessed || l.Pointer {
		t.Errorf("a known history ledger should be neither guessed nor a pointer: %+v", l)
	}
	if l.idCol != "id" || l.nameCol != "migration" {
		t.Errorf("columns resolved to id=%q name=%q", l.idCol, l.nameCol)
	}
}

// TestFindIgnoresOrdinaryTables: a database nothing manages must come back empty
// rather than with a hedge about some table that happens to have a version
// column.
func TestFindIgnoresOrdinaryTables(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"users":     {cols: []string{"id", "email"}},
		"documents": {cols: []string{"id", "version", "body"}},
	}}
	found, err := Find(context.Background(), src, "shop")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found %+v, want nothing", found)
	}
}

// TestFindSeparatesSchemaMigrations is the case the Absent/Alongside fields exist
// for: four unrelated tools write a table with this exact name, and only the rest
// of the row tells them apart.
func TestFindSeparatesSchemaMigrations(t *testing.T) {
	tests := []struct {
		name   string
		tables map[string]fakeTable
		want   string
	}{
		{
			name:   "golang-migrate has a dirty flag",
			tables: map[string]fakeTable{"schema_migrations": {cols: []string{"version", "dirty"}}},
			want:   "golang-migrate",
		},
		{
			name:   "Ecto stamps each row",
			tables: map[string]fakeTable{"schema_migrations": {cols: []string{"version", "inserted_at"}}},
			want:   "Ecto",
		},
		{
			name: "Rails is told by the table beside it",
			tables: map[string]fakeTable{
				"schema_migrations":    {cols: []string{"version"}},
				"ar_internal_metadata": {cols: []string{"key", "value"}},
			},
			want: "Rails",
		},
		{
			name:   "dbmate is the bare one",
			tables: map[string]fakeTable{"schema_migrations": {cols: []string{"version"}}},
			want:   "dbmate",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found, err := Find(context.Background(), &fakeSource{kind: db.KindPostgres, tables: tc.tables}, "app")
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(found) != 1 {
				t.Fatalf("found %d ledgers, want 1: %+v", len(found), found)
			}
			if found[0].Tool != tc.want {
				t.Errorf("tool = %q, want %q", found[0].Tool, tc.want)
			}
		})
	}
}

// TestFindPrefersKnownOverShape: the shape pass is the fallback for an unknown
// tool, not a second opinion. A Laravel app with its own data_migrations table
// gets one clean answer.
func TestFindPrefersKnownOverShape(t *testing.T) {
	src := laravelSource()
	src.tables["data_migrations"] = fakeTable{cols: []string{"id", "name", "run_at"}}

	found, err := Find(context.Background(), src, "shop")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 1 || found[0].Tool != "Laravel" {
		t.Fatalf("got %+v, want only the Laravel ledger", found)
	}
}

// TestFindFallsBackToShape: a hand-rolled ledger is still worth reporting, and
// must be labelled as a guess rather than attributed to a tool.
func TestFindFallsBackToShape(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"users":           {cols: []string{"id", "email"}},
		"data_migrations": {cols: []string{"id", "name", "run_at"}},
	}}
	found, err := Find(context.Background(), src, "shop")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d ledgers, want 1: %+v", len(found), found)
	}
	l := found[0]
	if !l.Guessed || l.Tool != "" {
		t.Errorf("a shape match must be marked as a guess with no tool: %+v", l)
	}
	if l.Label() != "unrecognised layout" {
		t.Errorf("label = %q", l.Label())
	}
	if l.atCol != "run_at" {
		t.Errorf("timestamp column = %q, want run_at", l.atCol)
	}
	if len(l.order) != 1 || l.order[0] != "run_at" {
		t.Errorf("order = %v, want the timestamp", l.order)
	}
}

// TestFindShapeNeedsAnIdentifier: a table named like a ledger but with nothing to
// identify a migration by isn't one.
func TestFindShapeNeedsAnIdentifier(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"migration_logs": {cols: []string{"message", "level"}},
	}}
	found, err := Find(context.Background(), src, "shop")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found %+v, want nothing", found)
	}
}

// TestFindMatchesCase: the catalogue is written lower-case, but SequelizeMeta and
// __EFMigrationsHistory keep their case on a server that preserves it — and the
// name that reaches SQL has to be the server's, not the catalogue's.
func TestFindMatchesCase(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"SequelizeMeta": {cols: []string{"name"}, rows: [][]any{{"20260714-add-orders.js"}}},
	}}
	found, err := Find(context.Background(), src, "app")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 1 || found[0].Tool != "Sequelize" {
		t.Fatalf("got %+v, want Sequelize", found)
	}
	if found[0].Table != "SequelizeMeta" {
		t.Errorf("table = %q, want the server's own spelling", found[0].Table)
	}

	read, err := Read(context.Background(), src, "app", found[0], 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(src.lastSelect(), `"SequelizeMeta"`) {
		t.Errorf("read %q, want the quoted server spelling", src.lastSelect())
	}
	if len(read.Latest) != 1 || read.Latest[0].ID != "20260714-add-orders.js" {
		t.Errorf("entries = %+v", read.Latest)
	}
}

// TestFindQualifiesPostgresSchema: a ledger outside the connection's search_path
// has to name its schema, both when describing it and when reading it.
func TestFindQualifiesPostgresSchema(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"schema_migrations": {schema: "app", cols: []string{"version"}, rows: [][]any{{"20260714120000"}}},
	}}
	found, err := Find(context.Background(), src, "shop")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 1 || found[0].Schema != "app" {
		t.Fatalf("got %+v, want the app schema recorded", found)
	}
	if found[0].Ref() != "app.schema_migrations" {
		t.Errorf("ref = %q", found[0].Ref())
	}
	if _, err := Read(context.Background(), src, "shop", found[0], 0); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := `"app"."schema_migrations"`; !strings.Contains(src.lastSelect(), want) {
		t.Errorf("read %q, want it qualified by %s", src.lastSelect(), want)
	}
}

// TestFindDropsMySQLSchema: MySQL reports the database as every table's schema,
// which is one namespace rather than a subdivision — reporting it would make an
// ordinary MySQL ledger look like it lived somewhere unusual.
func TestFindDropsMySQLSchema(t *testing.T) {
	src := &fakeSource{kind: db.KindMySQL, tables: map[string]fakeTable{
		"migrations": {cols: []string{"id", "migration", "batch"}, rows: [][]any{{int64(1), "create_users", int64(1)}}},
	}}
	found, err := Find(context.Background(), src, "shop")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 1 || found[0].Schema != "" {
		t.Fatalf("got %+v, want no schema recorded", found)
	}
	if _, err := Read(context.Background(), src, "shop", found[0], 0); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := "`shop`.`migrations`"; !strings.Contains(src.lastSelect(), want) {
		t.Errorf("read %q, want it qualified by %s", src.lastSelect(), want)
	}
}

// --- reading ----------------------------------------------------------------

func TestReadCountsAndOrders(t *testing.T) {
	src := laravelSource()
	status, err := Detect(context.Background(), src, "shop", 2)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	l := status.Ledgers[0]
	if l.Applied != 3 {
		t.Errorf("applied = %d, want the exact count of 3 even though only 2 rows were read", l.Applied)
	}
	if len(l.Latest) != 2 {
		t.Fatalf("read %d entries, want 2", len(l.Latest))
	}
	if l.Latest[0].Name != "2026_07_14_120000_create_orders_table" {
		t.Errorf("first entry is %+v, want the newest", l.Latest[0])
	}
	sql := src.lastSelect()
	if !strings.Contains(sql, `ORDER BY "id" DESC`) {
		t.Errorf("read %q, want it ordered newest-first", sql)
	}
	if !strings.HasSuffix(sql, "LIMIT 2") {
		t.Errorf("read %q, want the limit applied", sql)
	}
	if !strings.Contains(src.ran[0], "COUNT(*)") {
		t.Errorf("first statement was %q, want the exact count", src.ran[0])
	}
}

// TestReadDefaultsAndAllLimits: zero means the default cap, negative means read
// the lot (which is what `--all` asks for).
func TestReadDefaultsAndAllLimits(t *testing.T) {
	src := laravelSource()
	if _, err := Detect(context.Background(), src, "shop", 0); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if want := fmt.Sprintf("LIMIT %d", DefaultLimit); !strings.HasSuffix(src.lastSelect(), want) {
		t.Errorf("read %q, want the default %s", src.lastSelect(), want)
	}

	src = laravelSource()
	if _, err := Detect(context.Background(), src, "shop", -1); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if strings.Contains(src.lastSelect(), "LIMIT") {
		t.Errorf("read %q, want no cap at all", src.lastSelect())
	}
}

// TestReadNamesOnlyOnce: where one column is both the identifier and the human
// name, the report shouldn't print it twice.
func TestReadNamesOnlyOnce(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"schema_migrations": {cols: []string{"version"}, rows: [][]any{{"20260714120000"}}},
	}}
	status, err := Detect(context.Background(), src, "app", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	e := status.Ledgers[0].Latest[0]
	if e.ID != "20260714120000" || e.Name != "" {
		t.Errorf("entry = %+v, want the version as the id and no repeated name", e)
	}
}

// TestReadFormatsTimestamps: pgx hands back a real time.Time, the other drivers
// hand back text. Both have to read as a date, and to the minute — a ledger's
// seconds are noise.
func TestReadFormatsTimestamps(t *testing.T) {
	when := time.Date(2026, 7, 14, 12, 5, 33, 0, time.UTC)
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"django_migrations": {
			cols: []string{"id", "app", "name", "applied"},
			rows: [][]any{{int64(1), "shop", "0001_initial", when}},
		},
	}}
	status, err := Detect(context.Background(), src, "app", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if got := status.Ledgers[0].Latest[0].At; got != "2026-07-14 12:05" {
		t.Errorf("applied at %q, want it to the minute", got)
	}
}

// TestReadFlagsTrouble covers every state a ledger records for a migration that
// didn't apply cleanly, in each of the shapes the drivers hand booleans back as.
func TestReadFlagsTrouble(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		cols    []string
		row     []any
		want    string
		wantErr bool // expect a trouble note at all
	}{
		{
			name: "golang-migrate dirty as a real bool", table: "schema_migrations",
			cols: []string{"version", "dirty"}, row: []any{"20260714120000", true},
			want: "dirty", wantErr: true,
		},
		{
			name: "golang-migrate dirty as MySQL's tinyint", table: "schema_migrations",
			cols: []string{"version", "dirty"}, row: []any{"20260714120000", int64(1)},
			want: "dirty", wantErr: true,
		},
		{
			name: "golang-migrate dirty as driver text", table: "schema_migrations",
			cols: []string{"version", "dirty"}, row: []any{"20260714120000", []byte("t")},
			want: "dirty", wantErr: true,
		},
		{
			name: "golang-migrate clean", table: "schema_migrations",
			cols: []string{"version", "dirty"}, row: []any{"20260714120000", false},
		},
		{
			name: "Flyway records a failure", table: "flyway_schema_history",
			cols: []string{"installed_rank", "version", "description", "installed_on", "success"},
			row:  []any{int64(4), "4", "add orders", "2026-07-14 12:05", false},
			want: "failed", wantErr: true,
		},
		{
			name: "Flyway success", table: "flyway_schema_history",
			cols: []string{"installed_rank", "version", "description", "installed_on", "success"},
			row:  []any{int64(4), "4", "add orders", "2026-07-14 12:05", int64(1)},
		},
		{
			name: "Prisma never finished", table: "_prisma_migrations",
			cols: []string{"id", "migration_name", "started_at", "finished_at", "rolled_back_at"},
			row:  []any{"abc", "20260714_add_orders", "2026-07-14 12:05", nil, nil},
			want: "never finished", wantErr: true,
		},
		{
			name: "Prisma rolled back", table: "_prisma_migrations",
			cols: []string{"id", "migration_name", "started_at", "finished_at", "rolled_back_at"},
			row:  []any{"abc", "20260714_add_orders", "2026-07-14 12:05", "2026-07-14 12:06", "2026-07-14 12:09"},
			want: "rolled back", wantErr: true,
		},
		{
			name: "Prisma applied cleanly", table: "_prisma_migrations",
			cols: []string{"id", "migration_name", "started_at", "finished_at", "rolled_back_at"},
			row:  []any{"abc", "20260714_add_orders", "2026-07-14 12:05", "2026-07-14 12:06", nil},
		},
		{
			name: "goose recorded but not applied", table: "goose_db_version",
			cols: []string{"id", "version_id", "is_applied", "tstamp"},
			row:  []any{int64(2), int64(20260714), int64(0), "2026-07-14 12:05"},
			want: "not applied", wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
				tc.table: {cols: tc.cols, rows: [][]any{tc.row}},
			}}
			status, err := Detect(context.Background(), src, "app", 0)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if len(status.Ledgers) != 1 {
				t.Fatalf("found %+v, want one ledger", status.Ledgers)
			}
			got := status.Ledgers[0].Latest[0].Trouble
			if !tc.wantErr {
				if got != "" {
					t.Fatalf("a clean row was flagged: %q", got)
				}
				if status.Trouble() {
					t.Error("status reports trouble for a clean ledger")
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("trouble = %q, want it to mention %q", got, tc.want)
			}
			if !status.Trouble() {
				t.Error("status should report trouble when a row is flagged")
			}
		})
	}
}

// TestPointerLedger: alembic and golang-migrate hold the version the database is
// *at*, not a history — "1 migration applied" would be the wrong sentence.
func TestPointerLedger(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"alembic_version": {cols: []string{"version_num"}, rows: [][]any{{"a1b2c3d4"}}},
	}}
	status, err := Detect(context.Background(), src, "app", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	l := status.Ledgers[0]
	if !l.Pointer {
		t.Fatalf("%+v should be a pointer ledger", l)
	}
	if !strings.Contains(Render(status), "at version a1b2c3d4") {
		t.Errorf("report should name the version:\n%s", Render(status))
	}
	if strings.Contains(Render(status), "1 migration applied") {
		t.Errorf("a pointer ledger must not be counted as a history:\n%s", Render(status))
	}
}

// TestEmptyLedger: a ledger with no rows is a real answer (the tool is set up and
// nothing has run), not an error and not "no ledger found".
func TestEmptyLedger(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"migrations": {cols: []string{"id", "migration", "batch"}},
	}}
	status, err := Detect(context.Background(), src, "shop", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(status.Ledgers) != 1 || status.Ledgers[0].Applied != 0 {
		t.Fatalf("ledgers = %+v", status.Ledgers)
	}
	report := Render(status)
	if !strings.Contains(report, "no rows") {
		t.Errorf("report should explain the empty ledger:\n%s", report)
	}
}

func TestDetectSurfacesListError(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, fail: fmt.Errorf("connection refused")}
	if _, err := Detect(context.Background(), src, "shop", 0); err == nil {
		t.Fatal("a failing table listing must surface, not read as an empty database")
	}
}

// --- the catalogue itself ---------------------------------------------------

// TestCatalogueEntriesAreWellFormed guards the shape of the data, since a typo in
// a column name is otherwise silent: it just means that tool is never recognised.
func TestCatalogueEntriesAreWellFormed(t *testing.T) {
	for _, c := range catalog {
		if c.Tool == "" || c.Table == "" || c.ID == "" || len(c.Require) == 0 || len(c.Order) == 0 {
			t.Errorf("%+v is missing a required field", c)
		}
		for _, name := range append(append(append([]string{c.Table, c.ID, c.Name, c.At, c.Alongside}, c.Require...), c.Absent...), c.Order...) {
			if name != strings.ToLower(name) {
				t.Errorf("%s: %q must be written lower-case — matching is case-insensitive and the server's spelling is what reaches SQL", c.Tool, name)
			}
		}
		for _, f := range c.Flags {
			if f.Column == "" || f.Bad == nil || f.Says == "" {
				t.Errorf("%s: incomplete flag %+v", c.Tool, f)
			}
			if f.Column != strings.ToLower(f.Column) {
				t.Errorf("%s: flag column %q must be lower-case", c.Tool, f.Column)
			}
		}
	}
}

// TestCatalogueIsUnambiguous is the guard that makes the ordering safe: build the
// minimal table each entry describes, and the catalogue must resolve it to that
// entry and no other. Adding a tool whose layout is a superset of an earlier one
// fails here rather than silently answering with the wrong tool's name.
func TestCatalogueIsUnambiguous(t *testing.T) {
	for _, c := range catalog {
		t.Run(c.Tool+"/"+c.Table, func(t *testing.T) {
			present := map[string]bool{c.Table: true}
			if c.Alongside != "" {
				present[c.Alongside] = true
			}
			got, ok := matchCatalog(c.Table, c.Require, present)
			if !ok {
				t.Fatalf("%s doesn't match its own required columns %v", c.Tool, c.Require)
			}
			if got.Tool != c.Tool {
				t.Errorf("a table with %v resolved to %s, want %s — the catalogue order can't separate them",
					c.Require, got.Tool, c.Tool)
			}
		})
	}
}

// TestFindDropsPublicSchema: Postgres reports public as the schema of an ordinary
// table, and "public.migrations" is noise — public is the default search_path
// entry, so it adds nothing to the report or to the read. A table in a *real*
// schema keeps it (TestFindQualifiesPostgresSchema).
func TestFindDropsPublicSchema(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"migrations": {schema: "public", cols: []string{"id", "migration", "batch"},
			rows: [][]any{{int64(1), "create_users", int64(1)}}},
	}}
	found, err := Find(context.Background(), src, "shop")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 1 || found[0].Schema != "" {
		t.Fatalf("got %+v, want the public schema dropped", found)
	}
	if found[0].Ref() != "migrations" {
		t.Errorf("ref = %q, want the bare table name", found[0].Ref())
	}
	if _, err := Read(context.Background(), src, "shop", found[0], 0); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if strings.Contains(src.lastSelect(), "public") {
		t.Errorf("read %q, want no public qualifier", src.lastSelect())
	}
}
