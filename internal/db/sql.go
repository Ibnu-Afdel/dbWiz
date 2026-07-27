package db

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	"github.com/Ibnu-Afdel/dbwiz/internal/debuglog"
)

// Pool sizing. DBWiz drives one interactive session, so a handful of connections
// is plenty; a small ceiling keeps idle server-side backends low and makes
// connection-leak regressions obvious in pool stats.
const (
	maxOpenConns = 5
	maxIdleConns = 2
	connMaxIdle  = 5 * time.Minute
)

// configurePool applies DBWiz's standard limits to a freshly opened pool.
func configurePool(pool *sql.DB) {
	pool.SetMaxOpenConns(maxOpenConns)
	pool.SetMaxIdleConns(maxIdleConns)
	pool.SetConnMaxIdleTime(connMaxIdle)
}

// runSQL executes arbitrary text against pool and shapes the outcome into a
// Result. It chooses QueryContext vs ExecContext by sniffing the statement (see
// returnsRows): row-returning statements stream into Result.Rows; everything
// else reports RowsAffected. The raw driver error is returned unwrapped so the
// caller can classify it with the engine-specific classifier. Cancellation
// flows through ctx to the driver, which cancels the statement server-side.
func runSQL(ctx context.Context, pool *sql.DB, text string) (Result, error) {
	// Single execution choke point, so redacted debug logging here covers every
	// statement (browse, query, and the password-bearing account SQL) in one place.
	debuglog.LogSQL("sql", text)
	start := time.Now()
	if returnsRows(text) {
		rows, err := pool.QueryContext(ctx, text)
		if err != nil {
			return Result{}, err
		}
		defer rows.Close()
		res, err := gather(rows)
		if err != nil {
			return Result{}, err
		}
		res.Duration = time.Since(start)
		return res, nil
	}
	r, err := pool.ExecContext(ctx, text)
	if err != nil {
		return Result{}, err
	}
	affected, _ := r.RowsAffected() // not all drivers report it; 0 is a fine default
	return Result{RowsAffected: affected, Duration: time.Since(start)}, nil
}

// gather reads every row of rs into a Result. A NULL cell is scanned as nil and
// left nil so the renderer can tell it apart from an empty string; []byte cells
// (MySQL returns text this way) are normalised to string so the tui sees one
// display type across engines.
func gather(rs *sql.Rows) (Result, error) {
	cols, err := rs.Columns()
	if err != nil {
		return Result{}, err
	}
	var out [][]any
	for rs.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			return Result{}, err
		}
		for i, c := range cells {
			if b, ok := c.([]byte); ok {
				cells[i] = string(b)
			}
		}
		out = append(out, cells)
	}
	if err := rs.Err(); err != nil {
		return Result{}, err
	}
	return Result{Columns: cols, Rows: out}, nil
}

// rowKeywords are the leading keywords of statements that return a result set.
var rowKeywords = map[string]bool{
	"SELECT": true, "WITH": true, "SHOW": true, "VALUES": true,
	"TABLE": true, "EXPLAIN": true, "PRAGMA": true, "DESCRIBE": true, "DESC": true,
}

// returnsRows reports whether text is a row-returning statement, so runSQL can
// pick QueryContext over ExecContext. It inspects the first keyword after any
// leading whitespace and comments, and also treats DML carrying a RETURNING
// clause (Postgres/SQLite) as row-returning. The heuristic is deliberately
// simple: a misclassification only changes which database/sql call is used, and
// both surface the same rows or the same error.
func returnsRows(text string) bool {
	s := stripLeading(text)
	if rowKeywords[firstWord(s)] {
		return true
	}
	return containsFold(text, " returning ")
}

// stripLeading drops leading whitespace, "-- line comments", and "/* block
// comments */" so firstWord sees the first real keyword.
func stripLeading(s string) string {
	for {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
				continue
			}
			return ""
		default:
			return s
		}
	}
}

// firstWord returns the leading run of letters of s, upper-cased.
func firstWord(s string) string {
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			end++
			continue
		}
		break
	}
	return strings.ToUpper(s[:end])
}

// containsFold reports whether substr appears in s, case-insensitively.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
