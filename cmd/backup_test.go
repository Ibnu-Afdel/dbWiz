package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// fakeExec captures the docker-exec request and optionally streams canned output
// or drains stdin, so dump/restore are testable without a live Docker.
func fakeExec(t *testing.T, stdout string, drainStdin *string, err error) *docker.ExecOptions {
	t.Helper()
	captured := &docker.ExecOptions{}
	old := execDocker
	execDocker = func(_ context.Context, opts docker.ExecOptions) error {
		*captured = opts
		if opts.Stdout != nil && stdout != "" {
			_, _ = opts.Stdout.Write([]byte(stdout))
		}
		if opts.Stdin != nil && drainStdin != nil {
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(opts.Stdin)
			*drainStdin = buf.String()
		}
		return err
	}
	t.Cleanup(func() { execDocker = old })
	return captured
}

func pgTarget() cliTarget {
	return cliTarget{
		container: docker.Container{Name: "pg-dev", Engine: docker.EnginePostgres, State: docker.StateRunning},
		kind:      db.KindPostgres,
		target:    db.Target{User: "postgres", Password: "s3cret"},
	}
}

// TestDumpSpecPostgres builds a pg_dump invocation forwarding the password by
// env (never in the argv).
func TestDumpSpecPostgres(t *testing.T) {
	spec, err := dumpSpec(pgTarget(), "shop", "s3cret")
	if err != nil {
		t.Fatalf("dumpSpec: %v", err)
	}
	if !reflect.DeepEqual(spec.args, []string{"pg_dump", "-U", "postgres", "shop"}) {
		t.Errorf("args = %v", spec.args)
	}
	if !reflect.DeepEqual(spec.env, []string{"PGPASSWORD=s3cret"}) {
		t.Errorf("env = %v", spec.env)
	}
	if strings.Contains(strings.Join(spec.args, " "), "s3cret") {
		t.Error("password must not appear in argv")
	}
}

// TestRestoreSpecMySQL builds a mysql invocation reading from stdin, MYSQL_PWD in
// env.
func TestRestoreSpecMySQL(t *testing.T) {
	tgt := cliTarget{
		container: docker.Container{Name: "my", Engine: docker.EngineMySQL, State: docker.StateRunning},
		kind:      db.KindMySQL,
		target:    db.Target{User: "root"},
	}
	spec, err := restoreSpec(tgt, "shop", "pw")
	if err != nil {
		t.Fatalf("restoreSpec: %v", err)
	}
	if !reflect.DeepEqual(spec.args, []string{"mysql", "-u", "root", "shop"}) {
		t.Errorf("args = %v", spec.args)
	}
	if !reflect.DeepEqual(spec.env, []string{"MYSQL_PWD=pw"}) {
		t.Errorf("env = %v", spec.env)
	}
}

// TestBackupSQLiteUnsupported rejects dump/restore on a SQLite target with a
// clear reason.
func TestBackupSQLiteUnsupported(t *testing.T) {
	sq := cliTarget{sqlite: true, kind: db.KindSQLite, target: db.Target{Path: "/tmp/x.db"}}
	if _, err := dumpSpec(sq, "shop", ""); err == nil || !strings.Contains(err.Error(), "SQLite") {
		t.Errorf("dump: expected a SQLite-unsupported error, got %v", err)
	}
	if _, err := restoreSpec(sq, "shop", ""); err == nil || !strings.Contains(err.Error(), "SQLite") {
		t.Errorf("restore: expected a SQLite-unsupported error, got %v", err)
	}
}

// TestBackupDashName rejects a database name a CLI would read as a flag.
func TestBackupDashName(t *testing.T) {
	if _, err := dumpSpec(pgTarget(), "-rf", ""); err == nil || !strings.Contains(err.Error(), "dash") {
		t.Errorf("expected a leading-dash rejection, got %v", err)
	}
}

// TestRunDumpToStdout streams the dump to the given writer and prints no
// confirmation there (that would corrupt the piped dump).
func TestRunDumpToStdout(t *testing.T) {
	captured := fakeExec(t, "-- dump sql --\n", nil, nil)
	var stdout, stderr bytes.Buffer
	if err := runDump(context.Background(), &stdout, &stderr, pgTarget(), "shop", "", ""); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	if stdout.String() != "-- dump sql --\n" {
		t.Errorf("dump not streamed to stdout: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("no confirmation should print when dumping to stdout, got %q", stderr.String())
	}
	if captured.Container != "pg-dev" || captured.Args[0] != "pg_dump" {
		t.Errorf("unexpected exec: %+v", captured)
	}
}

// TestRunDumpToFile writes the dump to a file and confirms on stderr.
func TestRunDumpToFile(t *testing.T) {
	fakeExec(t, "-- dump --", nil, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "shop.sql")
	var stdout, stderr bytes.Buffer
	if err := runDump(context.Background(), &stdout, &stderr, pgTarget(), "shop", "", path); err != nil {
		t.Fatalf("runDump: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "-- dump --" {
		t.Fatalf("file not written: %v / %q", err, string(data))
	}
	if !strings.Contains(stderr.String(), "Dumped shop") {
		t.Errorf("expected a file confirmation on stderr, got %q", stderr.String())
	}
}

// TestRunRestoreStreamsFile feeds the dump file into the container over stdin.
func TestRunRestoreStreamsFile(t *testing.T) {
	var seen string
	captured := fakeExec(t, "", &seen, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "dump.sql")
	if err := os.WriteFile(path, []byte("CREATE TABLE t(x int);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runRestore(context.Background(), &out, pgTarget(), "shop", path, "pw"); err != nil {
		t.Fatalf("runRestore: %v", err)
	}
	if seen != "CREATE TABLE t(x int);\n" {
		t.Errorf("dump file not streamed to stdin: %q", seen)
	}
	if captured.Args[0] != "psql" || !strings.Contains(out.String(), "Restored") {
		t.Errorf("unexpected restore: %+v / %q", captured, out.String())
	}
}

// TestRunRestoreMissingFile fails clearly before invoking docker.
func TestRunRestoreMissingFile(t *testing.T) {
	called := false
	old := execDocker
	execDocker = func(context.Context, docker.ExecOptions) error { called = true; return nil }
	t.Cleanup(func() { execDocker = old })

	err := runRestore(context.Background(), new(bytes.Buffer), pgTarget(), "shop", "/no/such/file.sql", "")
	if err == nil {
		t.Fatal("expected an error opening a missing file")
	}
	if called {
		t.Error("docker must not be invoked when the dump file can't be opened")
	}
}
