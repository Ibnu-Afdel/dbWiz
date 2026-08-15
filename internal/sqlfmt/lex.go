// Package sqlfmt reformats SQL text into readable, clause-per-line,
// keyword-cased SQL. It's a leaf package (no tui import), used by the SQL
// editor's ⌥f key (v5 2.2) and tested standalone.
//
// It is deliberately token-based, not grammar-based: it recognizes clause
// keywords, brackets, strings, and comments well enough to break and indent
// the statement shapes DBWiz's users actually write (SELECT/INSERT/UPDATE/
// DELETE, joins, CTEs), and leaves anything it doesn't recognize exactly as
// typed. It never rewrites the content of a string, identifier, or comment —
// only the whitespace and keyword casing around them — see sqlfmt_test.go's
// token-stream invariant for what "never" means in practice.
package sqlfmt

import "strings"

type tokKind int

const (
	tKeyword tokKind = iota // a recognized SQL word, text upper-cased (possibly merged, "GROUP BY")
	tIdent                  // table/column/function names, cast type names, etc. — verbatim
	tQuoted                 // "quoted" or `backtick` identifiers — verbatim, including the quotes
	tString                 // 'string literal' — verbatim, including the quotes
	tNumber                 // 123, 3.14, 1e10 — verbatim
	tPunct                  // , ( ) ; . :: = < > etc. — verbatim
	tComment                // -- line or /* block */ — verbatim, always own line in output
)

type token struct {
	kind tokKind
	text string
}

// keywordSet is every SQL word sqlfmt upper-cases when it recognizes one. It
// is deliberately broad (covers DDL/DML/constraint vocabulary) even though
// only a subset of these ever trigger a line break — casing every reserved
// word consistently is cheap and reads better than casing only the ones that
// also happen to break lines.
var keywordSet = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "GROUP": true, "BY": true,
	"ORDER": true, "HAVING": true, "LIMIT": true, "OFFSET": true,
	"INSERT": true, "INTO": true, "VALUES": true, "UPDATE": true, "SET": true,
	"DELETE": true, "CREATE": true, "ALTER": true, "DROP": true, "TABLE": true,
	"INDEX": true, "VIEW": true, "WITH": true, "UNION": true, "ALL": true,
	"RETURNING": true, "JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true,
	"FULL": true, "CROSS": true, "OUTER": true, "ON": true, "AND": true,
	"OR": true, "NOT": true, "NULL": true, "IS": true, "IN": true,
	"LIKE": true, "ILIKE": true, "BETWEEN": true, "AS": true, "DISTINCT": true,
	"ASC": true, "DESC": true, "CASE": true, "WHEN": true, "THEN": true,
	"ELSE": true, "END": true, "EXISTS": true, "PRIMARY": true,
	"FOREIGN": true, "KEY": true, "REFERENCES": true, "DEFAULT": true,
	"UNIQUE": true, "CONSTRAINT": true, "CHECK": true, "BEGIN": true,
	"COMMIT": true, "ROLLBACK": true, "TRANSACTION": true, "USING": true,
	"CASCADE": true, "RESTRICT": true, "TRUNCATE": true, "GRANT": true,
	"REVOKE": true, "IF": true, "TRUE": true, "FALSE": true, "EXPLAIN": true,
	"ANALYZE": true, "SCHEMA": true, "DATABASE": true, "COLUMN": true,
}

// composites merges runs of keyword tokens that only mean something as a
// phrase — "ORDER" and "BY" aren't independent clauses. Listed longest first
// so e.g. "LEFT OUTER JOIN" wins over "LEFT JOIN" matching a prefix of it.
var composites = [][]string{
	{"LEFT", "OUTER", "JOIN"}, {"RIGHT", "OUTER", "JOIN"}, {"FULL", "OUTER", "JOIN"},
	{"GROUP", "BY"}, {"ORDER", "BY"}, {"INSERT", "INTO"}, {"DELETE", "FROM"},
	{"UNION", "ALL"}, {"LEFT", "JOIN"}, {"RIGHT", "JOIN"}, {"INNER", "JOIN"},
	{"CROSS", "JOIN"}, {"FULL", "JOIN"}, {"IS", "NOT"}, {"NOT", "NULL"},
	{"NOT", "IN"}, {"NOT", "LIKE"}, {"PRIMARY", "KEY"}, {"FOREIGN", "KEY"},
}

// lex tokenizes sql. It never errors: unrecognized input (an unterminated
// string or comment) is captured as-is into a trailing token rather than
// dropped, so the caller always gets back everything it put in.
func lex(sql string) []token {
	var toks []token
	r := []rune(sql)
	n := len(r)
	i := 0

	for i < n {
		c := r[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++

		case c == '-' && i+1 < n && r[i+1] == '-':
			start := i
			for i < n && r[i] != '\n' {
				i++
			}
			toks = append(toks, token{tComment, string(r[start:i])})

		case c == '/' && i+1 < n && r[i+1] == '*':
			start := i
			i += 2
			for i+1 < n && !(r[i] == '*' && r[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2
			} else {
				i = n
			}
			toks = append(toks, token{tComment, string(r[start:i])})

		case c == '\'':
			start := i
			i++
			for i < n {
				if r[i] == '\'' {
					if i+1 < n && r[i+1] == '\'' { // '' escape
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			toks = append(toks, token{tString, string(r[start:i])})

		case c == '"':
			start := i
			i++
			for i < n {
				if r[i] == '"' {
					if i+1 < n && r[i+1] == '"' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			toks = append(toks, token{tQuoted, string(r[start:i])})

		case c == '`':
			start := i
			i++
			for i < n && r[i] != '`' {
				i++
			}
			if i < n {
				i++
			}
			toks = append(toks, token{tQuoted, string(r[start:i])})

		case isDigit(c):
			start := i
			for i < n && isDigit(r[i]) {
				i++
			}
			if i < n && r[i] == '.' && i+1 < n && isDigit(r[i+1]) {
				i++
				for i < n && isDigit(r[i]) {
					i++
				}
			}
			if i < n && (r[i] == 'e' || r[i] == 'E') {
				j := i + 1
				if j < n && (r[j] == '+' || r[j] == '-') {
					j++
				}
				if j < n && isDigit(r[j]) {
					i = j
					for i < n && isDigit(r[i]) {
						i++
					}
				}
			}
			toks = append(toks, token{tNumber, string(r[start:i])})

		case c == '$' && i+1 < n && isDigit(r[i+1]):
			// A numbered placeholder ($1, $2, ...) — Postgres's positional
			// parameter syntax. Lexed as one token so it renders tight
			// ("$1", not "$ 1"); distinct from the dollar-quote tags Format
			// bails out on entirely (hasDollarQuote), which never reach here.
			start := i
			i++
			for i < n && isDigit(r[i]) {
				i++
			}
			toks = append(toks, token{tNumber, string(r[start:i])})

		case isIdentStart(c):
			start := i
			for i < n && isIdentPart(r[i]) {
				i++
			}
			word := string(r[start:i])
			if keywordSet[strings.ToUpper(word)] {
				toks = append(toks, token{tKeyword, strings.ToUpper(word)})
			} else {
				toks = append(toks, token{tIdent, word})
			}

		case c == ':' && i+1 < n && r[i+1] == ':':
			toks = append(toks, token{tPunct, "::"})
			i += 2

		default:
			toks = append(toks, token{tPunct, string(c)})
			i++
		}
	}

	return mergeComposites(toks)
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || isDigit(r)
}

// mergeComposites folds runs of keyword tokens matching a phrase in
// composites into one keyword token ("GROUP", "BY" → "GROUP BY"), so the
// printer can treat the phrase as a single clause.
func mergeComposites(toks []token) []token {
	out := make([]token, 0, len(toks))
	for i := 0; i < len(toks); {
		matched := false
		for _, phrase := range composites {
			if matchesPhrase(toks, i, phrase) {
				out = append(out, token{tKeyword, strings.Join(phrase, " ")})
				i += len(phrase)
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, toks[i])
			i++
		}
	}
	return out
}

func matchesPhrase(toks []token, at int, phrase []string) bool {
	if at+len(phrase) > len(toks) {
		return false
	}
	for j, word := range phrase {
		t := toks[at+j]
		if t.kind != tKeyword || t.text != word {
			return false
		}
	}
	return true
}
