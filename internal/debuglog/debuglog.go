// Package debuglog is an opt-in diagnostic log. It is off unless DBWIZ_DEBUG=1,
// and when on it appends to $XDG_STATE_HOME/dbwiz/dbwiz.log (default
// ~/.local/state/dbwiz/dbwiz.log). It records the docker commands and SQL DBWiz
// runs so a user can see what happened after the fact — but it is built to never
// write a password: SQL is passed through Redact first, and the connection log
// takes the target's fields individually, with no password parameter at all, so
// a secret cannot reach the writer even by mistake.
//
// It is a leaf package: db, docker, and tui may import it; it imports none of
// them, preserving the one-way dependency direction.
package debuglog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	mu sync.Mutex
	// w is the log sink. A nil w means logging is disabled, which is the default
	// and makes every entry point a cheap no-op.
	w io.Writer
)

// init enables logging only when explicitly opted in, so the default build path
// opens no files and writes nothing.
func init() {
	if os.Getenv("DBWIZ_DEBUG") != "1" {
		return
	}
	f, err := openLogFile()
	if err != nil {
		// Debug logging is best-effort: a failure to open the file must never
		// break the app, so we silently stay disabled.
		return
	}
	w = f
	logf("=== dbwiz debug log opened %s (pid %d) ===", time.Now().Format(time.RFC3339), os.Getpid())
}

// Enabled reports whether debug logging is active.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return w != nil
}

// logPath returns the XDG state-dir path for the log file:
// $XDG_STATE_HOME/dbwiz/dbwiz.log, falling back to ~/.local/state/dbwiz.
func logPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "dbwiz", "dbwiz.log"), nil
}

// openLogFile ensures the state directory exists and opens the log for appending.
func openLogFile() (*os.File, error) {
	path, err := logPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// logf writes one timestamped line if logging is enabled. Callers hold no lock;
// logf takes it.
func logf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if w == nil {
		return
	}
	fmt.Fprintf(w, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

// LogExec records an external command DBWiz shelled out to (e.g. the docker CLI).
// Command arguments never carry a password, so they are logged verbatim.
func LogExec(name string, args []string) {
	logf("exec: %s %s", name, strings.Join(args, " "))
}

// LogSQL records a statement DBWiz executed, with any password literal redacted
// first so a CREATE ROLE/USER … PASSWORD never lands in the log.
func LogSQL(engine, sql string) {
	logf("sql[%s]: %s", engine, Redact(collapse(sql)))
}

// LogConnect records a connection attempt. It deliberately has no password
// parameter: the caller passes only non-secret fields, so a password cannot be
// logged here even by accident.
func LogConnect(engine, host string, port int, user, database string) {
	logf("connect[%s]: host=%s port=%d user=%s db=%s", engine, host, port, user, database)
}

// passwordLiteral matches a single-quoted SQL string literal that follows a
// PASSWORD or IDENTIFIED BY clause (the only ways DBWiz puts a secret into SQL,
// since a password can't be parameterised in CREATE ROLE/USER). The literal body
// allows doubled ” escapes.
var passwordLiteral = regexp.MustCompile(`(?i)((?:PASSWORD|IDENTIFIED\s+BY)\s+)'(?:[^']|'')*'`)

// Redact replaces the string literal after any PASSWORD / IDENTIFIED BY clause
// with '***', so account-creation SQL can be logged without leaking the secret.
// It is exported so the redaction guarantee can be unit-tested directly.
func Redact(sql string) string {
	return passwordLiteral.ReplaceAllString(sql, "$1'***'")
}

// collapse flattens a multi-line statement to a single log line.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
