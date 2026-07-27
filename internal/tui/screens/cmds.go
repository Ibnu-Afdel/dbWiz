package screens

import (
	"context"
	"errors"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// connectTimeout bounds a single connection attempt so a wedged server surfaces
// as a Timeout error instead of hanging the UI. Detection and start have their
// own deadlines inside the docker package.
const connectTimeout = 10 * time.Second

// DetectFn and NewEngineFn are the two boundaries the TUI crosses into the
// outside world: a Docker scan and opening a database engine. They are exported
// vars (defaulting to the real implementations) purely so integration tests can
// substitute deterministic fakes and drive the whole app under teatest without a
// live Docker daemon or database. Production never reassigns them.
var (
	DetectFn    = docker.Detect
	NewEngineFn = db.New
)

// Result messages the async commands below deliver back into Update. Screens
// switch on these; the raw docker/db calls never touch the model directly.
type (
	// detectDoneMsg carries the containers a scan found. An empty slice is a
	// success (docker ran, nothing matched) — the detect screen turns that into
	// the NoContainers empty state.
	detectDoneMsg struct{ containers []docker.Container }
	// detectErrMsg is a typed docker failure from the scan (daemon down, etc.).
	detectErrMsg struct{ err *docker.DockerError }

	// startedMsg reports a stopped container was started successfully; the home
	// screen re-scans after it.
	startedMsg struct{ name string }
	// startErrMsg is a typed docker failure from starting a container.
	startErrMsg struct{ err *docker.DockerError }

	// connectedMsg carries a live engine plus the target it connected to; the
	// connect screen pushes the dashboard with it.
	connectedMsg struct {
		engine    db.Engine
		target    db.Target
		container docker.Container
	}
	// connectAuthMsg means every non-interactive rung of the credential ladder
	// was exhausted and the server rejected the login — the connect screen drops
	// to the masked password prompt.
	connectAuthMsg struct{ target db.Target }
	// connectErrMsg is any other typed failure while connecting.
	connectErrMsg struct{ err error }

	// Browser (dashboard) load results. Each carries the key it was requested
	// for (database/table) so a late reply for a since-abandoned selection can be
	// dropped instead of clobbering the current view.
	databasesLoadedMsg struct{ databases []db.Database }
	databasesErrMsg    struct{ err *db.DBError }

	tablesLoadedMsg struct {
		database string
		tables   []db.Table
	}
	tablesErrMsg struct {
		database string
		err      *db.DBError
	}

	rowsLoadedMsg struct {
		database string
		table    string
		result   db.Result
	}
	rowsErrMsg struct {
		database string
		table    string
		err      *db.DBError
	}

	describeLoadedMsg struct {
		database string
		table    string
		columns  []db.Column
	}
	describeErrMsg struct {
		database string
		table    string
		err      *db.DBError
	}

	// Admin (Phase 6) loads and mutations. usersLoadedMsg feeds the navigator's
	// users section; adminDoneMsg reports a completed mutation with a toast and
	// which lists to reload; adminErrMsg is a typed failure shown inline.
	usersLoadedMsg struct{ users []db.User }
	usersErrMsg    struct{ err *db.DBError }

	adminDoneMsg struct {
		notice          string
		reloadDatabases bool
		reloadUsers     bool
	}
	adminErrMsg struct{ err *db.DBError }

	// Query (Phase 7) results. Each carries the seq it was launched with so a
	// late reply for a superseded (re-run or cancelled) statement is dropped
	// instead of clobbering the current one. verb is the statement's leading
	// keyword, kept so an exec result can read "UPDATE — 3 rows".
	queryDoneMsg struct {
		seq    int
		verb   string
		result db.Result
	}
	queryErrMsg struct {
		seq int
		err *db.DBError
	}
)

// runQueryCmd executes an arbitrary statement off the Update goroutine. The
// context is supplied by the caller (not bounded here) so a running query stays
// cancellable from the UI — cancelling ctx propagates to the driver, which kills
// the statement server-side. A typed failure comes back as queryErrMsg for the
// dashboard to route (inline for user errors, full-screen for system ones).
func runQueryCmd(ctx context.Context, engine db.Engine, sql string, seq int, verb string) tea.Cmd {
	return func() tea.Msg {
		res, err := engine.Query(ctx, sql)
		if err != nil {
			return queryErrMsg{seq: seq, err: asDBError(err)}
		}
		return queryDoneMsg{seq: seq, verb: verb, result: res}
	}
}

// loadUsersCmd lists the engine's (non-system) login users for the navigator.
func loadUsersCmd(engine db.Engine) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		users, err := engine.ListUsers(ctx)
		if err != nil {
			return usersErrMsg{err: asDBError(err)}
		}
		return usersLoadedMsg{users: users}
	}
}

// createDatabaseCmd creates a database and, when withUser is set, also creates a
// matching login user and grants it ALL on the new database — the one-shot
// create-and-continue flow. It stops at the first failure so a partial result is
// reported clearly rather than compounding.
func createDatabaseCmd(engine db.Engine, name string, withUser bool, password string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		// The user must exist before the database can be owned by it, so create
		// the role first — otherwise CREATE DATABASE … OWNER <name> fails with
		// "role <name> does not exist".
		if withUser {
			if err := engine.CreateUser(ctx, name, password); err != nil {
				return adminErrMsg{err: asDBError(err)}
			}
		}
		if err := engine.CreateDatabase(ctx, name, db.CreateOpts{Owner: ownerIf(withUser, name)}); err != nil {
			return adminErrMsg{err: asDBError(err)}
		}
		if !withUser {
			return adminDoneMsg{notice: "Created database " + name, reloadDatabases: true}
		}
		if err := engine.Grant(ctx, name, name, db.GrantAll); err != nil {
			return adminErrMsg{err: asDBError(err)}
		}
		return adminDoneMsg{
			notice:          "Created database " + name + " + user " + name + " (granted ALL)",
			reloadDatabases: true,
			reloadUsers:     true,
		}
	}
}

// ownerIf returns name when withUser is set, so CreateDatabase can hand ownership
// to the about-to-be-created user; otherwise the engine default owner is used.
func ownerIf(withUser bool, name string) string {
	if withUser {
		return name
	}
	return ""
}

// dropDatabaseCmd drops a database. The engine refuses the current database and
// classifies an in-use failure, so the caller just renders the typed error.
func dropDatabaseCmd(engine db.Engine, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		if err := engine.DropDatabase(ctx, name); err != nil {
			return adminErrMsg{err: asDBError(err)}
		}
		return adminDoneMsg{notice: "Dropped database " + name, reloadDatabases: true}
	}
}

// createUserCmd creates a login user with an optional password.
func createUserCmd(engine db.Engine, name, password string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		if err := engine.CreateUser(ctx, name, password); err != nil {
			return adminErrMsg{err: asDBError(err)}
		}
		return adminDoneMsg{notice: "Created user " + name, reloadUsers: true}
	}
}

// dropUserCmd drops a user. A dependent-objects failure (Postgres) comes back
// typed, so the caller renders plain-language text rather than a raw code.
func dropUserCmd(engine db.Engine, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		if err := engine.DropUser(ctx, name); err != nil {
			return adminErrMsg{err: asDBError(err)}
		}
		return adminDoneMsg{notice: "Dropped user " + name, reloadUsers: true}
	}
}

// grantCmd grants or revokes ALL for a user on a database (v1's coarse level).
func grantCmd(engine db.Engine, user, database string, grant bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		var err error
		verb := "Granted"
		if grant {
			err = engine.Grant(ctx, user, database, db.GrantAll)
		} else {
			err = engine.Revoke(ctx, user, database, db.GrantAll)
			verb = "Revoked"
		}
		if err != nil {
			return adminErrMsg{err: asDBError(err)}
		}
		return adminDoneMsg{notice: verb + " " + user + " on " + database}
	}
}

// browseTimeout bounds a single browser read (list/preview/describe) so a wedged
// or killed server surfaces as a pane-level Timeout error instead of freezing
// the UI mid-browse.
const browseTimeout = 15 * time.Second

// previewLimit caps how many rows a table preview pulls, keeping the preview
// snappy on large tables.
const previewLimit = 200

// loadDatabasesCmd lists the engine's databases off the Update goroutine.
func loadDatabasesCmd(engine db.Engine) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		dbs, err := engine.ListDatabases(ctx)
		if err != nil {
			return databasesErrMsg{err: asDBError(err)}
		}
		return databasesLoadedMsg{databases: dbs}
	}
}

// loadTablesCmd lists the tables in database. For engines that host a single
// database (SQLite) database is empty and ignored by the driver.
func loadTablesCmd(engine db.Engine, database string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		tables, err := engine.ListTables(ctx, database)
		if err != nil {
			return tablesErrMsg{database: database, err: asDBError(err)}
		}
		return tablesLoadedMsg{database: database, tables: tables}
	}
}

// previewRowsCmd fetches up to previewLimit rows of a table for the results pane.
func previewRowsCmd(engine db.Engine, database, table string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		res, err := engine.PreviewRows(ctx, database, table, previewLimit)
		if err != nil {
			return rowsErrMsg{database: database, table: table, err: asDBError(err)}
		}
		return rowsLoadedMsg{database: database, table: table, result: res}
	}
}

// describeTableCmd fetches a table's columns (name, type, nullability, key) for
// the results pane's describe view.
func describeTableCmd(engine db.Engine, database, table string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		cols, err := engine.DescribeTable(ctx, database, table)
		if err != nil {
			return describeErrMsg{database: database, table: table, err: asDBError(err)}
		}
		return describeLoadedMsg{database: database, table: table, columns: cols}
	}
}

// detectCmd runs a docker scan (plus env hints are folded in later, per target)
// off the Update goroutine. docker.Detect bounds its own deadline, so a hung
// daemon returns a Timeout error rather than blocking.
func detectCmd() tea.Cmd {
	return func() tea.Msg {
		containers, err := DetectFn(context.Background())
		if err != nil {
			return detectErrMsg{err: asDockerError(err)}
		}
		return detectDoneMsg{containers: containers}
	}
}

// startCmd starts a stopped container and waits for its port to accept
// connections, then reports success so the caller can re-scan.
func startCmd(c docker.Container) tea.Cmd {
	return func() tea.Msg {
		if err := docker.StartContainer(context.Background(), c.Name, c.HostPort); err != nil {
			return startErrMsg{err: asDockerError(err)}
		}
		return startedMsg{name: c.Name}
	}
}

// connectCmd runs the D11 credential ladder for a target container and opens a
// live engine connection. prompted carries whatever the user typed at the
// masked prompt; it overrides the recovered credentials (it is the last rung and
// only reached because the earlier ones were rejected). An auth failure always
// returns connectAuthMsg so the connect screen can (re-)show the masked prompt —
// this is how a wrong password loops the prompt instead of dead-ending.
func connectCmd(c docker.Container, prompted db.Target, hasPrompt bool) tea.Cmd {
	return func() tea.Msg {
		kind, ok := kindOf(c.Engine)
		if !ok {
			return connectErrMsg{err: errors.New("unsupported engine for " + c.Name)}
		}

		target := resolveTarget(c, kind)
		if hasPrompt {
			if prompted.User != "" {
				target.User = prompted.User
			}
			target.Password = prompted.Password
		}

		engine, err := NewEngineFn(kind)
		if err != nil {
			return connectErrMsg{err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()

		if err := engine.Connect(ctx, target); err != nil {
			_ = engine.Close()
			var dberr *db.DBError
			if errors.As(err, &dberr) && dberr.Kind == db.DBErrAuthFailed {
				return connectAuthMsg{target: target}
			}
			return connectErrMsg{err: err}
		}
		return connectedMsg{engine: engine, target: target, container: c}
	}
}

// resolveTarget builds the connection target from the non-interactive rungs of
// the ladder: Omarchy defaults (already on the container when detected) →
// docker inspect env → the cwd .env file. Later rungs only fill gaps left by
// earlier ones. Engine defaults backfill a user/maintenance-db when nothing
// recovered one.
func resolveTarget(c docker.Container, kind db.Kind) db.Target {
	creds := c.Creds
	port := c.HostPort

	// Rung 2: docker inspect env — recovers a port and/or creds we don't have.
	if creds.User == "" || creds.Password == "" || port == 0 {
		if p, ic, err := docker.Inspect(context.Background(), c.Name, c.Engine); err == nil {
			if port == 0 {
				port = p
			}
			creds = mergeCreds(creds, ic)
		}
	}

	// Rung 3: a .env / DATABASE_URL in the working directory.
	if creds.User == "" || creds.Password == "" {
		if hint, ok := docker.ReadEnvFile(cwd()); ok {
			creds = mergeCreds(creds, docker.Creds{
				User:     hint.User,
				Password: hint.Password,
				Database: hint.Database,
			})
		}
	}

	target := db.Target{
		Host:     "127.0.0.1",
		Port:     port,
		User:     creds.User,
		Password: creds.Password,
		Database: creds.Database,
	}
	applyEngineDefaults(&target, kind)
	return target
}

// mergeCreds fills empty fields of base from extra without overwriting anything
// base already recovered from a higher-priority rung.
func mergeCreds(base, extra docker.Creds) docker.Creds {
	if base.User == "" {
		base.User = extra.User
	}
	if base.Password == "" {
		base.Password = extra.Password
	}
	if base.Database == "" {
		base.Database = extra.Database
	}
	return base
}

// applyEngineDefaults backfills the conventional admin user and maintenance
// database when the ladder recovered none, so an Omarchy-less container can
// still connect without prompting for a username.
func applyEngineDefaults(t *db.Target, kind db.Kind) {
	switch kind {
	case db.KindPostgres:
		if t.User == "" {
			t.User = "postgres"
		}
		if t.Database == "" {
			t.Database = "postgres"
		}
	case db.KindMySQL, db.KindMariaDB:
		if t.User == "" {
			t.User = "root"
		}
	}
}

// kindOf maps a detected docker engine to the db engine kind. SQLite is never
// produced by container detection, so it has no mapping here.
func kindOf(e docker.Engine) (db.Kind, bool) {
	switch e {
	case docker.EnginePostgres:
		return db.KindPostgres, true
	case docker.EngineMySQL:
		return db.KindMySQL, true
	case docker.EngineMariaDB:
		return db.KindMariaDB, true
	}
	return 0, false
}

// asDockerError recovers the typed *docker.DockerError from an error, falling
// back to a generic internal error so the error screen always has a kind.
func asDockerError(err error) *docker.DockerError {
	var de *docker.DockerError
	if errors.As(err, &de) {
		return de
	}
	return &docker.DockerError{
		Kind:   docker.DockerErrInternal,
		Title:  "Something went wrong",
		Detail: err.Error(),
		Hint:   "press [r] to retry",
		Err:    err,
	}
}

// cwd returns the working directory, or "." when it can't be determined, so the
// .env rung degrades gracefully.
func cwd() string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return "."
}
