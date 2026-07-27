package docker

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EnvHint holds database connection details recovered from a project's .env
// file. It uses only generic, framework-neutral conventions (DB_* keys and
// DATABASE_URL) and is used both to rank/match detected containers and as a
// credential fallback. Any field may be empty.
type EnvHint struct {
	Engine   Engine // inferred from a DATABASE_URL scheme; EngineUnknown otherwise
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

// ReadEnvFile reads and parses a .env file in dir. The bool is false when no
// .env exists (the common case, silently skipped); a present-but-malformed file
// still returns true with whatever parsed cleanly.
func ReadEnvFile(dir string) (EnvHint, bool) {
	b, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		return EnvHint{}, false
	}
	return parseEnv(b), true
}

// parseEnv builds an EnvHint from raw .env bytes. DATABASE_URL provides the base
// (including engine), then explicit DB_* keys override individual fields.
// Malformed lines and an unparseable URL are skipped rather than failing.
func parseEnv(b []byte) EnvHint {
	m := parseDotenv(b)

	var h EnvHint
	if raw := m["DATABASE_URL"]; raw != "" {
		h = hintFromURL(raw)
	}
	if v := firstNonEmpty(m["DB_HOST"], m["DB_HOSTNAME"]); v != "" {
		h.Host = v
	}
	if v := m["DB_PORT"]; v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			h.Port = p
		}
	}
	if v := firstNonEmpty(m["DB_DATABASE"], m["DB_NAME"]); v != "" {
		h.Database = v
	}
	if v := firstNonEmpty(m["DB_USERNAME"], m["DB_USER"]); v != "" {
		h.User = v
	}
	if v := firstNonEmpty(m["DB_PASSWORD"], m["DB_PASS"]); v != "" {
		h.Password = v
	}
	if h.Engine == EngineUnknown {
		h.Engine = engineFromConnection(m["DB_CONNECTION"])
	}
	return h
}

// parseDotenv parses KEY=VALUE lines, tolerating `export ` prefixes, blank
// lines, `#` comments, surrounding quotes, and inline comments on unquoted
// values. Lines without '=' are skipped.
func parseDotenv(b []byte) map[string]string {
	m := map[string]string{}
	for raw := range strings.SplitSeq(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		m[key] = unquote(val)
	}
	return m
}

// unquote strips matching surrounding quotes; on an unquoted value it also drops
// a trailing inline comment.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	if i := strings.IndexByte(v, '#'); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// hintFromURL parses a DATABASE_URL like
// "postgres://user:pass@host:5432/dbname" into an EnvHint.
func hintFromURL(raw string) EnvHint {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return EnvHint{}
	}
	h := EnvHint{
		Engine:   engineFromScheme(u.Scheme),
		Host:     u.Hostname(),
		Database: strings.TrimPrefix(u.Path, "/"),
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			h.Port = n
		}
	}
	if u.User != nil {
		h.User = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			h.Password = pw
		}
	}
	return h
}

func engineFromScheme(scheme string) Engine {
	switch strings.ToLower(scheme) {
	case "postgres", "postgresql", "pgsql":
		return EnginePostgres
	case "mysql":
		return EngineMySQL
	case "mariadb":
		return EngineMariaDB
	}
	return EngineUnknown
}

// engineFromConnection maps a generic DB_CONNECTION value (a common driver-name
// convention) to an engine.
func engineFromConnection(conn string) Engine {
	switch strings.ToLower(conn) {
	case "pgsql", "postgres", "postgresql":
		return EnginePostgres
	case "mysql":
		return EngineMySQL
	case "mariadb":
		return EngineMariaDB
	}
	return EngineUnknown
}
