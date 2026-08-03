package explain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// Runner is the narrow slice of db.Engine that Explain needs. Keeping it this
// small is what lets every parser be tested against a literal fixture instead of
// a live database — the same pattern schema.Source and the scripting commands
// use.
type Runner interface {
	Kind() db.Kind
	Query(ctx context.Context, sql string) (db.Result, error)
}

// Options controls how a plan is captured.
type Options struct {
	// Analyze runs the statement and reports measured rows and timings instead of
	// the planner's estimates. It is opt-in because it really does execute the
	// statement — see Explain.
	Analyze bool
}

// Explain captures the plan for statement.
//
// With Analyze set, the engine *executes* the statement to measure it. On a
// DELETE that is not an analysis, it is a deletion, so a statement that writes is
// refused outright rather than wrapped in a transaction that would roll back:
// a rollback still fires triggers, consumes sequences and holds locks, so
// refusing is the honest answer.
func Explain(ctx context.Context, r Runner, statement string, opts Options) (Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return Plan{}, errors.New("nothing to explain — write a statement first")
	}

	kind := r.Kind()
	if opts.Analyze {
		if Writes(statement) {
			return Plan{}, errors.New("--analyze runs the statement to measure it, and this one writes: " +
				"drop --analyze to see the planner's estimates instead")
		}
		if kind == db.KindSQLite {
			return Plan{}, errors.New("SQLite reports a plan but never measures it, so --analyze has nothing to add")
		}
	}

	command, err := explainStatement(kind, statement, opts.Analyze)
	if err != nil {
		return Plan{}, err
	}

	res, err := r.Query(ctx, command)
	if err != nil {
		return Plan{}, fmt.Errorf("running the plan for this statement: %w", err)
	}

	plan := Plan{
		Kind:      kind,
		Statement: statement,
		Command:   command,
		Analyzed:  opts.Analyze,
		Raw:       rawText(res),
	}
	if plan.Raw == "" && len(res.Rows) == 0 {
		return Plan{}, errors.New("the engine returned an empty plan")
	}

	switch kind {
	case db.KindPostgres:
		err = parsePostgres(&plan)
	case db.KindMySQL, db.KindMariaDB:
		err = parseMySQL(&plan)
	case db.KindSQLite:
		err = parseSQLite(&plan, res)
	default:
		err = fmt.Errorf("%s has no query plan support in DBWiz", kind)
	}
	if err != nil {
		return Plan{}, err
	}
	if plan.Root == nil {
		return Plan{}, errors.New("the engine's plan had no steps in it")
	}
	return plan, nil
}

// explainStatement is the EXPLAIN dialect for each engine. The measured forms
// diverge the most: MySQL's is plain text rather than JSON, and MariaDB spells it
// ANALYZE rather than EXPLAIN ANALYZE.
func explainStatement(kind db.Kind, statement string, analyze bool) (string, error) {
	switch kind {
	case db.KindPostgres:
		if analyze {
			return "EXPLAIN (FORMAT JSON, ANALYZE) " + statement, nil
		}
		return "EXPLAIN (FORMAT JSON) " + statement, nil
	case db.KindMySQL:
		if analyze {
			return "EXPLAIN ANALYZE " + statement, nil
		}
		return "EXPLAIN FORMAT=JSON " + statement, nil
	case db.KindMariaDB:
		if analyze {
			return "ANALYZE FORMAT=JSON " + statement, nil
		}
		return "EXPLAIN FORMAT=JSON " + statement, nil
	case db.KindSQLite:
		return "EXPLAIN QUERY PLAN " + statement, nil
	default:
		return "", fmt.Errorf("%s has no query plan support in DBWiz", kind)
	}
}

// rawText joins the first column of every row, which is how all three engines
// hand back a textual plan: Postgres and MySQL as one JSON document, MySQL's
// measured form as an indented tree that may arrive as one row or several.
func rawText(res db.Result) string {
	var b strings.Builder
	for _, row := range res.Rows {
		if len(row) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(cellText(row[0]))
	}
	return b.String()
}

// cellText renders one result cell as text, matching how the db layer hands back
// driver values (strings, []byte for JSON columns, nil for NULL).
func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

// cellInt reads a cell as an integer, returning Unknown when it isn't one. The
// drivers are inconsistent about integer width, so every numeric shape is
// accepted.
func cellInt(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int32:
		return int64(t)
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case []byte:
		return parseInt(string(t))
	case string:
		return parseInt(t)
	default:
		return Unknown
	}
}

// readOnlyStarts are the statement kinds that only read. Anything starting with
// something else is treated as a write.
var readOnlyStarts = map[string]bool{
	"SELECT":   true,
	"WITH":     true,
	"TABLE":    true,
	"VALUES":   true,
	"SHOW":     true,
	"DESCRIBE": true,
	"DESC":     true,
}

// writeKeywords are words that mean the statement changes something. They are
// checked anywhere in the text, not just at the front, because a Postgres CTE can
// open with WITH and still end in an INSERT.
var writeKeywords = map[string]bool{
	"INSERT":   true,
	"UPDATE":   true,
	"DELETE":   true,
	"MERGE":    true,
	"TRUNCATE": true,
	"REPLACE":  true,
	"CREATE":   true,
	"ALTER":    true,
	"DROP":     true,
	"GRANT":    true,
	"REVOKE":   true,
	"CALL":     true,
	"DO":       true,
}

// Writes reports whether the statement changes anything — the gate on --analyze.
//
// It leans toward answering yes: string literals and comments are stripped first
// so that `SELECT 'delete me'` isn't misread, but a word like UPDATE anywhere in
// the remaining text is enough (`SELECT … FOR UPDATE` does take row locks, so
// counting it is right rather than merely cautious). Identifiers are matched
// whole, so a column named deleted_at is not a DELETE.
func Writes(statement string) bool {
	clean := stripLiterals(statement)
	words := sqlWords(clean)
	if len(words) == 0 {
		return false
	}
	if !readOnlyStarts[words[0]] {
		return true
	}
	for _, w := range words[1:] {
		if writeKeywords[w] {
			return true
		}
	}
	return false
}

// stripLiterals removes quoted strings and comments so that keyword matching sees
// only SQL structure.
func stripLiterals(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == '\'' || runes[i] == '"' || runes[i] == '`':
			quote := runes[i]
			i++
			for i < len(runes) && runes[i] != quote {
				i++
			}
			b.WriteByte(' ')
		case runes[i] == '-' && i+1 < len(runes) && runes[i+1] == '-':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case runes[i] == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i++
			b.WriteByte(' ')
		default:
			b.WriteRune(runes[i])
		}
	}
	return b.String()
}

// sqlWords splits text into upper-cased identifier-shaped words, so that
// deleted_at stays one word and never matches DELETE.
func sqlWords(s string) []string {
	return strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool {
		return !(r == '_' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}
