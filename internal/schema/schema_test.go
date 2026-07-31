package schema

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// fakeSource stands in for a live engine: a fixed table list plus the columns
// each table reference should return. describeCalls records the exact reference
// Capture asked for, which is how the qualification rules are asserted.
type fakeSource struct {
	kind          db.Kind
	tables        []db.Table
	cols          map[string][]db.Column
	listErr       error
	describeErr   error
	describeCalls []string
}

func (f *fakeSource) Kind() db.Kind { return f.kind }

func (f *fakeSource) ListTables(_ context.Context, _ string) ([]db.Table, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tables, nil
}

func (f *fakeSource) DescribeTable(_ context.Context, _, table string) ([]db.Column, error) {
	f.describeCalls = append(f.describeCalls, table)
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	return f.cols[table], nil
}

func TestCaptureSortsTablesAndKeepsColumnOrder(t *testing.T) {
	src := &fakeSource{
		kind: db.KindSQLite,
		tables: []db.Table{
			{Name: "users", Rows: 10},
			{Name: "accounts", Rows: 3},
		},
		cols: map[string][]db.Column{
			"users": {
				{Name: "id", Type: "INTEGER", Key: "PRI"},
				{Name: "email", Type: "TEXT", Nullable: true},
			},
			"accounts": {{Name: "id", Type: "INTEGER", Key: "PRI"}},
		},
	}

	snap, err := Capture(context.Background(), src, "app.db")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if snap.Database != "app.db" || snap.Label != "app.db" {
		t.Errorf("database/label = %q/%q, want app.db", snap.Database, snap.Label)
	}
	if snap.Kind != db.KindSQLite {
		t.Errorf("Kind = %v, want SQLite (should be read off the source)", snap.Kind)
	}
	if len(snap.Tables) != 2 {
		t.Fatalf("got %d tables, want 2", len(snap.Tables))
	}
	if snap.Tables[0].Name != "accounts" || snap.Tables[1].Name != "users" {
		t.Errorf("tables not sorted: %s, %s", snap.Tables[0].Name, snap.Tables[1].Name)
	}
	// Column order is structure and must survive verbatim.
	users, ok := snap.Table("users")
	if !ok {
		t.Fatal("users missing from snapshot")
	}
	if users.Columns[0].Name != "id" || users.Columns[1].Name != "email" {
		t.Errorf("column order changed: %+v", users.Columns)
	}
	if !users.Columns[1].Nullable || users.Columns[0].Key != "PRI" {
		t.Errorf("column detail lost: %+v", users.Columns)
	}
}

func TestCaptureDropsMySQLSchemaEqualToDatabase(t *testing.T) {
	// MySQL reports table_schema == the database; keeping it would make every
	// table look like it moved when two MySQL databases are compared.
	src := &fakeSource{
		kind:   db.KindMySQL,
		tables: []db.Table{{Schema: "shop", Name: "orders"}},
		cols:   map[string][]db.Column{"orders": {{Name: "id", Type: "int"}}},
	}

	snap, err := Capture(context.Background(), src, "shop")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if got := snap.Tables[0].Schema; got != "" {
		t.Errorf("schema = %q, want it dropped", got)
	}
	if got := snap.Tables[0].Qualified(); got != "orders" {
		t.Errorf("Qualified() = %q, want orders", got)
	}
	if len(src.describeCalls) != 1 || src.describeCalls[0] != "orders" {
		t.Errorf("describe calls = %v, want unqualified [orders]", src.describeCalls)
	}
}

func TestCaptureQualifiesRealSchemas(t *testing.T) {
	// Postgres schemas are real subdivisions: they stay, and the describe call is
	// qualified so two same-named tables in different schemas stay distinct.
	src := &fakeSource{
		kind: db.KindPostgres,
		tables: []db.Table{
			{Schema: "public", Name: "users"},
			{Schema: "audit", Name: "users"},
		},
		cols: map[string][]db.Column{
			"public.users": {{Name: "id", Type: "integer", Key: "PRI"}},
			"audit.users":  {{Name: "event", Type: "text", Nullable: true}},
		},
	}

	snap, err := Capture(context.Background(), src, "app")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(snap.Tables) != 2 {
		t.Fatalf("got %d tables, want 2", len(snap.Tables))
	}
	if got := snap.Tables[0].Qualified(); got != "audit.users" {
		t.Errorf("first table = %q, want audit.users (sorted by schema)", got)
	}
	audit, _ := snap.Table("audit.users")
	pub, _ := snap.Table("public.users")
	if len(audit.Columns) != 1 || audit.Columns[0].Name != "event" {
		t.Errorf("audit.users got the wrong columns: %+v", audit.Columns)
	}
	if len(pub.Columns) != 1 || pub.Columns[0].Name != "id" {
		t.Errorf("public.users got the wrong columns: %+v", pub.Columns)
	}
	if len(src.describeCalls) != 2 || src.describeCalls[0] != "public.users" {
		t.Errorf("describe calls = %v, want qualified references", src.describeCalls)
	}
}

func TestCaptureColumnLookup(t *testing.T) {
	tbl := Table{Name: "t", Columns: []Column{{Name: "a"}, {Name: "b", Type: "text"}}}
	if c, ok := tbl.Column("b"); !ok || c.Type != "text" {
		t.Errorf("Column(b) = %+v, %v", c, ok)
	}
	if _, ok := tbl.Column("missing"); ok {
		t.Error("Column(missing) reported found")
	}
}

func TestCaptureErrorsNameTheirContext(t *testing.T) {
	boom := errors.New("connection reset")

	if _, err := Capture(context.Background(), &fakeSource{listErr: boom}, "app"); err == nil {
		t.Fatal("want an error when listing fails")
	} else if !strings.Contains(err.Error(), `"app"`) || !errors.Is(err, boom) {
		t.Errorf("list error lost context or cause: %v", err)
	}

	src := &fakeSource{tables: []db.Table{{Name: "users"}}, describeErr: boom}
	if _, err := Capture(context.Background(), src, "app"); err == nil {
		t.Fatal("want an error when describing fails")
	} else if !strings.Contains(err.Error(), "users") || !errors.Is(err, boom) {
		t.Errorf("describe error lost context or cause: %v", err)
	}
}

func TestCaptureNilContext(t *testing.T) {
	// The TUI and CLI both hand contexts around; a nil must not panic.
	src := &fakeSource{tables: []db.Table{{Name: "t"}}, cols: map[string][]db.Column{"t": {{Name: "id"}}}}
	var nilCtx context.Context // deliberately nil, to exercise the guard
	if _, err := Capture(nilCtx, src, "app"); err != nil {
		t.Fatalf("Capture with nil ctx: %v", err)
	}
}
