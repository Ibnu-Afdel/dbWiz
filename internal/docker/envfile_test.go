package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnv(t *testing.T) {
	tests := []struct {
		name string
		body string
		want EnvHint
	}{
		{
			"generic DB_ keys",
			"DB_HOST=127.0.0.1\nDB_PORT=5432\nDB_DATABASE=app\nDB_USERNAME=admin\nDB_PASSWORD=secret\n",
			EnvHint{Host: "127.0.0.1", Port: 5432, Database: "app", User: "admin", Password: "secret"},
		},
		{
			"DATABASE_URL parsed incl engine",
			"DATABASE_URL=postgres://bob:pw@db.local:5555/shop\n",
			EnvHint{Engine: EnginePostgres, Host: "db.local", Port: 5555, Database: "shop", User: "bob", Password: "pw"},
		},
		{
			"DB_ keys override DATABASE_URL fields",
			"DATABASE_URL=mysql://root:x@localhost:3306/base\nDB_PASSWORD=override\n",
			EnvHint{Engine: EngineMySQL, Host: "localhost", Port: 3306, Database: "base", User: "root", Password: "override"},
		},
		{
			"export prefix, quotes, comments, blank lines",
			"# comment\n\nexport DB_HOST=\"quoted-host\"\nDB_PASSWORD='p@ss'\nDB_DATABASE=app # inline\n",
			EnvHint{Host: "quoted-host", Database: "app", Password: "p@ss"},
		},
		{
			"engine from DB_CONNECTION when no URL",
			"DB_CONNECTION=pgsql\nDB_HOST=h\n",
			EnvHint{Engine: EnginePostgres, Host: "h"},
		},
		{
			"malformed lines skipped, degrades gracefully",
			"garbage without equals\nDB_HOST=ok\n=noKey\nDB_PORT=notanumber\n",
			EnvHint{Host: "ok"}, // bad port ignored, keeps host
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseEnv([]byte(tt.body)); got != tt.want {
				t.Errorf("parseEnv =\n %+v\n want %+v", got, tt.want)
			}
		})
	}
}

func TestReadEnvFile(t *testing.T) {
	dir := t.TempDir()

	// Absent .env is silently skipped.
	if _, ok := ReadEnvFile(dir); ok {
		t.Error("missing .env should report ok=false")
	}

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_HOST=here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, ok := ReadEnvFile(dir)
	if !ok || h.Host != "here" {
		t.Errorf("present .env: got %+v ok=%v", h, ok)
	}
}
