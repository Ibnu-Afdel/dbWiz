package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCompose(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		want   EnvHint
		wantOK bool
	}{
		{
			name: "postgres, map environment + short ports",
			body: `
services:
  db:
    image: postgres:16
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: appdb
    ports:
      - "5432:5432"
`,
			want:   EnvHint{Engine: EnginePostgres, User: "app", Password: "secret", Database: "appdb", Port: 5432},
			wantOK: true,
		},
		{
			name: "mysql, list environment + host-bound port",
			body: `
services:
  mysql:
    image: mysql:8
    environment:
      - MYSQL_USER=me
      - MYSQL_PASSWORD=pw
      - MYSQL_DATABASE=shop
    ports:
      - "127.0.0.1:3307:3306"
`,
			want:   EnvHint{Engine: EngineMySQL, User: "me", Password: "pw", Database: "shop", Port: 3307},
			wantOK: true,
		},
		{
			name: "mysql root-password fallback when no user",
			body: `
services:
  db:
    image: mysql:8
    environment:
      MYSQL_ROOT_PASSWORD: rootpw
      MYSQL_DATABASE: shop
`,
			want:   EnvHint{Engine: EngineMySQL, User: "root", Password: "rootpw", Database: "shop"},
			wantOK: true,
		},
		{
			name: "mariadb via image, long-syntax ports",
			body: `
services:
  maria:
    image: mariadb:11
    environment:
      MARIADB_USER: m
      MARIADB_PASSWORD: mp
      MARIADB_DATABASE: md
    ports:
      - target: 3306
        published: 3316
        protocol: tcp
`,
			want:   EnvHint{Engine: EngineMariaDB, User: "m", Password: "mp", Database: "md", Port: 3316},
			wantOK: true,
		},
		{
			name: "engine inferred from env when image is bespoke",
			body: `
services:
  db:
    image: my-registry/custom-pg:latest
    environment:
      POSTGRES_PASSWORD: only
`,
			want:   EnvHint{Engine: EnginePostgres, Password: "only"},
			wantOK: true,
		},
		{
			name: "database service chosen deterministically over an app service",
			body: `
services:
  web:
    image: nginx
    ports:
      - "8080:80"
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: app
    ports:
      - "5432:5432"
`,
			want:   EnvHint{Engine: EnginePostgres, User: "app", Port: 5432},
			wantOK: true,
		},
		{
			name:   "no database service → no hint",
			body:   "services:\n  web:\n    image: nginx\n",
			wantOK: false,
		},
		{
			name:   "malformed yaml → no hint",
			body:   "services: : :\n  - broken",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseCompose([]byte(tc.body))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (hint=%+v)", ok, tc.wantOK, got)
			}
			if ok && got != tc.want {
				t.Errorf("hint = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestHostPortFromString(t *testing.T) {
	cases := map[string]int{
		"5432:5432":           5432,
		"127.0.0.1:5433:5432": 5433,
		"5432":                5432,
		"5432:5432/tcp":       5432,
		"":                    0,
		"nope:nope":           0,
	}
	for in, want := range cases {
		if got := hostPortFromString(in); got != want {
			t.Errorf("hostPortFromString(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestEngineFromImage(t *testing.T) {
	cases := map[string]Engine{
		"postgres:16":     EnginePostgres,
		"postgis/postgis": EnginePostgres,
		"mariadb:11":      EngineMariaDB,
		"mysql:8":         EngineMySQL,
		"nginx:latest":    EngineUnknown,
	}
	for img, want := range cases {
		if got := engineFromImage(img); got != want {
			t.Errorf("engineFromImage(%q) = %v, want %v", img, got, want)
		}
	}
}

// TestReadComposeFile covers file discovery and precedence: compose.yaml wins
// over docker-compose.yml when both exist, and a dir with none returns false.
func TestReadComposeFile(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadComposeFile(dir); ok {
		t.Fatal("empty dir should yield no compose hint")
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("docker-compose.yml", "services:\n  db:\n    image: mysql:8\n    environment:\n      MYSQL_ROOT_PASSWORD: legacy\n")
	write("compose.yaml", "services:\n  db:\n    image: postgres:16\n    environment:\n      POSTGRES_USER: won\n")

	h, ok := ReadComposeFile(dir)
	if !ok {
		t.Fatal("a compose file should be found")
	}
	if h.Engine != EnginePostgres || h.User != "won" {
		t.Errorf("compose.yaml should win over docker-compose.yml; got %+v", h)
	}
}
