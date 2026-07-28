package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// fakeQuerier returns a canned result (or error) for the one statement the query
// command runs.
type fakeQuerier struct {
	res db.Result
	err error
	got string
}

func (f *fakeQuerier) Query(_ context.Context, sql string) (db.Result, error) {
	f.got = sql
	return f.res, f.err
}

func selectResult() db.Result {
	return db.Result{
		Columns:  []string{"id", "name"},
		Rows:     [][]any{{"1", "alice"}, {"2", nil}},
		Duration: 3 * time.Millisecond,
	}
}

// TestQueryTable renders a row set as an aligned table with NULL spelled out and
// a summary footer.
func TestQueryTable(t *testing.T) {
	q := &fakeQuerier{res: selectResult()}
	var out bytes.Buffer
	if err := runQuery(context.Background(), &out, q, "select * from t", "table"); err != nil {
		t.Fatalf("runQuery: %v", err)
	}
	got := out.String()
	for _, want := range []string{"id", "name", "alice", "NULL", "(2 row(s)"} {
		if !strings.Contains(got, want) {
			t.Errorf("table missing %q:\n%s", want, got)
		}
	}
	if q.got != "select * from t" {
		t.Errorf("statement not passed through: %q", q.got)
	}
}

// TestQueryJSON emits an array of row objects with NULL as JSON null.
func TestQueryJSON(t *testing.T) {
	q := &fakeQuerier{res: selectResult()}
	var out bytes.Buffer
	if err := runQuery(context.Background(), &out, q, "select", "json"); err != nil {
		t.Fatalf("runQuery: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if len(rows) != 2 || rows[0]["name"] != "alice" || rows[1]["name"] != nil {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

// TestQueryCSV emits RFC 4180 CSV with a header row.
func TestQueryCSV(t *testing.T) {
	q := &fakeQuerier{res: selectResult()}
	var out bytes.Buffer
	if err := runQuery(context.Background(), &out, q, "select", "csv"); err != nil {
		t.Fatalf("runQuery: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || lines[0] != "id,name" || lines[1] != "1,alice" {
		t.Fatalf("unexpected CSV:\n%s", out.String())
	}
}

// TestQueryExecTable reports the affected-row count for a statement with no rows.
func TestQueryExecTable(t *testing.T) {
	q := &fakeQuerier{res: db.Result{RowsAffected: 5}}
	var out bytes.Buffer
	if err := runQuery(context.Background(), &out, q, "update t set x=1", "table"); err != nil {
		t.Fatalf("runQuery: %v", err)
	}
	if !strings.Contains(out.String(), "5 row(s) affected") {
		t.Errorf("expected an affected count, got %q", out.String())
	}
}

// TestQueryExecJSON reports the affected count as structured JSON.
func TestQueryExecJSON(t *testing.T) {
	q := &fakeQuerier{res: db.Result{RowsAffected: 5}}
	var out bytes.Buffer
	if err := runQuery(context.Background(), &out, q, "delete from t", "json"); err != nil {
		t.Fatalf("runQuery: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out.String())
	}
	if obj["rows_affected"] != float64(5) {
		t.Fatalf("unexpected object: %+v", obj)
	}
}

// TestQueryError surfaces a typed DBError's detail/hint plainly.
func TestQueryError(t *testing.T) {
	q := &fakeQuerier{err: &db.DBError{Kind: db.DBErrQuerySyntax, Title: "Syntax error", Detail: "near SELCT", Hint: "check the statement"}}
	err := runQuery(context.Background(), new(bytes.Buffer), q, "SELCT 1", "table")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"near SELCT", "check the statement"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should keep DBError detail/hint %q, got: %v", want, err)
		}
	}
}

// TestQueryFormatConflict rejects --json and --csv together.
func TestQueryFormatConflict(t *testing.T) {
	if _, err := queryFormat(true, true); err == nil {
		t.Fatal("expected a conflict error for --json and --csv")
	}
	for _, tc := range []struct {
		j, c bool
		want string
	}{{true, false, "json"}, {false, true, "csv"}, {false, false, "table"}} {
		got, err := queryFormat(tc.j, tc.c)
		if err != nil || got != tc.want {
			t.Errorf("queryFormat(%v,%v) = (%q,%v), want %q", tc.j, tc.c, got, err, tc.want)
		}
	}
}

// TestQueryEmptyResult renders a zero-row SELECT as just a header and a 0 footer,
// not an error.
func TestQueryEmptyResult(t *testing.T) {
	q := &fakeQuerier{res: db.Result{Columns: []string{"id"}, Rows: nil}}
	var out bytes.Buffer
	if err := runQuery(context.Background(), &out, q, "select", "table"); err != nil {
		t.Fatalf("runQuery: %v", err)
	}
	if !strings.Contains(out.String(), "(0 row(s)") {
		t.Errorf("expected a 0-row footer, got %q", out.String())
	}
}
