package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// fakeSchemaEngine serves a fixed structure. It embeds db.Engine so only the
// handful of methods the schema commands touch need implementing; anything else
// would panic loudly rather than quietly return a zero value.
type fakeSchemaEngine struct {
	db.Engine
	kind   db.Kind
	tables []db.Table
	cols   map[string][]db.Column
}

func (f *fakeSchemaEngine) Kind() db.Kind                            { return f.kind }
func (f *fakeSchemaEngine) Connect(context.Context, db.Target) error { return nil }
func (f *fakeSchemaEngine) Close() error                             { return nil }

func (f *fakeSchemaEngine) ListTables(context.Context, string) ([]db.Table, error) {
	return f.tables, nil
}

func (f *fakeSchemaEngine) DescribeTable(_ context.Context, _, table string) ([]db.Column, error) {
	return f.cols[table], nil
}

// fakeEngines hands out the given engines in order, one per newEngine call, so a
// two-sided diff gets a distinct structure for each side.
func fakeEngines(t *testing.T, engines ...db.Engine) {
	t.Helper()
	old := newEngine
	i := 0
	newEngine = func(db.Kind) (db.Engine, error) {
		if i >= len(engines) {
			t.Fatalf("newEngine called %d times, only %d engines provided", i+1, len(engines))
		}
		e := engines[i]
		i++
		return e, nil
	}
	t.Cleanup(func() { newEngine = old })
}

// devSide is a small Postgres-shaped structure used as the "before" side.
func devSide() *fakeSchemaEngine {
	return &fakeSchemaEngine{
		kind:   db.KindPostgres,
		tables: []db.Table{{Schema: "public", Name: "users"}, {Schema: "public", Name: "legacy"}},
		cols: map[string][]db.Column{
			"public.users":  {{Name: "id", Type: "integer", Key: "PRI"}, {Name: "nickname", Type: "text", Nullable: true}},
			"public.legacy": {{Name: "x", Type: "text", Nullable: true}},
		},
	}
}

// stagingSide drops a table, adds one, and changes a column of users.
func stagingSide() *fakeSchemaEngine {
	return &fakeSchemaEngine{
		kind:   db.KindPostgres,
		tables: []db.Table{{Schema: "public", Name: "users"}, {Schema: "public", Name: "audit"}},
		cols: map[string][]db.Column{
			"public.users": {{Name: "id", Type: "bigint", Key: "PRI"}, {Name: "email", Type: "text", Nullable: true}},
			"public.audit": {{Name: "at", Type: "timestamp", Nullable: true}},
		},
	}
}

func schemaTestSetup(t *testing.T, engines ...db.Engine) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fakeDetect(t, []docker.Container{full("pg", docker.EnginePostgres)}, nil)
	fakeEngines(t, engines...)
}

// TestSchemaDiffReportsDrift compares two databases on one container: the report
// lists what moved and the command exits non-zero.
func TestSchemaDiffReportsDrift(t *testing.T) {
	schemaTestSetup(t, devSide(), stagingSide())

	var out bytes.Buffer
	err := runSchemaDiff(context.Background(), &out, schemaDiffOpts{dbA: "dev", dbB: "staging"})
	if err == nil {
		t.Fatal("differing databases must exit non-zero")
	}
	if !strings.Contains(err.Error(), "differ") {
		t.Errorf("unexpected error: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"--- pg/dev",
		"+++ pg/staging",
		"+ table public.audit",
		"- table public.legacy",
		"~ table public.users",
		"type integer → bigint",
		"+ email",
		"- nickname",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
}

// TestSchemaDiffIdenticalExitsZero: matching structures are a success, and say so.
func TestSchemaDiffIdenticalExitsZero(t *testing.T) {
	schemaTestSetup(t, devSide(), devSide())

	var out bytes.Buffer
	if err := runSchemaDiff(context.Background(), &out, schemaDiffOpts{dbA: "dev", dbB: "staging"}); err != nil {
		t.Fatalf("identical databases must exit zero, got %v", err)
	}
	if !strings.Contains(out.String(), "No differences") {
		t.Errorf("expected a plain no-differences line:\n%s", out.String())
	}
}

// TestSchemaDiffJSON emits the machine-readable report.
func TestSchemaDiffJSON(t *testing.T) {
	schemaTestSetup(t, devSide(), stagingSide())

	var out bytes.Buffer
	_ = runSchemaDiff(context.Background(), &out, schemaDiffOpts{dbA: "dev", dbB: "staging", asJSON: true})

	var payload struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Tables []struct {
			Table   string `json:"table"`
			Status  string `json:"status"`
			Columns []struct {
				Column string `json:"column"`
				Status string `json:"status"`
			} `json:"columns"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if payload.From != "pg/dev" || payload.To != "pg/staging" {
		t.Errorf("labels = %q/%q", payload.From, payload.To)
	}
	if len(payload.Tables) != 3 {
		t.Fatalf("got %d table entries, want 3: %+v", len(payload.Tables), payload.Tables)
	}
	if payload.Tables[0].Table != "public.audit" || payload.Tables[0].Status != "added" {
		t.Errorf("first entry = %+v", payload.Tables[0])
	}
}

// TestSchemaDiffRefusesSameDatabase: comparing a database with itself is a
// mistake worth naming, not an empty report.
func TestSchemaDiffRefusesSameDatabase(t *testing.T) {
	schemaTestSetup(t, devSide(), devSide())

	var out bytes.Buffer
	err := runSchemaDiff(context.Background(), &out, schemaDiffOpts{dbA: "dev", dbB: "dev"})
	if err == nil || !strings.Contains(err.Error(), "both sides resolve to pg/dev") {
		t.Fatalf("expected a same-database refusal, got %v", err)
	}
}

// TestSchemaDiffDefaultsSecondDatabaseToFirst: with a second target, one database
// name means "the same database over there".
func TestSchemaDiffAgainstFile(t *testing.T) {
	sqliteSide := &fakeSchemaEngine{
		kind:   db.KindSQLite,
		tables: []db.Table{{Name: "users"}},
		cols:   map[string][]db.Column{"users": {{Name: "id", Type: "INTEGER", Key: "PRI"}}},
	}
	schemaTestSetup(t, devSide(), sqliteSide)

	var out bytes.Buffer
	_ = runSchemaDiff(context.Background(), &out, schemaDiffOpts{dbA: "dev", againstFile: "/tmp/old.db"})

	got := out.String()
	if !strings.Contains(got, "+++ /tmp/old.db") {
		t.Errorf("the SQLite side should be labelled by its path:\n%s", got)
	}
	// Engines differ, so the report warns that type names aren't comparable.
	if !strings.Contains(got, "PostgreSQL") || !strings.Contains(got, "SQLite") {
		t.Errorf("expected a cross-engine warning:\n%s", got)
	}
}

// TestSchemaDiffRejectsBothAgainstFlags is checked at the flag layer, so it runs
// through the real cobra command.
func TestSchemaDiffRejectsBothAgainstFlags(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cmd := NewSchemaCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"diff", "dev", "--against-target", "pg2", "--against-file", "/tmp/x.db"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("expected the two --against flags to be mutually exclusive, got %v", err)
	}
}

// fakeDDLEngine adds a DDL source to the fake structure engine, so `schema dump`
// can be driven end to end without a live server.
type fakeDDLEngine struct {
	*fakeSchemaEngine
	ddl  map[string]string
	asks []string
}

func (f *fakeDDLEngine) TableDDL(_ context.Context, _, table string) (string, error) {
	f.asks = append(f.asks, table)
	return f.ddl[table], nil
}

func dumpEngine() *fakeDDLEngine {
	return &fakeDDLEngine{
		fakeSchemaEngine: devSide(),
		ddl: map[string]string{
			"public.users":  "CREATE TABLE \"public\".\"users\" (\n    \"id\" integer NOT NULL\n);",
			"public.legacy": "CREATE TABLE \"public\".\"legacy\" (\n    \"x\" text\n);",
		},
	}
}

// TestSchemaDumpEmitsEveryTable exports the whole database, sorted, with a header
// that says plainly that no rows are involved.
func TestSchemaDumpEmitsEveryTable(t *testing.T) {
	e := dumpEngine()
	schemaTestSetup(t, e)

	var out bytes.Buffer
	if err := runSchemaDump(context.Background(), &out, schemaDumpOpts{database: "app"}); err != nil {
		t.Fatalf("runSchemaDump: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"-- DBWiz schema dump of pg/app (PostgreSQL)",
		"structure only; no rows",
		`CREATE TABLE "public"."legacy"`,
		`CREATE TABLE "public"."users"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dump missing %q:\n%s", want, got)
		}
	}
	// Sorted order, and asked for by the same qualified reference the diff uses.
	if len(e.asks) != 2 || e.asks[0] != "public.legacy" || e.asks[1] != "public.users" {
		t.Errorf("DDL requested as %v, want sorted qualified refs", e.asks)
	}
}

// TestSchemaDumpSingleTable narrows to one table, by bare or qualified name.
func TestSchemaDumpSingleTable(t *testing.T) {
	for _, name := range []string{"users", "public.users"} {
		e := dumpEngine()
		schemaTestSetup(t, e)

		var out bytes.Buffer
		if err := runSchemaDump(context.Background(), &out, schemaDumpOpts{database: "app", table: name}); err != nil {
			t.Fatalf("runSchemaDump(%s): %v", name, err)
		}
		if strings.Contains(out.String(), "legacy") {
			t.Errorf("--table %s exported more than asked:\n%s", name, out.String())
		}
		if !strings.Contains(out.String(), `"public"."users"`) {
			t.Errorf("--table %s exported nothing:\n%s", name, out.String())
		}
	}
}

// TestSchemaDumpUnknownTable names the mistake instead of writing an empty file.
func TestSchemaDumpUnknownTable(t *testing.T) {
	schemaTestSetup(t, dumpEngine())

	var out bytes.Buffer
	err := runSchemaDump(context.Background(), &out, schemaDumpOpts{database: "app", table: "nope"})
	if err == nil || !strings.Contains(err.Error(), `no table named "nope"`) {
		t.Fatalf("expected an unknown-table error, got %v", err)
	}
}

// TestSchemaDumpToFile writes the same document to disk, leaving stdout clean so
// a redirect isn't polluted by the confirmation.
func TestSchemaDumpToFile(t *testing.T) {
	schemaTestSetup(t, dumpEngine())
	path := filepath.Join(t.TempDir(), "schema.sql")

	var out bytes.Buffer
	if err := runSchemaDump(context.Background(), &out, schemaDumpOpts{database: "app", output: path}); err != nil {
		t.Fatalf("runSchemaDump: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should stay empty when writing a file, got %q", out.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), `CREATE TABLE "public"."users"`) {
		t.Errorf("file missing the DDL:\n%s", data)
	}
}

// TestSchemaDumpRefusesEngineWithoutDDL: an engine that can't export says so
// rather than producing an empty dump.
func TestSchemaDumpRefusesEngineWithoutDDL(t *testing.T) {
	schemaTestSetup(t, devSide()) // no TableDDL method

	var out bytes.Buffer
	err := runSchemaDump(context.Background(), &out, schemaDumpOpts{database: "app"})
	if err == nil || !strings.Contains(err.Error(), "can't export DDL") {
		t.Fatalf("expected a refusal, got %v", err)
	}
}
