package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeFiles are the conventional Compose filenames, in the precedence Docker
// Compose itself uses. ReadComposeFile tries them in order.
var composeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// ReadComposeFile looks for a Docker Compose file in dir and extracts a database
// connection hint from the first database service it defines (v2 3.4). It is a
// generic, framework-neutral hint source that extends the .env ladder: it reads
// only the official image env-var conventions (POSTGRES_*/MYSQL_*/MARIADB_*), so
// it works for any project that runs its database with Compose. The bool is
// false when no Compose file is present (the common case) or none of its
// services look like a database.
func ReadComposeFile(dir string) (EnvHint, bool) {
	for _, name := range composeFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		return parseCompose(b)
	}
	return EnvHint{}, false
}

// parseCompose extracts a hint from raw Compose bytes. It navigates the document
// loosely (map[string]any) rather than binding a strict schema, so the many
// shapes a Compose file can take — environment as a map or a list, ports as
// short or long syntax — are all tolerated best-effort. Services are considered
// in sorted name order so the choice is deterministic when several databases are
// defined.
func parseCompose(b []byte) (EnvHint, bool) {
	var root map[string]any
	if err := yaml.Unmarshal(b, &root); err != nil {
		return EnvHint{}, false
	}
	services, ok := root["services"].(map[string]any)
	if !ok {
		return EnvHint{}, false
	}
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc, ok := services[name].(map[string]any)
		if !ok {
			continue
		}
		image, _ := svc["image"].(string)
		env := composeEnv(svc["environment"])

		engine := engineFromImage(image)
		if engine == EngineUnknown {
			engine = engineFromComposeEnv(env)
		}
		if engine == EngineUnknown {
			continue // not a database service — keep looking
		}

		h := hintFromComposeEnv(engine, env)
		h.Engine = engine
		h.Port = firstHostPort(svc["ports"])
		return h, true
	}
	return EnvHint{}, false
}

// composeEnv normalises a service's environment — a mapping (KEY: value) or a
// list (- KEY=value) — into a plain map. Values are stringified so a numeric
// port or a quoted secret both come through.
func composeEnv(v any) map[string]string {
	m := map[string]string{}
	switch env := v.(type) {
	case map[string]any:
		for k, val := range env {
			if val == nil {
				continue // `KEY:` with no value passes the host var through — nothing to read
			}
			m[k] = fmt.Sprint(val)
		}
	case []any:
		for _, item := range env {
			if s, ok := item.(string); ok {
				if k, val, found := strings.Cut(s, "="); found {
					m[strings.TrimSpace(k)] = strings.TrimSpace(val)
				}
			}
		}
	}
	return m
}

// engineFromImage infers the engine from a service's image reference, matching
// the official image names (and postgis, a common Postgres derivative).
func engineFromImage(image string) Engine {
	img := strings.ToLower(image)
	switch {
	case strings.Contains(img, "postgres"), strings.Contains(img, "postgis"):
		return EnginePostgres
	case strings.Contains(img, "mariadb"):
		return EngineMariaDB
	case strings.Contains(img, "mysql"):
		return EngineMySQL
	}
	return EngineUnknown
}

// engineFromComposeEnv infers the engine from which official env-var family a
// service defines, so a database built from a bespoke image is still recognised.
func engineFromComposeEnv(env map[string]string) Engine {
	switch {
	case hasPrefixKey(env, "POSTGRES_"):
		return EnginePostgres
	case hasPrefixKey(env, "MARIADB_"):
		return EngineMariaDB
	case hasPrefixKey(env, "MYSQL_"):
		return EngineMySQL
	}
	return EngineUnknown
}

// hintFromComposeEnv reads the standard credential env vars for the engine. For
// MySQL/MariaDB, an explicit user wins; failing that, a root password maps to
// the root account (the image's own default admin).
func hintFromComposeEnv(engine Engine, env map[string]string) EnvHint {
	var h EnvHint
	switch engine {
	case EnginePostgres:
		h.User = env["POSTGRES_USER"]
		h.Password = env["POSTGRES_PASSWORD"]
		h.Database = env["POSTGRES_DB"]
	default: // MySQL / MariaDB share the same var shapes; accept either prefix.
		user := firstNonEmpty(env["MYSQL_USER"], env["MARIADB_USER"])
		pass := firstNonEmpty(env["MYSQL_PASSWORD"], env["MARIADB_PASSWORD"])
		if user != "" {
			h.User, h.Password = user, pass
		} else if root := firstNonEmpty(env["MYSQL_ROOT_PASSWORD"], env["MARIADB_ROOT_PASSWORD"]); root != "" {
			h.User, h.Password = "root", root
		}
		h.Database = firstNonEmpty(env["MYSQL_DATABASE"], env["MARIADB_DATABASE"])
	}
	return h
}

// firstHostPort returns the published host port from a service's ports entry,
// tolerating both the short string syntax ("5432:5432", "127.0.0.1:5432:5432",
// "5432") and the long map syntax (published: 5432). Zero when none parses.
func firstHostPort(v any) int {
	list, ok := v.([]any)
	if !ok {
		return 0
	}
	for _, item := range list {
		switch p := item.(type) {
		case string:
			if port := hostPortFromString(p); port != 0 {
				return port
			}
		case map[string]any:
			if pub, ok := p["published"]; ok {
				if port, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(pub))); err == nil {
					return port
				}
			}
		}
	}
	return 0
}

// hostPortFromString pulls the host port out of a short-syntax mapping. In
// "[HOST:]HOST_PORT:CONTAINER_PORT" the host port is the field before the
// container port; a lone "PORT" is taken as-is. A "/tcp" suffix is stripped.
func hostPortFromString(s string) int {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ":")
	var field string
	switch len(parts) {
	case 1:
		field = parts[0] // container-only publish ("5432")
	default:
		field = parts[len(parts)-2] // the host port sits before the container port
	}
	if port, err := strconv.Atoi(strings.TrimSpace(field)); err == nil {
		return port
	}
	return 0
}

// hasPrefixKey reports whether any key in env starts with prefix.
func hasPrefixKey(env map[string]string, prefix string) bool {
	for k := range env {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}
