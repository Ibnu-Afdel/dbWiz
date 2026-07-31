package schema

import (
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

func snap(label string, tables ...Table) Snapshot {
	return Snapshot{Label: label, Database: label, Tables: tables}
}

func tbl(name string, cols ...Column) Table {
	return Table{Name: name, Columns: cols}
}

func col(name, typ string, nullable bool) Column {
	return Column{Name: name, Type: typ, Nullable: nullable}
}

func TestDiffIdentical(t *testing.T) {
	s := snap("dev", tbl("users", col("id", "integer", false), col("email", "text", true)))
	r := Diff(s, snap("staging", s.Tables...))
	if !r.Identical() {
		t.Fatalf("identical snapshots reported %d differences: %+v", len(r.Tables), r.Tables)
	}
	if out := Render(r); !strings.Contains(out, "No differences") {
		t.Errorf("Render should say so plainly, got:\n%s", out)
	}
}

func TestDiffAddedAndRemovedTables(t *testing.T) {
	from := snap("dev", tbl("users", col("id", "integer", false)), tbl("legacy", col("x", "text", true)))
	to := snap("staging", tbl("users", col("id", "integer", false)), tbl("audit", col("at", "timestamp", true)))

	r := Diff(from, to)
	if len(r.Tables) != 2 {
		t.Fatalf("got %d table diffs, want 2: %+v", len(r.Tables), r.Tables)
	}
	// Sorted by qualified name: audit before legacy.
	if r.Tables[0].Table != "audit" || r.Tables[0].Status != Added {
		t.Errorf("first diff = %+v, want audit added", r.Tables[0])
	}
	if r.Tables[1].Table != "legacy" || r.Tables[1].Status != Removed {
		t.Errorf("second diff = %+v, want legacy removed", r.Tables[1])
	}
	// A wholly added table still lists what it contains.
	if len(r.Tables[0].Columns) != 1 || r.Tables[0].Columns[0].Status != Added {
		t.Errorf("added table should list its columns as added: %+v", r.Tables[0].Columns)
	}
	added, removed, changed := r.Counts()
	if added != 1 || removed != 1 || changed != 0 {
		t.Errorf("counts = %d/%d/%d, want 1/1/0", added, removed, changed)
	}
}

func TestDiffColumnChanges(t *testing.T) {
	from := snap("dev", tbl("users",
		col("id", "integer", false),
		col("email", "text", true),
		col("nickname", "text", true),
	))
	to := snap("staging", tbl("users",
		Column{Name: "id", Type: "bigint", Nullable: true, Key: "PRI"},
		col("email", "text", false),
		col("last_login", "timestamp", true),
	))

	r := Diff(from, to)
	if len(r.Tables) != 1 || r.Tables[0].Status != Changed {
		t.Fatalf("want one changed table, got %+v", r.Tables)
	}
	got := map[string]ColumnDiff{}
	for _, c := range r.Tables[0].Columns {
		got[c.Column] = c
	}

	id := got["id"]
	if id.Status != Changed || len(id.Attrs) != 3 {
		t.Fatalf("id should report type, nullable and key moving: %+v", id)
	}
	if id.Attrs[0] != (Attr{"type", "integer", "bigint"}) {
		t.Errorf("id type attr = %+v", id.Attrs[0])
	}
	if id.Attrs[1] != (Attr{"nullable", "NOT NULL", "NULL"}) {
		t.Errorf("id nullable attr = %+v", id.Attrs[1])
	}
	if id.Attrs[2] != (Attr{"key", "none", "PRI"}) {
		t.Errorf("id key attr = %+v", id.Attrs[2])
	}

	if e := got["email"]; e.Status != Changed || len(e.Attrs) != 1 || e.Attrs[0].Name != "nullable" {
		t.Errorf("email should report only a nullability change: %+v", e)
	}
	if l := got["last_login"]; l.Status != Added || l.Detail != "timestamp NULL" {
		t.Errorf("last_login = %+v, want added with a readable detail", l)
	}
	if n := got["nickname"]; n.Status != Removed {
		t.Errorf("nickname = %+v, want removed", n)
	}

	// Order: to-side columns in ordinal order, then removed ones.
	names := []string{}
	for _, c := range r.Tables[0].Columns {
		names = append(names, c.Column)
	}
	want := []string{"id", "email", "last_login", "nickname"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("column order = %v, want %v", names, want)
	}
}

func TestDiffIgnoresColumnReordering(t *testing.T) {
	a := col("a", "text", true)
	b := col("b", "integer", false)
	from := snap("dev", tbl("t", a, b))
	to := snap("staging", tbl("t", b, a))
	if r := Diff(from, to); !r.Identical() {
		t.Errorf("reordering identical columns should not be a difference: %+v", r.Tables)
	}
}

func TestDiffQualifiedTablesAreDistinct(t *testing.T) {
	from := snap("dev", Table{Schema: "public", Name: "users", Columns: []Column{col("id", "integer", false)}})
	to := snap("staging", Table{Schema: "audit", Name: "users", Columns: []Column{col("id", "integer", false)}})

	r := Diff(from, to)
	if len(r.Tables) != 2 {
		t.Fatalf("same-named tables in different schemas must not match: %+v", r.Tables)
	}
	if r.Tables[0].Table != "audit.users" || r.Tables[1].Table != "public.users" {
		t.Errorf("diff should use qualified names: %+v", r.Tables)
	}
}

func TestDiffWarnsAcrossEngines(t *testing.T) {
	from := Snapshot{Label: "dev", Kind: db.KindPostgres}
	to := Snapshot{Label: "shop", Kind: db.KindMySQL}
	r := Diff(from, to)
	if r.Warning == "" {
		t.Fatal("comparing across engines should carry a warning")
	}
	if !strings.Contains(r.Warning, "PostgreSQL") || !strings.Contains(r.Warning, "MySQL") {
		t.Errorf("warning should name both engines: %q", r.Warning)
	}
	if out := Render(r); !strings.Contains(out, r.Warning) {
		t.Errorf("Render dropped the warning:\n%s", out)
	}
}

func TestRenderLayout(t *testing.T) {
	from := snap("app@dev", tbl("users", col("nickname", "text", true)), tbl("legacy"))
	to := snap("app@staging", tbl("users", col("email", "text", false)), tbl("audit", col("at", "timestamp", true)))

	out := Render(Diff(from, to))
	for _, want := range []string{
		"--- app@dev",
		"+++ app@staging",
		"+ table audit",
		"- table legacy",
		"~ table users",
		"+ email",
		"- nickname",
		"1 table added, 1 table removed, 1 table changed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderPluralises(t *testing.T) {
	from := snap("a")
	to := snap("b", tbl("x"), tbl("y"))
	if out := Render(Diff(from, to)); !strings.Contains(out, "2 tables added, 0 tables removed, 0 tables changed") {
		t.Errorf("summary line wrong:\n%s", out)
	}
}

func TestStatusStrings(t *testing.T) {
	for status, want := range map[Status]string{Same: "same", Added: "added", Removed: "removed", Changed: "changed"} {
		if got := status.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", status, got, want)
		}
	}
}
