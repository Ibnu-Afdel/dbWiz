package db

import (
	"errors"
	"strings"
	"testing"
)

func strptr(s string) *string { return &s }

// TestBuildUpdatePostgres builds a quoted single-cell UPDATE with a PK WHERE.
func TestBuildUpdatePostgres(t *testing.T) {
	sql, err := BuildUpdate(KindPostgres, "shop", "orders",
		[]string{"id"}, []any{"7"}, "status", strptr("shipped"))
	if err != nil {
		t.Fatalf("BuildUpdate: %v", err)
	}
	want := `UPDATE "orders" SET "status" = 'shipped' WHERE "id" = '7'`
	if sql != want {
		t.Fatalf("got  %s\nwant %s", sql, want)
	}
}

// TestBuildUpdateMySQLQualifies schema-qualifies the table and backtick-quotes.
func TestBuildUpdateMySQLQualifies(t *testing.T) {
	sql, err := BuildUpdate(KindMySQL, "shop", "orders",
		[]string{"id"}, []any{"7"}, "note", strptr("hi"))
	if err != nil {
		t.Fatalf("BuildUpdate: %v", err)
	}
	if !strings.HasPrefix(sql, "UPDATE `shop`.`orders` SET `note` = 'hi' WHERE `id` = '7'") {
		t.Fatalf("unexpected: %s", sql)
	}
}

// TestBuildUpdateNullValue renders a nil new value as the NULL keyword, unquoted.
func TestBuildUpdateNullValue(t *testing.T) {
	sql, err := BuildUpdate(KindSQLite, "", "t", []string{"id"}, []any{"1"}, "col", nil)
	if err != nil {
		t.Fatalf("BuildUpdate: %v", err)
	}
	if !strings.Contains(sql, `SET "col" = NULL`) {
		t.Fatalf("expected NULL keyword, got: %s", sql)
	}
}

// TestBuildUpdateNullKeyUsesIsNull turns a nil key value into IS NULL, since
// "= NULL" never matches.
func TestBuildUpdateNullKeyUsesIsNull(t *testing.T) {
	sql, err := BuildUpdate(KindPostgres, "", "t", []string{"a", "b"}, []any{nil, "2"}, "c", strptr("x"))
	if err != nil {
		t.Fatalf("BuildUpdate: %v", err)
	}
	if !strings.Contains(sql, `"a" IS NULL`) || !strings.Contains(sql, `"b" = '2'`) {
		t.Fatalf("composite/null WHERE wrong: %s", sql)
	}
}

// TestBuildUpdateNoPK refuses when there's no identity, so the browser can decline.
func TestBuildUpdateNoPK(t *testing.T) {
	_, err := BuildUpdate(KindPostgres, "", "t", nil, nil, "c", strptr("x"))
	if !errors.Is(err, ErrNoRowIdentity) {
		t.Fatalf("expected ErrNoRowIdentity, got %v", err)
	}
}

// TestBuildDelete builds a PK-scoped DELETE and refuses without a key.
func TestBuildDelete(t *testing.T) {
	sql, err := BuildDelete(KindPostgres, "", "orders", []string{"id"}, []any{"7"})
	if err != nil {
		t.Fatalf("BuildDelete: %v", err)
	}
	if sql != `DELETE FROM "orders" WHERE "id" = '7'` {
		t.Fatalf("unexpected: %s", sql)
	}
	if _, err := BuildDelete(KindPostgres, "", "orders", nil, nil); !errors.Is(err, ErrNoRowIdentity) {
		t.Fatalf("expected ErrNoRowIdentity, got %v", err)
	}
}

// TestBuildInsert names only the given columns (so omitted ones take their DB
// default) and renders a nil value as NULL.
func TestBuildInsert(t *testing.T) {
	sql, err := BuildInsert(KindMySQL, "shop", "orders",
		[]string{"name", "note"}, []*string{strptr("widget"), nil})
	if err != nil {
		t.Fatalf("BuildInsert: %v", err)
	}
	want := "INSERT INTO `shop`.`orders` (`name`, `note`) VALUES ('widget', NULL)"
	if sql != want {
		t.Fatalf("got  %s\nwant %s", sql, want)
	}
	if _, err := BuildInsert(KindPostgres, "", "t", nil, nil); err == nil {
		t.Fatal("expected an error with no columns")
	}
}

// TestBuildUpdateQuotesInjection: a value with a quote is escaped, not breaking out.
func TestBuildUpdateQuotesInjection(t *testing.T) {
	sql, err := BuildUpdate(KindPostgres, "", "t", []string{"id"}, []any{"1"}, "name", strptr("O'Brien'; DROP TABLE t;--"))
	if err != nil {
		t.Fatalf("BuildUpdate: %v", err)
	}
	if !strings.Contains(sql, `'O''Brien''; DROP TABLE t;--'`) {
		t.Fatalf("single quotes not doubled: %s", sql)
	}
}
