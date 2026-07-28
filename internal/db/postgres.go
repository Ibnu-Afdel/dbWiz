package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/debuglog"
)

// connectTimeout bounds the initial handshake so a wrong host/port fails fast
// instead of hanging the TUI.
const connectTimeout = 10 * time.Second

// Postgres is the Engine implementation for PostgreSQL, driven through pgx's
// database/sql-compatible stdlib adapter (driver name "pgx"). It holds exactly
// one live pool; Connect closes any previous pool before opening the new one, so
// switching targets never leaks connections.
type Postgres struct {
	pool    *sql.DB
	current string // database the pool is connected to
	target  Target
}

// NewPostgres returns an unconnected Postgres engine.
func NewPostgres() *Postgres { return &Postgres{} }

// compile-time interface checks. Postgres also satisfies Extensioner (v3 1.4);
// MySQL and SQLite deliberately do not.
var (
	_ Engine      = (*Postgres)(nil)
	_ Extensioner = (*Postgres)(nil)
)

func (p *Postgres) Kind() Kind { return KindPostgres }

// wrap classifies err, returning a true nil error when err is nil (avoiding the
// typed-nil trap of returning a nil *DBError through the error interface).
func (p *Postgres) wrap(err error) error {
	if err == nil {
		return nil
	}
	return classifyPostgres(err)
}

func (p *Postgres) Capabilities() Capabilities {
	return Capabilities{Users: true, Grants: true, MultipleDatabases: true, RoleFlags: true}
}

// Connect opens the single pool to target. When target.Database is empty it
// connects to the "postgres" maintenance database, which always exists and is
// where admin operations (create/drop database, roles) run.
func (p *Postgres) Connect(ctx context.Context, target Target) error {
	p.target = target
	db := target.Database
	if db == "" {
		db = "postgres"
	}
	debuglog.LogConnect("postgres", target.Host, target.Port, target.User, db)
	return p.connectPool(ctx, db)
}

// connectPool (re)opens the pool against dbname, closing any existing pool
// first. Because Postgres databases are isolated, browsing a different database
// means reconnecting — this is the one place that happens, keeping the
// single-live-pool invariant intact.
func (p *Postgres) connectPool(ctx context.Context, dbname string) error {
	dsn := p.dsn(dbname)
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		return classifyPostgres(err)
	}
	configurePool(pool)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		_ = pool.Close()
		return classifyPostgres(err)
	}

	if p.pool != nil {
		_ = p.pool.Close()
	}
	p.pool = pool
	p.current = dbname
	return nil
}

// dsn builds a URL-form connection string, escaping user and password. sslmode
// is disabled: DBWiz targets local containers, not TLS-fronted remotes (remote
// support is a v3 concern).
func (p *Postgres) dsn(dbname string) string {
	host := p.target.Host
	if host == "" {
		host = "127.0.0.1"
	}
	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(host, strconv.Itoa(p.target.Port)),
		Path:   "/" + dbname,
	}
	if p.target.User != "" {
		u.User = url.UserPassword(p.target.User, p.target.Password)
	}
	q := url.Values{}
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", "10")
	u.RawQuery = q.Encode()
	return u.String()
}

// Close releases the pool. It is safe to call more than once.
func (p *Postgres) Close() error {
	if p.pool == nil {
		return nil
	}
	err := p.pool.Close()
	p.pool = nil
	p.current = ""
	return err
}

// ensureDB reconnects the pool to database if it differs from the one currently
// connected. An empty database means "stay where we are".
func (p *Postgres) ensureDB(ctx context.Context, database string) error {
	if database == "" || database == p.current {
		return nil
	}
	if err := validateIdent(database); err != nil {
		return err
	}
	return p.connectPool(ctx, database)
}

func (p *Postgres) ListDatabases(ctx context.Context) ([]Database, error) {
	const q = `SELECT d.datname, pg_catalog.pg_get_userbyid(d.datdba)
	           FROM pg_catalog.pg_database d
	           WHERE d.datistemplate = false AND d.datallowconn = true
	           ORDER BY d.datname`
	rows, err := p.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, classifyPostgres(err)
	}
	defer rows.Close()
	var out []Database
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.Name, &d.Owner); err != nil {
			return nil, classifyPostgres(err)
		}
		out = append(out, d)
	}
	return out, p.wrap(rows.Err())
}

func (p *Postgres) ListTables(ctx context.Context, database string) ([]Table, error) {
	if err := p.ensureDB(ctx, database); err != nil {
		return nil, err
	}
	// User schemas only: exclude pg_catalog, information_schema and the pg_toast
	// family. reltuples is the planner's row estimate (-1 before first ANALYZE).
	const q = `SELECT n.nspname, c.relname, c.reltuples::bigint
	           FROM pg_catalog.pg_class c
	           JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	           WHERE c.relkind IN ('r','p')
	             AND n.nspname NOT IN ('pg_catalog','information_schema')
	             AND n.nspname NOT LIKE 'pg_toast%'
	           ORDER BY n.nspname, c.relname`
	rows, err := p.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, classifyPostgres(err)
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Schema, &t.Name, &t.Rows); err != nil {
			return nil, classifyPostgres(err)
		}
		if t.Rows < 0 {
			t.Rows = -1
		}
		out = append(out, t)
	}
	return out, p.wrap(rows.Err())
}

func (p *Postgres) DescribeTable(ctx context.Context, database, table string) ([]Column, error) {
	if err := p.ensureDB(ctx, database); err != nil {
		return nil, err
	}
	if err := validateIdent(table); err != nil {
		return nil, err
	}
	// information_schema for column type/nullability; a correlated lookup against
	// pg_constraint marks primary-key columns. Table name is parameterised.
	const q = `SELECT c.column_name,
	                  c.data_type,
	                  (c.is_nullable = 'YES') AS nullable,
	                  CASE WHEN pk.column_name IS NOT NULL THEN 'PRI' ELSE '' END AS key
	           FROM information_schema.columns c
	           LEFT JOIN (
	               SELECT kcu.column_name
	               FROM information_schema.table_constraints tc
	               JOIN information_schema.key_column_usage kcu
	                 ON kcu.constraint_name = tc.constraint_name
	                AND kcu.table_schema = tc.table_schema
	               WHERE tc.constraint_type = 'PRIMARY KEY'
	                 AND tc.table_name = $1
	           ) pk ON pk.column_name = c.column_name
	           WHERE c.table_name = $1
	           ORDER BY c.ordinal_position`
	rows, err := p.pool.QueryContext(ctx, q, table)
	if err != nil {
		return nil, classifyPostgres(err)
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable, &c.Key); err != nil {
			return nil, classifyPostgres(err)
		}
		out = append(out, c)
	}
	return out, p.wrap(rows.Err())
}

func (p *Postgres) PreviewRows(ctx context.Context, database, table string, limit int) (Result, error) {
	if err := p.ensureDB(ctx, database); err != nil {
		return Result{}, err
	}
	if err := validateIdent(table); err != nil {
		return Result{}, err
	}
	if limit <= 0 {
		limit = 100
	}
	// Identifier is validated+quoted (can't be parameterised); limit is a bound
	// integer literal.
	q := fmt.Sprintf("SELECT * FROM %s LIMIT %d", quotePGIdent(table), limit)
	res, err := runSQL(ctx, p.pool, q)
	if err != nil {
		return Result{}, classifyPostgres(err)
	}
	return res, nil
}

func (p *Postgres) CreateDatabase(ctx context.Context, name string, opts CreateOpts) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	stmt := "CREATE DATABASE " + quotePGIdent(name)
	if opts.Owner != "" {
		if err := validateIdent(opts.Owner); err != nil {
			return err
		}
		stmt += " OWNER " + quotePGIdent(opts.Owner)
	}
	// CREATE DATABASE cannot run inside a transaction; ExecContext runs it in
	// autocommit, which is exactly right.
	if _, err := p.pool.ExecContext(ctx, stmt); err != nil {
		return classifyPostgres(err)
	}
	return nil
}

func (p *Postgres) DropDatabase(ctx context.Context, name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	if name == p.current {
		return &DBError{
			Kind:   DBErrInvalidInput,
			Title:  "Can't drop the current database",
			Detail: fmt.Sprintf("You're connected to %q, so it can't be dropped from here.", name),
			Hint:   "switch to another database first, then drop it",
		}
	}
	// WITH (FORCE) terminates other sessions (PG13+) so an idle connection from
	// another client doesn't block the drop.
	stmt := "DROP DATABASE " + quotePGIdent(name) + " WITH (FORCE)"
	if _, err := p.pool.ExecContext(ctx, stmt); err != nil {
		return classifyPostgres(err)
	}
	return nil
}

func (p *Postgres) ListUsers(ctx context.Context) ([]User, error) {
	// Login roles only, excluding the built-in pg_* roles. rolcreatedb rides along
	// so the edit-user form (v2 3.2) can show the current CREATEDB flag; rolcanlogin
	// is true for every row here by construction.
	const q = `SELECT rolname, rolcanlogin, rolcreatedb FROM pg_catalog.pg_roles
	           WHERE rolcanlogin = true AND rolname NOT LIKE 'pg_%'
	           ORDER BY rolname`
	rows, err := p.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, classifyPostgres(err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Name, &u.CanLogin, &u.CreateDB); err != nil {
			return nil, classifyPostgres(err)
		}
		out = append(out, u)
	}
	return out, p.wrap(rows.Err())
}

func (p *Postgres) CreateUser(ctx context.Context, name, password string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	stmt := "CREATE ROLE " + quotePGIdent(name) + " LOGIN"
	if password != "" {
		// Password is a literal, not an identifier, and can't be parameterised in
		// DDL; quotePGLiteral escapes it.
		stmt += " PASSWORD " + quotePGLiteral(password)
	}
	if _, err := p.pool.ExecContext(ctx, stmt); err != nil {
		return classifyPostgres(err)
	}
	return nil
}

func (p *Postgres) DropUser(ctx context.Context, name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	if _, err := p.pool.ExecContext(ctx, "DROP ROLE "+quotePGIdent(name)); err != nil {
		return classifyPostgres(err)
	}
	return nil
}

// AlterUser sets the role's LOGIN and CREATEDB flags (v2 3.2). Both are always
// written (there is no partial state to preserve), so the statement reads e.g.
// ALTER ROLE "app" LOGIN NOCREATEDB.
func (p *Postgres) AlterUser(ctx context.Context, name string, canLogin, createDB bool) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	login, cdb := "NOLOGIN", "NOCREATEDB"
	if canLogin {
		login = "LOGIN"
	}
	if createDB {
		cdb = "CREATEDB"
	}
	stmt := fmt.Sprintf("ALTER ROLE %s %s %s", quotePGIdent(name), login, cdb)
	if _, err := p.pool.ExecContext(ctx, stmt); err != nil {
		return classifyPostgres(err)
	}
	return nil
}

// SetPassword changes an existing role's password. The password is a literal
// that can't be parameterised in DDL, so quotePGLiteral escapes it.
func (p *Postgres) SetPassword(ctx context.Context, name, password string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	stmt := "ALTER ROLE " + quotePGIdent(name) + " PASSWORD " + quotePGLiteral(password)
	if _, err := p.pool.ExecContext(ctx, stmt); err != nil {
		return classifyPostgres(err)
	}
	return nil
}

func (p *Postgres) Grant(ctx context.Context, user, database string, level GrantLevel) error {
	return p.grantRevoke(ctx, true, user, database)
}

func (p *Postgres) Revoke(ctx context.Context, user, database string, level GrantLevel) error {
	return p.grantRevoke(ctx, false, user, database)
}

// grantRevoke implements v1's single GrantAll level: GRANT/REVOKE ALL PRIVILEGES
// ON DATABASE. Note this covers database-level privileges (CONNECT, CREATE,
// TEMP); object-level defaults inside the database are a v2 concern.
func (p *Postgres) grantRevoke(ctx context.Context, grant bool, user, database string) error {
	if err := validateIdent(user); err != nil {
		return err
	}
	if err := validateIdent(database); err != nil {
		return err
	}
	var stmt string
	if grant {
		stmt = fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s",
			quotePGIdent(database), quotePGIdent(user))
	} else {
		stmt = fmt.Sprintf("REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s",
			quotePGIdent(database), quotePGIdent(user))
	}
	if _, err := p.pool.ExecContext(ctx, stmt); err != nil {
		return classifyPostgres(err)
	}
	return nil
}

func (p *Postgres) DatabasePrivileges() []Privilege { return pgDatabasePrivileges }

// ListGrants reports which database-scope privileges user effectively holds on
// database. It asks the server directly via has_database_privilege, so the
// answer includes privileges reached through PUBLIC or role membership, not just
// direct grants — the honest "who can do what" picture.
func (p *Postgres) ListGrants(ctx context.Context, user, database string) ([]Privilege, error) {
	if err := validateIdent(user); err != nil {
		return nil, err
	}
	if err := validateIdent(database); err != nil {
		return nil, err
	}
	var held []Privilege
	for _, pr := range pgDatabasePrivileges {
		var ok bool
		// user, database and the privilege name are all bound parameters here — no
		// interpolation, so this read needs no quoting.
		if err := p.pool.QueryRowContext(ctx,
			"SELECT has_database_privilege($1, $2, $3)", user, database, string(pr)).Scan(&ok); err != nil {
			return nil, classifyPostgres(err)
		}
		if ok {
			held = append(held, pr)
		}
	}
	return held, nil
}

// SetGrant grants or revokes one database-scope privilege for user on database.
func (p *Postgres) SetGrant(ctx context.Context, user, database string, priv Privilege, grant bool) error {
	if err := validateIdent(user); err != nil {
		return err
	}
	if err := validateIdent(database); err != nil {
		return err
	}
	if !knownPrivilege(pgDatabasePrivileges, priv) {
		return errInvalidPrivilege(priv)
	}
	// priv is whitelisted above (so interpolating the keyword is safe); the
	// identifiers are quoted.
	var stmt string
	if grant {
		stmt = fmt.Sprintf("GRANT %s ON DATABASE %s TO %s",
			string(priv), quotePGIdent(database), quotePGIdent(user))
	} else {
		stmt = fmt.Sprintf("REVOKE %s ON DATABASE %s FROM %s",
			string(priv), quotePGIdent(database), quotePGIdent(user))
	}
	if _, err := p.pool.ExecContext(ctx, stmt); err != nil {
		return classifyPostgres(err)
	}
	return nil
}

func (p *Postgres) Query(ctx context.Context, sql string) (Result, error) {
	res, err := runSQL(ctx, p.pool, sql)
	if err != nil {
		return Result{}, classifyPostgres(err)
	}
	return res, nil
}

// ExecMutation reconnects to database (Postgres databases are isolated, so a
// statement against another one needs its own connection — the same reason the
// browse methods call ensureDB) and runs the pre-built statement there.
func (p *Postgres) ExecMutation(ctx context.Context, database, sql string) (Result, error) {
	if err := p.ensureDB(ctx, database); err != nil {
		return Result{}, err
	}
	res, err := runSQL(ctx, p.pool, sql)
	if err != nil {
		return Result{}, classifyPostgres(err)
	}
	return res, nil
}

// ListExtensions reports every extension the running image makes available in
// database, each with its installed version if any (v3 1.4). It reads
// pg_available_extensions, so the list is exactly what this image can install —
// postgis shows up only on a PostGIS image. Extensions are per-database, so it
// reconnects to database first, like the browse methods.
func (p *Postgres) ListExtensions(ctx context.Context, database string) ([]Extension, error) {
	if err := p.ensureDB(ctx, database); err != nil {
		return nil, err
	}
	const q = `SELECT name, default_version,
	                  COALESCE(installed_version, ''),
	                  COALESCE(comment, '')
	           FROM pg_available_extensions
	           ORDER BY name`
	rows, err := p.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, classifyPostgres(err)
	}
	defer rows.Close()
	var out []Extension
	for rows.Next() {
		var e Extension
		if err := rows.Scan(&e.Name, &e.DefaultVersion, &e.InstalledVersion, &e.Comment); err != nil {
			return nil, classifyPostgres(err)
		}
		out = append(out, e)
	}
	return out, p.wrap(rows.Err())
}

// CreateExtension installs name in database with CREATE EXTENSION IF NOT EXISTS,
// so re-running is a no-op. The name is validated and quoted like any other
// identifier. If the image doesn't ship the extension the server rejects it (a
// missing control file), which classifies to a plain DBError the caller shows.
func (p *Postgres) CreateExtension(ctx context.Context, database, name string) error {
	if err := p.ensureDB(ctx, database); err != nil {
		return err
	}
	if err := validateIdent(name); err != nil {
		return err
	}
	stmt := "CREATE EXTENSION IF NOT EXISTS " + quotePGIdent(name)
	if _, err := p.pool.ExecContext(ctx, stmt); err != nil {
		return classifyPostgres(err)
	}
	return nil
}
