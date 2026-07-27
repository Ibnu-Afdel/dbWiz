package db

import (
	"strings"
	"testing"
)

func TestValidateIdent(t *testing.T) {
	ok := []string{"users", "my_table", "Ünïcode", "a.b", strings.Repeat("x", 64)}
	for _, name := range ok {
		if err := validateIdent(name); err != nil {
			t.Errorf("validateIdent(%q) = %v, want nil", name, err)
		}
	}
	bad := []string{"", strings.Repeat("x", 65), "a\x00b", "line\nbreak", "tab\tchar"}
	for _, name := range bad {
		if err := validateIdent(name); err == nil {
			t.Errorf("validateIdent(%q) = nil, want error", name)
		}
	}
}

func TestQuoting(t *testing.T) {
	// Embedded quote characters must be doubled, not dropped, so an injected
	// identifier can't break out of its quotes.
	if got := quotePGIdent(`a"b`); got != `"a""b"` {
		t.Errorf("quotePGIdent = %q", got)
	}
	if got := quoteMySQLIdent("a`b"); got != "`a``b`" {
		t.Errorf("quoteMySQLIdent = %q", got)
	}
	if got := quoteSQLiteIdent(`a"b`); got != `"a""b"` {
		t.Errorf("quoteSQLiteIdent = %q", got)
	}
	if got := quotePGLiteral("O'Brien"); got != "'O''Brien'" {
		t.Errorf("quotePGLiteral = %q", got)
	}
	if got := quoteMySQLLiteral(`a'\b`); got != `'a''\\b'` {
		t.Errorf("quoteMySQLLiteral = %q", got)
	}
}

func TestReturnsRows(t *testing.T) {
	yes := []string{
		"SELECT 1",
		"  select * from t",
		"\n-- comment\nSELECT 1",
		"/* c */ WITH x AS (SELECT 1) SELECT * FROM x",
		"SHOW TABLES",
		"PRAGMA table_info(t)",
		"insert into t values (1) returning id",
		"EXPLAIN SELECT 1",
	}
	for _, s := range yes {
		if !returnsRows(s) {
			t.Errorf("returnsRows(%q) = false, want true", s)
		}
	}
	no := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"CREATE TABLE t (id int)",
		"-- just a comment",
		"",
	}
	for _, s := range no {
		if returnsRows(s) {
			t.Errorf("returnsRows(%q) = true, want false", s)
		}
	}
}
