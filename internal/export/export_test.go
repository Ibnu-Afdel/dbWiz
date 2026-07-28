package export

import (
	"encoding/json"
	"strings"
	"testing"
)

var (
	cols = []string{"id", "name", "note"}
	rows = [][]any{
		{int64(1), "alice", nil},               // NULL note
		{int64(2), []byte("bob"), "has,comma"}, // []byte cell + a value needing quoting
	}
)

// TestCSVHeaderAndRows verifies the header comes first and NULL renders as an
// empty field while an embedded comma is quoted per RFC 4180.
func TestCSVHeaderAndRows(t *testing.T) {
	out, err := CSV(cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	lines := strings.Split(strings.TrimRight(got, "\r\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d lines:\n%s", len(lines), got)
	}
	if strings.TrimRight(lines[0], "\r") != "id,name,note" {
		t.Errorf("header = %q", lines[0])
	}
	// NULL note → trailing empty field.
	if !strings.HasPrefix(lines[1], "1,alice,") || strings.TrimRight(lines[1], "\r") != "1,alice," {
		t.Errorf("NULL should be an empty field, row = %q", lines[1])
	}
	// []byte rendered as text; the comma value is quoted.
	if !strings.Contains(lines[2], "bob") || !strings.Contains(lines[2], `"has,comma"`) {
		t.Errorf("row = %q, want bob and a quoted comma value", lines[2])
	}
}

// TestCSVEmptyIsHeaderOnly verifies exporting zero rows still yields the header.
func TestCSVEmptyIsHeaderOnly(t *testing.T) {
	out, err := CSV(cols, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(string(out), "\r\n") != "id,name,note" {
		t.Errorf("empty export should be header-only, got %q", out)
	}
}

// TestJSONRowObjects verifies JSON is an array of column-keyed objects, with NULL
// as JSON null and []byte decoded to a string.
func TestJSONRowObjects(t *testing.T) {
	out, err := JSON(cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 objects, got %d", len(got))
	}
	if got[0]["name"] != "alice" {
		t.Errorf("row0 name = %v", got[0]["name"])
	}
	if v, ok := got[0]["note"]; !ok || v != nil {
		t.Errorf("NULL should marshal as JSON null (present, nil), got %v ok=%v", v, ok)
	}
	if got[1]["name"] != "bob" {
		t.Errorf("[]byte cell should decode to a string, got %v", got[1]["name"])
	}
}

// TestJSONEmptyIsArray verifies an empty result is an empty array, not null.
func TestJSONEmptyIsArray(t *testing.T) {
	out, err := JSON(cols, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "[]" {
		t.Errorf("empty JSON export should be [], got %q", out)
	}
}
