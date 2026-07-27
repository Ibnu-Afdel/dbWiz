package db

import (
	"context"
	"errors"
	"syscall"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyPostgres(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want DBErrKind
	}{
		{"nil", nil, 0}, // handled specially below
		{"auth", &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}, DBErrAuthFailed},
		{"auth invalid authz", &pgconn.PgError{Code: "28000"}, DBErrAuthFailed},
		{"canceled server", &pgconn.PgError{Code: "57014"}, DBErrCanceled},
		{"syntax", &pgconn.PgError{Code: "42601", Message: "syntax error", Position: 8}, DBErrQuerySyntax},
		{"conn class 08", &pgconn.PgError{Code: "08006"}, DBErrConnRefused},
		{"constraint unique", &pgconn.PgError{Code: "23505", Message: "duplicate key"}, DBErrQueryConstraint},
		{"constraint fk", &pgconn.PgError{Code: "23503"}, DBErrQueryConstraint},
		{"db exists", &pgconn.PgError{Code: "42P04"}, DBErrObjectExists},
		{"role exists", &pgconn.PgError{Code: "42710"}, DBErrObjectExists},
		{"table exists", &pgconn.PgError{Code: "42P07"}, DBErrObjectExists},
		{"missing catalog", &pgconn.PgError{Code: "3D000"}, DBErrObjectMissing},
		{"missing table", &pgconn.PgError{Code: "42P01"}, DBErrObjectMissing},
		{"missing role", &pgconn.PgError{Code: "42704"}, DBErrObjectMissing},
		{"unspecialised pg", &pgconn.PgError{Code: "53300", Message: "too many"}, DBErrInternal},
		{"ctx canceled", context.Canceled, DBErrCanceled},
		{"ctx deadline", context.DeadlineExceeded, DBErrTimeout},
		{"conn refused errno", syscall.ECONNREFUSED, DBErrConnRefused},
		{"plain", errors.New("boom"), DBErrInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyPostgres(tc.err)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("classifyPostgres(nil) = %v, want nil", got)
				}
				return
			}
			if got == nil || got.Kind != tc.want {
				t.Fatalf("classifyPostgres(%v).Kind = %v, want %v", tc.err, kindOf(got), tc.want)
			}
			if !errors.Is(got, tc.err) {
				t.Errorf("classified error should unwrap to the original driver error")
			}
		})
	}
}

func TestClassifyMySQL(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want DBErrKind
	}{
		{"auth", &mysql.MySQLError{Number: 1045, Message: "Access denied"}, DBErrAuthFailed},
		{"db access", &mysql.MySQLError{Number: 1044}, DBErrAuthFailed},
		{"interrupted", &mysql.MySQLError{Number: 1317}, DBErrCanceled},
		{"syntax", &mysql.MySQLError{Number: 1064, Message: "You have an error in your SQL syntax"}, DBErrQuerySyntax},
		{"dup entry", &mysql.MySQLError{Number: 1062}, DBErrQueryConstraint},
		{"fk", &mysql.MySQLError{Number: 1452}, DBErrQueryConstraint},
		{"db exists", &mysql.MySQLError{Number: 1007}, DBErrObjectExists},
		{"table exists", &mysql.MySQLError{Number: 1050}, DBErrObjectExists},
		{"user exists", &mysql.MySQLError{Number: 1396}, DBErrObjectExists},
		{"drop missing db", &mysql.MySQLError{Number: 1008}, DBErrObjectMissing},
		{"unknown db", &mysql.MySQLError{Number: 1049}, DBErrObjectMissing},
		{"unknown table", &mysql.MySQLError{Number: 1146}, DBErrObjectMissing},
		{"unspecialised", &mysql.MySQLError{Number: 9999, Message: "weird"}, DBErrInternal},
		{"ctx canceled", context.Canceled, DBErrCanceled},
		{"plain", errors.New("boom"), DBErrInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyMySQL(tc.err)
			if got == nil || got.Kind != tc.want {
				t.Fatalf("classifyMySQL(%v).Kind = %v, want %v", tc.err, kindOf(got), tc.want)
			}
		})
	}
}

func TestClassifySQLite(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want DBErrKind
	}{
		{"syntax", errors.New(`near "slect": syntax error`), DBErrQuerySyntax},
		{"exists", errors.New("table foo already exists"), DBErrObjectExists},
		{"missing", errors.New("no such table: bar"), DBErrObjectMissing},
		{"constraint", errors.New("UNIQUE constraint failed: t.id"), DBErrQueryConstraint},
		{"plain", errors.New("disk I/O error"), DBErrInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySQLite(tc.err)
			if got == nil || got.Kind != tc.want {
				t.Fatalf("classifySQLite(%v).Kind = %v, want %v", tc.err, kindOf(got), tc.want)
			}
		})
	}
}

// kindOf is a nil-safe accessor for error messages in the tests above.
func kindOf(e *DBError) any {
	if e == nil {
		return "<nil>"
	}
	return e.Kind
}
