package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBErrKind classifies a database failure into a small, stable set the UI can
// switch on. Raw driver error strings/codes are matched exactly once — inside
// the classify functions in this file — never in the tui layer.
type DBErrKind int

const (
	DBErrInternal         DBErrKind = iota // unclassified / unexpected
	DBErrAuthFailed                        // bad credentials
	DBErrConnRefused                       // server unreachable / refused
	DBErrQuerySyntax                       // malformed SQL
	DBErrQueryConstraint                   // constraint / integrity violation
	DBErrObjectExists                      // CREATE of something already present
	DBErrObjectMissing                     // reference to something absent
	DBErrTimeout                           // deadline exceeded
	DBErrCanceled                          // context canceled by the user
	DBErrInvalidInput                      // caller-supplied name/arg rejected before SQL
	DBErrUnsupported                       // operation not available on this engine
	DBErrObjectInUse                       // DROP blocked because the object is in use
	DBErrDependentObjects                  // DROP blocked by objects the target owns/depends on
)

// DBError is the typed error every db operation returns on failure. Each kind
// carries plain-language text the UI shows verbatim: a Title, a Detail, and a
// Hint suggesting the next action/key. Err wraps the underlying driver error
// for logging.
type DBError struct {
	Kind   DBErrKind
	Title  string
	Detail string
	Hint   string
	Err    error
}

// Error implements the error interface.
func (e *DBError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Title, e.Err)
	}
	return e.Title
}

// Unwrap exposes the wrapped driver error for errors.Is/As.
func (e *DBError) Unwrap() error { return e.Err }

// classify maps a raw error with no engine-specific detail into a typed
// DBError. It handles the cases every engine shares — context cancellation,
// deadlines, and a refused TCP connection — and falls back to Internal. The
// per-engine classifiers below delegate here once they have ruled out driver
// codes they alone understand.
func classify(err error) *DBError {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled):
		return errCanceled(err)
	case errors.Is(err, context.DeadlineExceeded):
		return errTimeout(err)
	case isConnRefused(err):
		return errConnRefused(err)
	}
	return errInternal(err)
}

// isConnRefused reports whether err is a refused/again TCP connection, checked
// both by errno and by the driver's stringified message (some wrap the syscall
// out of reach of errors.Is).
func isConnRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "connection refused") || strings.Contains(s, "no such host")
}

// classifyPostgres maps a pgx error to a typed DBError by SQLSTATE, falling back
// to the shared classifier for connection/context errors. It is the single
// place Postgres error codes are interpreted.
func classifyPostgres(err error) *DBError {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.Code == "28P01", pg.Code == "28000":
			return errAuthFailed(err)
		case pg.Code == "57014":
			return errCanceled(err)
		case pg.Code == "42601":
			return errQuerySyntax(pg.Message, int(pg.Position), err)
		case strings.HasPrefix(pg.Code, "08"):
			return errConnRefused(err)
		case strings.HasPrefix(pg.Code, "23"):
			return errQueryConstraint(pg.Message, err)
		case pg.Code == "42P04", pg.Code == "42710", pg.Code == "42P06",
			pg.Code == "42P07", pg.Code == "42723":
			return errObjectExists(pg.Message, err)
		case pg.Code == "3D000", pg.Code == "3F000", pg.Code == "42P01",
			pg.Code == "42704", pg.Code == "42883":
			return errObjectMissing(pg.Message, err)
		case pg.Code == "55006": // object_in_use — e.g. DROP DATABASE with live sessions
			return errObjectInUse(pg.Message, err)
		case pg.Code == "2BP01": // dependent_objects_still_exist — DROP ROLE that owns objects
			return errDependentObjects(pg.Message, err)
		}
		// A recognised PgError we don't specialise still shouldn't leak raw:
		// wrap its message as Internal.
		return &DBError{Kind: DBErrInternal, Title: "Database error", Detail: pg.Message, Hint: "press [b] to go back", Err: err}
	}
	return classify(err)
}

// classifyMySQL maps a go-sql-driver error to a typed DBError by error number,
// falling back to the shared classifier. It is the single place MySQL/MariaDB
// error codes are interpreted.
func classifyMySQL(err error) *DBError {
	if err == nil {
		return nil
	}
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		switch my.Number {
		case 1045, 1044, 1698:
			return errAuthFailed(err)
		case 1317: // query execution interrupted
			return errCanceled(err)
		case 1064:
			return errQuerySyntax(my.Message, 0, err)
		case 1062, 1451, 1452, 1048, 1216, 1217, 1364:
			return errQueryConstraint(my.Message, err)
		case 1007, 1050, 1396: // db exists, table exists, CREATE USER of existing
			return errObjectExists(my.Message, err)
		case 1008, 1049, 1146, 1051, 1147: // drop-missing db/table, unknown db/table
			return errObjectMissing(my.Message, err)
		}
		return &DBError{Kind: DBErrInternal, Title: "Database error", Detail: my.Message, Hint: "press [b] to go back", Err: err}
	}
	return classify(err)
}

// classifySQLite maps a modernc.org/sqlite error to a typed DBError. SQLite
// exposes far less structure than the server engines, so classification leans on
// the driver's message text; the shared classifier covers the rest.
func classifySQLite(err error) *DBError {
	if err == nil {
		return nil
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "syntax error"):
		return errQuerySyntax(err.Error(), 0, err)
	case strings.Contains(s, "already exists"):
		return errObjectExists(err.Error(), err)
	case strings.Contains(s, "no such table"), strings.Contains(s, "no such column"):
		return errObjectMissing(err.Error(), err)
	case strings.Contains(s, "constraint failed"):
		return errQueryConstraint(err.Error(), err)
	}
	return classify(err)
}

func errInternal(err error) *DBError {
	return &DBError{
		Kind:   DBErrInternal,
		Title:  "Something went wrong",
		Detail: "The database returned an error we couldn't classify.",
		Hint:   "press [b] to go back",
		Err:    err,
	}
}

func errAuthFailed(err error) *DBError {
	return &DBError{
		Kind:   DBErrAuthFailed,
		Title:  "Authentication failed",
		Detail: "The database rejected the username or password.",
		Hint:   "check the credentials, or press [b] to pick another target",
		Err:    err,
	}
}

func errConnRefused(err error) *DBError {
	return &DBError{
		Kind:   DBErrConnRefused,
		Title:  "Couldn't reach the database",
		Detail: "Nothing accepted a connection at that host and port.",
		Hint:   "make sure the container is running, then press [r] to retry",
		Err:    err,
	}
}

func errQuerySyntax(msg string, position int, err error) *DBError {
	detail := msg
	if detail == "" {
		detail = "The statement couldn't be parsed."
	}
	if position > 0 {
		detail = fmt.Sprintf("%s (at position %d)", detail, position)
	}
	return &DBError{
		Kind:   DBErrQuerySyntax,
		Title:  "Syntax error",
		Detail: detail,
		Hint:   "fix the statement and run it again",
		Err:    err,
	}
}

func errQueryConstraint(msg string, err error) *DBError {
	if msg == "" {
		msg = "The statement violated a constraint."
	}
	return &DBError{
		Kind:   DBErrQueryConstraint,
		Title:  "Constraint violation",
		Detail: msg,
		Hint:   "adjust the data so it satisfies the constraint",
		Err:    err,
	}
}

func errObjectExists(msg string, err error) *DBError {
	if msg == "" {
		msg = "That object already exists."
	}
	return &DBError{
		Kind:   DBErrObjectExists,
		Title:  "Already exists",
		Detail: msg,
		Hint:   "pick a different name, or use the existing one",
		Err:    err,
	}
}

func errObjectMissing(msg string, err error) *DBError {
	if msg == "" {
		msg = "That object doesn't exist."
	}
	return &DBError{
		Kind:   DBErrObjectMissing,
		Title:  "Not found",
		Detail: msg,
		Hint:   "check the name, or press [r] to refresh the list",
		Err:    err,
	}
}

func errObjectInUse(msg string, err error) *DBError {
	if msg == "" {
		msg = "The object is currently in use."
	}
	return &DBError{
		Kind:   DBErrObjectInUse,
		Title:  "In use",
		Detail: msg,
		Hint:   "close other connections to it, then try again",
		Err:    err,
	}
}

func errDependentObjects(msg string, err error) *DBError {
	if msg == "" {
		msg = "Other objects still depend on it."
	}
	return &DBError{
		Kind:   DBErrDependentObjects,
		Title:  "Still has dependent objects",
		Detail: msg,
		Hint:   "reassign or drop the objects it owns first, then try again",
		Err:    err,
	}
}

func errTimeout(err error) *DBError {
	return &DBError{
		Kind:   DBErrTimeout,
		Title:  "The database timed out",
		Detail: "The operation took too long and was cancelled.",
		Hint:   "press [r] to retry",
		Err:    err,
	}
}

func errCanceled(err error) *DBError {
	return &DBError{
		Kind:   DBErrCanceled,
		Title:  "Cancelled",
		Detail: "The operation was cancelled before it finished.",
		Hint:   "press [r] to run it again",
		Err:    err,
	}
}

func errInvalidIdent(name, why string) *DBError {
	return &DBError{
		Kind:   DBErrInvalidInput,
		Title:  "Invalid name",
		Detail: fmt.Sprintf("%q can't be used as a database object name because %s.", name, why),
		Hint:   "use a shorter name with ordinary characters",
	}
}

func errUnsupported(op string, k Kind) *DBError {
	return &DBError{
		Kind:   DBErrUnsupported,
		Title:  "Not supported here",
		Detail: fmt.Sprintf("%s doesn't support %s.", k, op),
		Hint:   "press [b] to go back",
	}
}

// errNotADatabase reports that a path the user chose is not a SQLite database
// file (its header magic is wrong or it is unreadable).
func errNotADatabase(path string, err error) *DBError {
	return &DBError{
		Kind:   DBErrInvalidInput,
		Title:  "Not a SQLite database",
		Detail: fmt.Sprintf("%q doesn't look like a SQLite database file.", path),
		Hint:   "pick a valid .db/.sqlite file",
		Err:    err,
	}
}
