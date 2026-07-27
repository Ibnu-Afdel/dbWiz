package debuglog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// enableTo points the log sink at buf for one test and restores the prior sink
// (nil = disabled) afterwards.
func enableTo(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	mu.Lock()
	old := w
	w = buf
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		w = old
		mu.Unlock()
	})
}

// TestRedactPasswordLiterals is the security-critical unit: every way DBWiz puts
// a secret into SQL must come back masked, while ordinary SQL is untouched.
func TestRedactPasswordLiterals(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"postgres create role", `CREATE ROLE "app" LOGIN PASSWORD 'hunter2'`, `CREATE ROLE "app" LOGIN PASSWORD '***'`},
		{"postgres with keyword", `CREATE ROLE app WITH LOGIN PASSWORD 'p@ss word'`, `CREATE ROLE app WITH LOGIN PASSWORD '***'`},
		{"postgres encrypted", `ALTER ROLE app ENCRYPTED PASSWORD 'secret'`, `ALTER ROLE app ENCRYPTED PASSWORD '***'`},
		{"mysql identified by", "CREATE USER `app`@'%' IDENTIFIED BY 'hunter2'", "CREATE USER `app`@'%' IDENTIFIED BY '***'"},
		{"mysql identified by spaced", "CREATE USER x IDENTIFIED  BY  's3cr3t'", "CREATE USER x IDENTIFIED  BY  '***'"},
		{"lowercase keyword", `create role app login password 'abc'`, `create role app login password '***'`},
		{"doubled-quote escape in literal", `PASSWORD 'a''b'`, `PASSWORD '***'`},
		{"no password untouched", `SELECT * FROM users WHERE email = 'a@x.io'`, `SELECT * FROM users WHERE email = 'a@x.io'`},
		{"column named password untouched", `SELECT password FROM users`, `SELECT password FROM users`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Redact(c.in); got != c.want {
				t.Errorf("Redact(%q)\n = %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

// TestNeverLogsPassword drives the real logging entry points with secrets present
// and asserts the plaintext password never reaches the sink.
func TestNeverLogsPassword(t *testing.T) {
	var buf bytes.Buffer
	enableTo(t, &buf)

	const secret = "SUP3R-SECRET-PW"
	LogSQL("postgres", "CREATE ROLE app LOGIN PASSWORD '"+secret+"'")
	LogSQL("mysql", "CREATE USER app@'%' IDENTIFIED BY '"+secret+"'")
	LogConnect("postgres", "127.0.0.1", 5432, "postgres", "appdb")
	LogExec("docker", []string{"ps", "--format", "{{json .}}"})

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("password leaked into the debug log:\n%s", out)
	}
	// Sanity: the redacted marker and the non-secret fields are present.
	if !strings.Contains(out, "'***'") {
		t.Error("expected the redacted password marker in the log")
	}
	if !strings.Contains(out, "user=postgres") || strings.Contains(out, "password") {
		t.Errorf("connect log should carry user but no password field:\n%s", out)
	}
}

// TestDisabledByDefault verifies the entry points are silent no-ops when the sink
// is nil (the default, since DBWIZ_DEBUG is unset in tests).
func TestDisabledByDefault(t *testing.T) {
	mu.Lock()
	got := w
	mu.Unlock()
	if got != nil {
		t.Fatal("debug logging should be disabled by default")
	}
	if Enabled() {
		t.Fatal("Enabled() should report false by default")
	}
	// These must not panic when disabled.
	LogSQL("sql", "SELECT 1")
	LogExec("docker", []string{"ps"})
	LogConnect("sqlite", "", 0, "", "/tmp/x.db")
}

// TestLogPathUsesXDG checks the log path honours XDG_STATE_HOME and falls back to
// ~/.local/state otherwise.
func TestLogPathUsesXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	if got, _ := logPath(); got != filepath.Join("/custom/state", "dbwiz", "dbwiz.log") {
		t.Errorf("with XDG_STATE_HOME: got %q", got)
	}

	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/tester")
	want := filepath.Join("/home/tester", ".local", "state", "dbwiz", "dbwiz.log")
	if got, _ := logPath(); got != want {
		t.Errorf("fallback: got %q, want %q", got, want)
	}
}

// TestOpenLogFileCreatesDir confirms opening the log makes the state directory
// and writes appended lines.
func TestOpenLogFileCreatesDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	f, err := openLogFile()
	if err != nil {
		t.Fatalf("openLogFile: %v", err)
	}
	defer f.Close()
	if _, err := os.Stat(filepath.Join(dir, "dbwiz", "dbwiz.log")); err != nil {
		t.Errorf("log file not created: %v", err)
	}
}
