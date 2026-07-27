package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"

	"github.com/go-sql-driver/mysql"

	"github.com/Ibnu-Afdel/dbwiz/internal/debuglog"
)

// MySQL is the Engine implementation for both MySQL and MariaDB — one wire
// protocol, one driver. The only difference is the reported Kind (and therefore
// the UI label), set at construction from the detected image. Unlike Postgres,
// a single MySQL connection can see every schema, so the database argument to
// the read methods scopes a WHERE clause instead of forcing a reconnect.
type MySQL struct {
	pool   *sql.DB
	kind   Kind
	target Target
}

// NewMySQL returns an unconnected engine reporting KindMySQL.
func NewMySQL() *MySQL { return &MySQL{kind: KindMySQL} }

// NewMariaDB returns an unconnected engine reporting KindMariaDB. It shares
// every code path with NewMySQL; only the label differs.
func NewMariaDB() *MySQL { return &MySQL{kind: KindMariaDB} }

var _ Engine = (*MySQL)(nil)

func (m *MySQL) Kind() Kind { return m.kind }

// wrap classifies err, returning a true nil error when err is nil.
func (m *MySQL) wrap(err error) error {
	if err == nil {
		return nil
	}
	return classifyMySQL(err)
}

func (m *MySQL) Capabilities() Capabilities {
	return Capabilities{Users: true, Grants: true, MultipleDatabases: true}
}

func (m *MySQL) Connect(ctx context.Context, target Target) error {
	m.target = target

	host := target.Host
	if host == "" {
		host = "127.0.0.1"
	}
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, strconv.Itoa(target.Port))
	cfg.User = target.User
	cfg.Passwd = target.Password
	cfg.DBName = target.Database
	debuglog.LogConnect(m.Kind().String(), host, target.Port, target.User, target.Database)
	cfg.ParseTime = true
	cfg.Timeout = connectTimeout
	cfg.AllowNativePasswords = true
	// MySQL 8.4 defaults accounts to caching_sha2_password. The driver completes
	// that handshake over a non-TLS loopback connection by fetching the server's
	// RSA public key automatically — DBWiz targets local containers, where that
	// is fine (remote/TLS targets are a v3 concern).

	pool, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return classifyMySQL(err)
	}
	configurePool(pool)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		_ = pool.Close()
		return classifyMySQL(err)
	}

	if m.pool != nil {
		_ = m.pool.Close()
	}
	m.pool = pool
	return nil
}

func (m *MySQL) Close() error {
	if m.pool == nil {
		return nil
	}
	err := m.pool.Close()
	m.pool = nil
	return err
}

// systemSchemas are MySQL/MariaDB's built-in schemas, hidden from the browser.
var systemSchemas = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

func (m *MySQL) ListDatabases(ctx context.Context) ([]Database, error) {
	rows, err := m.pool.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, classifyMySQL(err)
	}
	defer rows.Close()
	var out []Database
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, classifyMySQL(err)
		}
		if systemSchemas[name] {
			continue
		}
		out = append(out, Database{Name: name}) // MySQL databases have no owner
	}
	return out, m.wrap(rows.Err())
}

// schemaOf resolves the schema to inspect: the explicit database if given, else
// the connection's default (may be empty if the target had no database).
func (m *MySQL) schemaOf(database string) string {
	if database != "" {
		return database
	}
	return m.target.Database
}

func (m *MySQL) ListTables(ctx context.Context, database string) ([]Table, error) {
	schema := m.schemaOf(database)
	const q = `SELECT table_name, IFNULL(table_rows, 0)
	           FROM information_schema.tables
	           WHERE table_schema = ? AND table_type = 'BASE TABLE'
	           ORDER BY table_name`
	rows, err := m.pool.QueryContext(ctx, q, schema)
	if err != nil {
		return nil, classifyMySQL(err)
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		t := Table{Schema: schema}
		if err := rows.Scan(&t.Name, &t.Rows); err != nil {
			return nil, classifyMySQL(err)
		}
		out = append(out, t)
	}
	return out, m.wrap(rows.Err())
}

func (m *MySQL) DescribeTable(ctx context.Context, database, table string) ([]Column, error) {
	schema := m.schemaOf(database)
	const q = `SELECT column_name, column_type,
	                  (is_nullable = 'YES') AS nullable,
	                  column_key
	           FROM information_schema.columns
	           WHERE table_schema = ? AND table_name = ?
	           ORDER BY ordinal_position`
	rows, err := m.pool.QueryContext(ctx, q, schema, table)
	if err != nil {
		return nil, classifyMySQL(err)
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable, &c.Key); err != nil {
			return nil, classifyMySQL(err)
		}
		out = append(out, c)
	}
	return out, m.wrap(rows.Err())
}

func (m *MySQL) PreviewRows(ctx context.Context, database, table string, limit int) (Result, error) {
	if err := validateIdent(table); err != nil {
		return Result{}, err
	}
	if limit <= 0 {
		limit = 100
	}
	schema := m.schemaOf(database)
	qualified := quoteMySQLIdent(table)
	if schema != "" {
		if err := validateIdent(schema); err != nil {
			return Result{}, err
		}
		qualified = quoteMySQLIdent(schema) + "." + qualified
	}
	q := fmt.Sprintf("SELECT * FROM %s LIMIT %d", qualified, limit)
	res, err := runSQL(ctx, m.pool, q)
	if err != nil {
		return Result{}, classifyMySQL(err)
	}
	return res, nil
}

func (m *MySQL) CreateDatabase(ctx context.Context, name string, opts CreateOpts) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	// MySQL databases have no owner concept; opts.Owner is intentionally ignored
	// here (grant that user access separately with Grant).
	if _, err := m.pool.ExecContext(ctx, "CREATE DATABASE "+quoteMySQLIdent(name)); err != nil {
		return classifyMySQL(err)
	}
	return nil
}

func (m *MySQL) DropDatabase(ctx context.Context, name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	if _, err := m.pool.ExecContext(ctx, "DROP DATABASE "+quoteMySQLIdent(name)); err != nil {
		return classifyMySQL(err)
	}
	return nil
}

func (m *MySQL) ListUsers(ctx context.Context) ([]User, error) {
	// Distinct account names, hiding the reserved mysql.* internal accounts.
	const q = `SELECT DISTINCT user FROM mysql.user
	           WHERE user NOT IN ('mysql.sys','mysql.session','mysql.infoschema')
	             AND user <> ''
	           ORDER BY user`
	rows, err := m.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, classifyMySQL(err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Name); err != nil {
			return nil, classifyMySQL(err)
		}
		out = append(out, u)
	}
	return out, m.wrap(rows.Err())
}

func (m *MySQL) CreateUser(ctx context.Context, name, password string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	// Account is name@'%' so it can connect from any host (the common container
	// case). Password is a literal.
	stmt := fmt.Sprintf("CREATE USER %s@'%%' IDENTIFIED BY %s",
		quoteMySQLIdent(name), quoteMySQLLiteral(password))
	if _, err := m.pool.ExecContext(ctx, stmt); err != nil {
		return classifyMySQL(err)
	}
	return nil
}

func (m *MySQL) DropUser(ctx context.Context, name string) error {
	if err := validateIdent(name); err != nil {
		return err
	}
	if _, err := m.pool.ExecContext(ctx, fmt.Sprintf("DROP USER %s@'%%'", quoteMySQLIdent(name))); err != nil {
		return classifyMySQL(err)
	}
	return nil
}

func (m *MySQL) Grant(ctx context.Context, user, database string, level GrantLevel) error {
	return m.grantRevoke(ctx, true, user, database)
}

func (m *MySQL) Revoke(ctx context.Context, user, database string, level GrantLevel) error {
	return m.grantRevoke(ctx, false, user, database)
}

func (m *MySQL) grantRevoke(ctx context.Context, grant bool, user, database string) error {
	if err := validateIdent(user); err != nil {
		return err
	}
	if err := validateIdent(database); err != nil {
		return err
	}
	// GRANT ALL PRIVILEGES ON `db`.* — the db-wide level; column/table grants are
	// v2. The database identifier is quoted; ".*" is appended outside the quotes.
	obj := quoteMySQLIdent(database) + ".*"
	var stmt string
	if grant {
		stmt = fmt.Sprintf("GRANT ALL PRIVILEGES ON %s TO %s@'%%'", obj, quoteMySQLIdent(user))
	} else {
		stmt = fmt.Sprintf("REVOKE ALL PRIVILEGES ON %s FROM %s@'%%'", obj, quoteMySQLIdent(user))
	}
	if _, err := m.pool.ExecContext(ctx, stmt); err != nil {
		return classifyMySQL(err)
	}
	return nil
}

func (m *MySQL) Query(ctx context.Context, sql string) (Result, error) {
	res, err := runSQL(ctx, m.pool, sql)
	if err != nil {
		return Result{}, classifyMySQL(err)
	}
	return res, nil
}
