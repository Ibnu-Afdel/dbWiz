package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// fakeLedgerEngine is a database whose only interesting feature is a migration
// table. It answers the three methods migrations.Source needs and records the
// statements it was asked to run.
type fakeLedgerEngine struct {
	db.Engine
	kind   db.Kind
	tables []db.Table
	cols   map[string][]db.Column
	rows   map[string][][]any
	ran    []string
}

func (f *fakeLedgerEngine) Kind() db.Kind                            { return f.kind }
func (f *fakeLedgerEngine) Connect(context.Context, db.Target) error { return nil }
func (f *fakeLedgerEngine) Close() error                             { return nil }

func (f *fakeLedgerEngine) ListTables(context.Context, string) ([]db.Table, error) {
	return f.tables, nil
}

func (f *fakeLedgerEngine) DescribeTable(_ context.Context, _, table string) ([]db.Column, error) {
	return f.cols[table], nil
}

func (f *fakeLedgerEngine) ExecMutation(_ context.Context, _, sql string) (db.Result, error) {
	f.ran = append(f.ran, sql)
	for name, rows := range f.rows {
		if !strings.Contains(sql, name) {
			continue
		}
		if strings.HasPrefix(sql, "SELECT COUNT(*)") {
			return db.Result{Columns: []string{"count"}, Rows: [][]any{{int64(len(rows))}}}, nil
		}
		return db.Result{Columns: colNames(f.cols[name]), Rows: rows}, nil
	}
	return db.Result{}, nil
}

func colNames(cols []db.Column) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

func cols(names ...string) []db.Column {
	out := make([]db.Column, 0, len(names))
	for _, n := range names {
		out = append(out, db.Column{Name: n, Type: "text", Nullable: true})
	}
	return out
}

// laravelEngine is a Postgres database managed by Laravel, with one ordinary
// table alongside the ledger.
func laravelEngine() *fakeLedgerEngine {
	return &fakeLedgerEngine{
		kind:   db.KindPostgres,
		tables: []db.Table{{Name: "users"}, {Name: "migrations"}},
		cols: map[string][]db.Column{
			"users":      cols("id", "email"),
			"migrations": cols("id", "migration", "batch"),
		},
		rows: map[string][][]any{
			"migrations": {
				{int64(2), "2026_07_14_120000_create_orders_table", int64(2)},
				{int64(1), "2024_10_12_000000_create_users_table", int64(1)},
			},
		},
	}
}

// ledgerTestSetup wires the detection and engine seams to a single fake container.
func ledgerTestSetup(t *testing.T, engine db.Engine) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{full("pg", docker.EnginePostgres)}, nil)
	fakeEngines(t, engine)
}

// TestMigrationsReportsLedger covers the common case end to end through the
// command: the tool is named, the count is exact, and the caveat about pending
// migrations is there.
func TestMigrationsReportsLedger(t *testing.T) {
	ledgerTestSetup(t, laravelEngine())

	var out bytes.Buffer
	if err := runMigrations(context.Background(), &out, migrationsOpts{}); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Laravel · migrations · 2 migrations applied",
		"2026_07_14_120000_create_orders_table",
		"pending",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
}

// TestMigrationsExitsNonZeroOnTrouble: a half-applied migration is the thing a
// deploy script wants to fail on, so the report prints and the command still
// fails.
func TestMigrationsExitsNonZeroOnTrouble(t *testing.T) {
	eng := &fakeLedgerEngine{
		kind:   db.KindPostgres,
		tables: []db.Table{{Name: "schema_migrations"}},
		cols:   map[string][]db.Column{"schema_migrations": cols("version", "dirty")},
		rows:   map[string][][]any{"schema_migrations": {{"20260714120000", true}}},
	}
	ledgerTestSetup(t, eng)

	var out bytes.Buffer
	err := runMigrations(context.Background(), &out, migrationsOpts{})
	if err == nil {
		t.Fatal("a dirty ledger must fail the command")
	}
	if !strings.Contains(out.String(), "! marked dirty") {
		t.Errorf("the report has to be printed before the failure:\n%s", out.String())
	}
}

// TestMigrationsNoLedgerIsSuccess: "nothing manages this database" is an answer,
// not a failure.
func TestMigrationsNoLedgerIsSuccess(t *testing.T) {
	eng := &fakeLedgerEngine{
		kind:   db.KindPostgres,
		tables: []db.Table{{Name: "users"}},
		cols:   map[string][]db.Column{"users": cols("id", "email")},
	}
	ledgerTestSetup(t, eng)

	var out bytes.Buffer
	if err := runMigrations(context.Background(), &out, migrationsOpts{}); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if !strings.Contains(out.String(), "No migration ledger found.") {
		t.Errorf("report:\n%s", out.String())
	}
}

// TestMigrationsJSON: a script sees the same facts a person does, including the
// tool name and the per-entry trouble.
func TestMigrationsJSON(t *testing.T) {
	ledgerTestSetup(t, laravelEngine())

	var out bytes.Buffer
	if err := runMigrations(context.Background(), &out, migrationsOpts{asJSON: true}); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	var parsed struct {
		Engine  string `json:"engine"`
		Ledgers []struct {
			Tool    string `json:"tool"`
			Table   string `json:"table"`
			Applied int    `json:"applied"`
			Latest  []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"latest"`
		} `json:"ledgers"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("output isn't JSON: %v\n%s", err, out.String())
	}
	if len(parsed.Ledgers) != 1 {
		t.Fatalf("ledgers = %+v", parsed.Ledgers)
	}
	l := parsed.Ledgers[0]
	if l.Tool != "Laravel" || l.Table != "migrations" || l.Applied != 2 {
		t.Errorf("ledger = %+v", l)
	}
	if len(l.Latest) != 2 || l.Latest[0].Name == "" {
		t.Errorf("latest = %+v", l.Latest)
	}
}

// TestMigrationsLimits: --limit caps the listing, --all lifts the cap entirely.
func TestMigrationsLimits(t *testing.T) {
	eng := laravelEngine()
	ledgerTestSetup(t, eng)

	var out bytes.Buffer
	if err := runMigrations(context.Background(), &out, migrationsOpts{limit: 1}); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if !strings.HasSuffix(lastRead(eng), "LIMIT 1") {
		t.Errorf("read %q, want the limit applied", lastRead(eng))
	}

	eng = laravelEngine()
	ledgerTestSetup(t, eng)
	out.Reset()
	if err := runMigrations(context.Background(), &out, migrationsOpts{limit: -1}); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if strings.Contains(lastRead(eng), "LIMIT") {
		t.Errorf("read %q, want no cap for --all", lastRead(eng))
	}
}

func lastRead(f *fakeLedgerEngine) string {
	for i := len(f.ran) - 1; i >= 0; i-- {
		if !strings.HasPrefix(f.ran[i], "SELECT COUNT(*)") {
			return f.ran[i]
		}
	}
	return ""
}
