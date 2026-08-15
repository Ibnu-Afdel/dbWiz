package sqlfmt

import (
	"strings"
	"testing"
)

// tokenStream re-tokenizes s and reduces it to the sequence a semantic
// comparison actually cares about: keyword text (already case-folded by
// lex), and the exact text of everything else. Two statements that differ
// only in whitespace, newlines, or keyword casing produce identical streams.
func tokenStream(t *testing.T, s string) []token {
	t.Helper()
	return lex(s)
}

func assertSameMeaning(t *testing.T, original string) {
	t.Helper()
	formatted := Format(original)
	want := tokenStream(t, original)
	got := tokenStream(t, formatted)
	if len(want) != len(got) {
		t.Fatalf("token count changed: %d -> %d\noriginal:\n%s\nformatted:\n%s", len(want), len(got), original, formatted)
	}
	for i := range want {
		if want[i].kind != got[i].kind || want[i].text != got[i].text {
			t.Fatalf("token %d changed: %+v -> %+v\noriginal:\n%s\nformatted:\n%s", i, want[i], got[i], original, formatted)
		}
	}
}

// TestSafetyInvariant is the property the whole package exists to guarantee:
// formatting only ever moves whitespace and cases keywords, never rewrites
// what a statement means.
func TestSafetyInvariant(t *testing.T) {
	stmts := []string{
		`select id, name from users where active = true`,
		`SELECT u.id, u.name, o.total FROM users u JOIN orders o ON o.user_id = u.id WHERE o.total > 100 ORDER BY o.total DESC LIMIT 10`,
		`insert into users (id, name, email, created_at) values (1, 'Ada', 'ada@example.com', now())`,
		`insert into t (a, b) values (1, 2), (3, 4), (5, 6)`,
		`update users set name = 'Ada Lovelace', updated_at = now() where id = 1`,
		`delete from sessions where expires_at < now()`,
		`with recent as (select id from orders where created_at > now() - interval '1 day') select * from recent`,
		`select * from users where name = 'select this'`, // keyword-looking text inside a string
		`select 1 -- trailing comment
from dual`,
		`/* leading comment */ select 1`,
		`select count(*), sum(amount), avg(amount) from payments group by customer_id having count(*) > 1`,
		`select a.x, b.y from a, b where a.id = b.id`,
		`create table t (id serial primary key, name text not null)`,
		`select "My Column" from "My Table"`,
		`select 1; select 2;`,
		`select price::numeric from items`,
	}
	for _, s := range stmts {
		assertSameMeaning(t, s)
	}
}

// TestIdempotent checks Format(Format(s)) == Format(s) — reformatting
// already-formatted SQL is a no-op, so hitting ⌥f twice never churns the
// buffer.
func TestIdempotent(t *testing.T) {
	stmts := []string{
		`select id, name, email, created_at, updated_at, last_login_at from users where active = true`,
		`SELECT u.id, u.name, o.total FROM users u JOIN orders o ON o.user_id = u.id WHERE o.total > 100`,
		`insert into t (a, b) values (1, 2), (3, 4)`,
		`update users set name = 'x' where id = 1`,
		`select 1`,
	}
	for _, s := range stmts {
		once := Format(s)
		twice := Format(once)
		if once != twice {
			t.Fatalf("not idempotent:\nonce:\n%s\ntwice:\n%s", once, twice)
		}
	}
}

func TestClauseBreaks(t *testing.T) {
	// TRUE/FALSE are recognized keywords (cased like every other keyword) —
	// only string, quoted-identifier, and comment content is ever left as
	// typed.
	got := Format(`select id, name from users where active = true order by name`)
	want := "SELECT id, name\nFROM users\nWHERE active = TRUE\nORDER BY name"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestShortSelectListStaysOneLine checks the *conditional* half of list
// breaking: clause keywords (SELECT/FROM/…) always get their own line — that
// part isn't gated on length — but a short column list doesn't also explode
// into one-column-per-line.
func TestShortSelectListStaysOneLine(t *testing.T) {
	got := Format(`select id, name from users`)
	if strings.Contains(got, "  id") {
		t.Fatalf("expected a short column list to stay on SELECT's line, got:\n%s", got)
	}
	want := "SELECT id, name\nFROM users"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestLongSelectBreaksOneColumnPerLine(t *testing.T) {
	got := Format(`select id, first_name, last_name, email_address, phone_number, created_at from users`)
	want := "SELECT\n  id,\n  first_name,\n  last_name,\n  email_address,\n  phone_number,\n  created_at\nFROM users"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMultiRowInsertBreaksEachTuple(t *testing.T) {
	got := Format(`insert into t (a, b) values (1, 2), (3, 4), (5, 6)`)
	want := "INSERT INTO t (a, b)\nVALUES (1, 2),\n  (3, 4),\n  (5, 6)"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSingleRowInsertStaysOneLine(t *testing.T) {
	got := Format(`insert into t (a, b) values (1, 2)`)
	want := "INSERT INTO t (a, b)\nVALUES (1, 2)"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJoinAndOnBreak(t *testing.T) {
	got := Format(`select o.id from orders o join customers c on o.customer_id = c.id`)
	want := "SELECT o.id\nFROM orders o\nJOIN customers c\n  ON o.customer_id = c.id"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFunctionCallHasNoInnerSpace(t *testing.T) {
	got := Format(`select count(*) from t`)
	if !strings.Contains(got, "count(*)") {
		t.Fatalf("expected count(*) with no space, got: %s", got)
	}
}

func TestMultipleStatementsFormatIndependently(t *testing.T) {
	got := Format(`select 1; select 2;`)
	want := "SELECT 1;\n\nSELECT 2;"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDollarQuotedBodyPassesThroughUnchanged(t *testing.T) {
	src := `create function f() returns void as $$
begin
  select 1;
end;
$$ language plpgsql`
	if got := Format(src); got != src {
		t.Fatalf("expected dollar-quoted body untouched:\ngot:\n%s\nwant:\n%s", got, src)
	}
}

func TestPlaceholderIsNotMistakenForDollarQuote(t *testing.T) {
	got := Format(`select * from users where id = $1 and org_id = $2`)
	if got == `select * from users where id = $1 and org_id = $2` {
		t.Fatalf("expected placeholders to still be formatted, got passthrough: %s", got)
	}
	if !strings.Contains(got, "$1") || !strings.Contains(got, "$2") {
		t.Fatalf("placeholders lost: %s", got)
	}
}

func TestEmptyAndWhitespaceInput(t *testing.T) {
	if got := Format(""); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if got := Format("   \n  "); got != "   \n  " {
		t.Fatalf("got %q, want unchanged whitespace", got)
	}
}

func TestUnterminatedStringPassesThroughRatherThanPanicking(t *testing.T) {
	src := `select 'unterminated`
	got := Format(src) // must not panic; content is preserved either way
	if !strings.Contains(got, "unterminated") {
		t.Fatalf("lost content: %s", got)
	}
}

func TestCommentOwnLine(t *testing.T) {
	got := Format("select 1 -- note\nfrom dual")
	want := "SELECT 1\n-- note\nFROM dual"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
