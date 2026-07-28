// Package dburl renders a resolved db.Target into the connection strings a
// developer pastes into a new project (v3 1.2): a single DATABASE_URL, a generic
// DB_* environment block, or a JDBC URL. It's the "wire this into my .env"
// convenience — the output of `dbwiz url` and of the TUI's [y] yank.
//
// The DB_* keys are deliberately generic (DB_HOST/DB_DATABASE/DB_USERNAME/…),
// not framework-branded (D8): they work as plain conventions in any project. A
// recovered password is included verbatim — this text is meant to be pasted into
// a local config, so callers should treat it as they would any credential.
//
// It imports db only; it never touches docker, tui, or the network.
package dburl

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// Format selects one of the rendered shapes.
type Format string

const (
	FormatURL  Format = "url"  // DATABASE_URL=<url>
	FormatEnv  Format = "env"  // a DB_* block
	FormatJDBC Format = "jdbc" // a jdbc: URL
)

// Formats lists the supported format names, in a sensible display order.
func Formats() []Format { return []Format{FormatURL, FormatEnv, FormatJDBC} }

// ParseFormat validates a --format value, returning a clear error naming the
// alternatives on a typo.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatURL:
		return FormatURL, nil
	case FormatEnv:
		return FormatEnv, nil
	case FormatJDBC:
		return FormatJDBC, nil
	}
	return "", fmt.Errorf("unknown format %q; use one of: url, env, jdbc", s)
}

// URL returns the bare connection URL for a target — postgresql://user:pass@host:port/db,
// mysql://…, or sqlite://<path>. This is the value the TUI yanks and the string
// the url format wraps in DATABASE_URL=. A password is omitted from the userinfo
// when the target has none, so no dangling "user:@" appears.
func URL(kind db.Kind, t db.Target) string {
	if kind == db.KindSQLite {
		return "sqlite://" + t.Path
	}
	u := url.URL{
		Scheme: urlScheme(kind),
		Host:   host(t),
		Path:   "/" + t.Database,
	}
	if t.User != "" {
		if t.Password != "" {
			u.User = url.UserPassword(t.User, t.Password)
		} else {
			u.User = url.User(t.User)
		}
	}
	return u.String()
}

// Render produces the labeled block for one format. SQLite, which has no
// host/port/user, renders a path-only shape in every format.
func Render(kind db.Kind, t db.Target, f Format) (string, error) {
	switch f {
	case FormatURL:
		return "DATABASE_URL=" + URL(kind, t), nil
	case FormatEnv:
		return envBlock(kind, t), nil
	case FormatJDBC:
		return jdbc(kind, t), nil
	}
	return "", fmt.Errorf("unknown format %q", f)
}

// envBlock renders the generic DB_* environment block.
func envBlock(kind db.Kind, t db.Target) string {
	var b strings.Builder
	fmt.Fprintf(&b, "DB_CONNECTION=%s\n", connName(kind))
	if kind == db.KindSQLite {
		fmt.Fprintf(&b, "DB_DATABASE=%s", t.Path)
		return b.String()
	}
	fmt.Fprintf(&b, "DB_HOST=%s\n", hostOnly(t))
	fmt.Fprintf(&b, "DB_PORT=%d\n", t.Port)
	fmt.Fprintf(&b, "DB_DATABASE=%s\n", t.Database)
	fmt.Fprintf(&b, "DB_USERNAME=%s\n", t.User)
	fmt.Fprintf(&b, "DB_PASSWORD=%s", t.Password)
	return b.String()
}

// jdbc renders a JDBC URL. Postgres/MySQL/MariaDB carry user+password as query
// params (JDBC's convention); SQLite is a bare file URL with no auth.
func jdbc(kind db.Kind, t db.Target) string {
	if kind == db.KindSQLite {
		return "jdbc:sqlite:" + t.Path
	}
	base := fmt.Sprintf("jdbc:%s://%s/%s", jdbcSubprotocol(kind), host(t), t.Database)
	q := url.Values{}
	if t.User != "" {
		q.Set("user", t.User)
	}
	if t.Password != "" {
		q.Set("password", t.Password)
	}
	if len(q) == 0 {
		return base
	}
	return base + "?" + q.Encode()
}

// urlScheme is the URL scheme for a server engine. MariaDB speaks the MySQL
// protocol, so mysql:// addresses it too.
func urlScheme(kind db.Kind) string {
	switch kind {
	case db.KindPostgres:
		return "postgresql"
	default:
		return "mysql"
	}
}

// jdbcSubprotocol is the JDBC subprotocol; MariaDB has its own driver name.
func jdbcSubprotocol(kind db.Kind) string {
	switch kind {
	case db.KindPostgres:
		return "postgresql"
	case db.KindMariaDB:
		return "mariadb"
	default:
		return "mysql"
	}
}

// connName is the generic DB_CONNECTION value (the engine, lowercased).
func connName(kind db.Kind) string {
	switch kind {
	case db.KindPostgres:
		return "postgres"
	case db.KindMySQL:
		return "mysql"
	case db.KindMariaDB:
		return "mariadb"
	case db.KindSQLite:
		return "sqlite"
	}
	return "unknown"
}

// host returns host:port, defaulting the host to 127.0.0.1 (DBWiz's containers
// bind loopback).
func host(t db.Target) string {
	return hostOnly(t) + ":" + strconv.Itoa(t.Port)
}

func hostOnly(t db.Target) string {
	if t.Host == "" {
		return "127.0.0.1"
	}
	return t.Host
}
