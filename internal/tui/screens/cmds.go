package screens

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/config"
	"github.com/Ibnu-Afdel/dbwiz/internal/connect"
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/debuglog"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/remote"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
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
	// connect screen pushes the dashboard with it. remote marks a non-local
	// connection (SSH tunnel or non-loopback host) so the dashboard shows the
	// REMOTE rails (v3 3.4).
	connectedMsg struct {
		engine    db.Engine
		target    db.Target
		container docker.Container
		remote    bool
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

	// schemaPrefetchMsg carries the columns of every table a background
	// describe pass reached after a table list loaded (v5 3.1) — a cache
	// warm-up for autocomplete, not a user-visible action, so there's no
	// error variant: a table that failed to describe is just missing from
	// columns, silently, and completes normally once actually browsed.
	schemaPrefetchMsg struct {
		database string
		columns  map[string][]string
	}

	// completeColumnsMsg answers a just-in-time single-table describe
	// (fetchColumnsForCompleteCmd, v5 3.2) — the fallback for a `table.`
	// completion whose table the background prefetch hasn't reached yet.
	// prefix is carried through so the picker that opens on arrival is
	// filtered exactly as it would have been synchronously; columns is nil
	// on a describe failure, which openComplete's caller renders as "no
	// completions" rather than an error.
	completeColumnsMsg struct {
		table   string
		prefix  string
		columns []string
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

	// Grant matrix (v2 3.1). grantsLoadedMsg carries a user's current privileges
	// on a database into the open matrix; grantSetMsg acknowledges one applied
	// grant/revoke; grantsErrMsg is a typed failure of either, shown inline in the
	// overlay. Each carries the user+database it was requested for so a late reply
	// for a since-changed selection is dropped.
	grantsLoadedMsg struct {
		user, database string
		held           []db.Privilege
	}
	grantSetMsg struct {
		user, database string
		priv           db.Privilege
		grant          bool
	}
	grantsErrMsg struct {
		user, database string
		err            *db.DBError
	}

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

	// historyPersistedMsg acknowledges a best-effort history write. It carries
	// nothing — the dashboard ignores it — but lets the persist run as a proper
	// tea.Cmd off the Update goroutine.
	historyPersistedMsg struct{}

	// savedPersistedMsg acknowledges a best-effort saved-query write, like
	// historyPersistedMsg. The dashboard ignores it.
	savedPersistedMsg struct{}

	// savedDeletedMsg carries the saved-query list refreshed after a delete (read
	// off the Update goroutine), so the open picker can rebuild — or close, when
	// the last entry is gone — without a second store read on the UI thread.
	savedDeletedMsg struct{ items []savedQueryItem }
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

// persistHistoryCmd writes a just-run statement to the cross-session history
// store off the Update goroutine (protocol #4: never block Update on I/O). It's
// best-effort — history is disposable cache, so a write failure is swallowed and
// reported as a no-op message the dashboard ignores.
func persistHistoryCmd(key, sql string) tea.Cmd {
	return func() tea.Msg {
		// Redact any PASSWORD / IDENTIFIED BY literal before it lands on disk, the
		// same structural guarantee the debug log makes (v1 8.4): DBWiz never writes
		// a password to a file. The in-memory session ring keeps the raw text, so
		// same-session recall stays exact; only the persisted copy is masked.
		_ = state.AddHistory(key, debuglog.Redact(sql))
		return historyPersistedMsg{}
	}
}

// persistSavedCmd writes a named query to the cross-session store off the Update
// goroutine (protocol #4). Like history it redacts any PASSWORD / IDENTIFIED BY
// literal first, upholding "DBWiz never writes a password to a file" (v1 8.4). An
// empty key targets the global scope. Best-effort: a write failure is swallowed.
func persistSavedCmd(key, name, sql string) tea.Cmd {
	return func() tea.Msg {
		_ = state.AddSaved(key, name, debuglog.Redact(sql))
		return savedPersistedMsg{}
	}
}

// deleteSavedCmd forgets a named query from scope (empty key = global) and reads
// back the picker's refreshed items for curKey off the Update goroutine, so the
// open overlay can rebuild from a single, consistent snapshot.
func deleteSavedCmd(key, name, curKey string) tea.Cmd {
	return func() tea.Msg {
		_ = state.DeleteSaved(key, name)
		return savedDeletedMsg{items: collectSaved(curKey)}
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

// alterUserCmd applies an edit-user form (v2 3.2): it sets role flags first
// (only where the engine has them) then changes the password when one was typed
// (a blank password leaves the current one untouched). It stops at the first
// failure so a partial result is reported clearly.
func alterUserCmd(engine db.Engine, name string, hasFlags bool, canLogin, createDB bool, password string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		if hasFlags {
			if err := engine.AlterUser(ctx, name, canLogin, createDB); err != nil {
				return adminErrMsg{err: asDBError(err)}
			}
		}
		if password != "" {
			if err := engine.SetPassword(ctx, name, password); err != nil {
				return adminErrMsg{err: asDBError(err)}
			}
		}
		return adminDoneMsg{notice: "Updated user " + name, reloadUsers: true}
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

// loadGrantsCmd reads which database-scope privileges user currently holds on
// database, feeding the grant matrix (v2 3.1).
func loadGrantsCmd(engine db.Engine, user, database string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		held, err := engine.ListGrants(ctx, user, database)
		if err != nil {
			return grantsErrMsg{user: user, database: database, err: asDBError(err)}
		}
		return grantsLoadedMsg{user: user, database: database, held: held}
	}
}

// setGrantCmd grants or revokes a single privilege for user on database. On
// success it reports grantSetMsg; the dashboard then reloads the matrix so the
// checkmarks reflect what the server actually did.
func setGrantCmd(engine db.Engine, user, database string, priv db.Privilege, grant bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		if err := engine.SetGrant(ctx, user, database, priv, grant); err != nil {
			return grantsErrMsg{user: user, database: database, err: asDBError(err)}
		}
		return grantSetMsg{user: user, database: database, priv: priv, grant: grant}
	}
}

// browseTimeout bounds a single browser read (list/preview/describe) so a wedged
// or killed server surfaces as a pane-level Timeout error instead of freezing
// the UI mid-browse.
const browseTimeout = 15 * time.Second

// previewLimit caps how many rows a table preview pulls, keeping the preview
// snappy on large tables. It defaults to 200 and is overridden by
// config.DefaultRowLimit via ApplyConfig (v2 3.3).
var previewLimit = 200

// savedTargets holds the valid manual (non-Docker) targets from config, offered
// on the home menu (v2 3.3). ApplyConfig populates it; it is empty with no config.
var savedTargets []config.ManualTarget

// vimEditor turns on the modal SQL editor (v2 2.4). It defaults off (the plain
// textarea) and is set from config.Editor.Vim via ApplyConfig, so every new
// dashboard reads one shared preference.
var vimEditor bool

// ApplyConfig folds the user's opt-in config into the screens layer at startup:
// the preview row cap, the saved manual targets, and the editor mode. It is
// called once from tui.Run before the program starts; with no config it is a
// harmless no-op.
func ApplyConfig(cfg config.Config) {
	if cfg.DefaultRowLimit > 0 {
		previewLimit = cfg.DefaultRowLimit
	}
	savedTargets = cfg.ValidTargets()
	vimEditor = cfg.Editor.Vim
}

// SavedTargets returns the configured manual targets (v2 3.3), for the home menu.
func SavedTargets() []config.ManualTarget { return savedTargets }

// manualKind maps a saved target's engine string to a db.Kind. The set matches
// config.knownEngines; an unknown string returns ok=false.
func manualKind(engine string) (db.Kind, bool) {
	switch engine {
	case "postgres":
		return db.KindPostgres, true
	case "mysql":
		return db.KindMySQL, true
	case "mariadb":
		return db.KindMariaDB, true
	}
	return 0, false
}

// dockerEngineOf is the reverse of kindOf, used to label a manual target with a
// docker.Engine so the dashboard's tab/title code (which expects a container)
// works unchanged.
func dockerEngineOf(k db.Kind) docker.Engine {
	switch k {
	case db.KindMySQL:
		return docker.EngineMySQL
	case db.KindMariaDB:
		return docker.EngineMariaDB
	default:
		return docker.EnginePostgres
	}
}

// manualContainer synthesizes the docker.Container a manual connection hands to
// the dashboard for its label — there is no real container, just enough for the
// tab title and status bar.
func manualContainer(name string, kind db.Kind, port int) docker.Container {
	return docker.Container{Name: name, Engine: dockerEngineOf(kind), State: docker.StateRunning, HostPort: port}
}

// connectManualCmd opens a live engine for a manual target (v2 3.3, v3 3.1).
// Unlike connectCmd it runs no credential ladder — a manual target carries its
// own host/port/user and DBWiz prompts for the password only if the server
// rejects what was entered. When ssh is non-nil it first opens an SSH tunnel
// (v3 3.2) and connects the driver to the tunnel's local port; the engine is
// then wrapped so closing it also tears the tunnel down. Each attempt (including
// a password retry) opens its own tunnel, so a rejected login never leaks one.
func connectManualCmd(name string, kind db.Kind, base db.Target, ssh *remote.SSHSpec, prompted db.Target, hasPrompt bool) tea.Cmd {
	return func() tea.Msg {
		target := base
		if hasPrompt {
			target.Password = prompted.Password
		}

		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()

		// Redirect the target through an SSH tunnel when one is configured.
		var tunnel *remote.Tunnel
		if ssh != nil {
			tun, err := openTunnel(ctx, *ssh, base.Host, base.Port)
			if err != nil {
				return connectErrMsg{err: err}
			}
			tunnel = tun
			target.Host = tun.LocalHost()
			target.Port = tun.LocalPort()
		}

		engine, err := NewEngineFn(kind)
		if err != nil {
			closeTunnel(tunnel)
			return connectErrMsg{err: err}
		}
		if err := engine.Connect(ctx, target); err != nil {
			_ = engine.Close()
			closeTunnel(tunnel)
			var dberr *db.DBError
			if errors.As(err, &dberr) && dberr.Kind == db.DBErrAuthFailed {
				return connectAuthMsg{target: target}
			}
			return connectErrMsg{err: err}
		}
		if tunnel != nil {
			engine = tunneledEngine{Engine: engine, tunnel: tunnel}
		}
		return connectedMsg{
			engine:    engine,
			target:    target,
			container: manualContainer(name, kind, base.Port),
			remote:    manualIsRemote(base.Host, ssh),
		}
	}
}

// manualIsRemote reports whether a manual connection is non-local: any SSH
// tunnel is remote, and so is a direct connection to a non-loopback host. A
// direct 127.0.0.1/localhost target (a host-native server) is treated as local.
func manualIsRemote(host string, ssh *remote.SSHSpec) bool {
	if ssh != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "::1", "localhost", "":
		return false
	}
	return true
}

// openTunnel dials the SSH host and opens a local forward to host:port as seen
// from that host. The returned tunnel owns the SSH client, so closing it closes
// both.
func openTunnel(ctx context.Context, spec remote.SSHSpec, host string, port int) (*remote.Tunnel, error) {
	client, err := remote.Dial(ctx, spec)
	if err != nil {
		return nil, err
	}
	tun, err := client.Tunnel(host, port)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return tun, nil
}

func closeTunnel(t *remote.Tunnel) {
	if t != nil {
		_ = t.Close()
	}
}

// tunneledEngine wraps a live engine whose connection runs over an SSH tunnel,
// tying the tunnel's lifetime to the engine's: every Engine method is the inner
// engine's (promoted), and Close tears down the tunnel after the connection. The
// dashboard closes the engine on Back, so the tunnel is released the same way.
type tunneledEngine struct {
	db.Engine
	tunnel *remote.Tunnel
}

func (e tunneledEngine) Close() error {
	err := e.Engine.Close()
	closeTunnel(e.tunnel)
	return err
}

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

// prefetchTimeout bounds the whole background schema-prefetch pass
// (prefetchColumnsCmd, v5 3.1), not one describe within it — generous
// relative to browseTimeout because it's a background cache warm-up with no
// user waiting on it, and prefetchColumnsMax already bounds the worst case
// on a very wide schema.
const prefetchTimeout = 60 * time.Second

// prefetchColumnsCmd warms the autocomplete column cache for every table in
// tables that known doesn't already cover, up to prefetchColumnsMax (v5
// 3.1). It runs after every table-list load, so a database switch or a
// manual refresh (Keys.Refresh) both keep it current; tables already in
// known are skipped, so a repeat load of the same database costs nothing
// extra. A single table's describe failure (a permission gap, say) is
// skipped rather than surfaced — this is a cache warm-up, not a
// user-initiated action, so it fails silently and completes normally for
// every other table.
func prefetchColumnsCmd(engine db.Engine, database string, tables []db.Table, known map[string][]string) tea.Cmd {
	return func() tea.Msg {
		if len(tables) == 0 {
			return schemaPrefetchMsg{database: database}
		}
		ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
		defer cancel()
		columns := map[string][]string{}
		fetched := 0
		for _, t := range tables {
			if fetched >= prefetchColumnsMax {
				break
			}
			if _, ok := known[t.Name]; ok {
				continue
			}
			cols, err := engine.DescribeTable(ctx, database, t.Name)
			if err != nil {
				continue
			}
			names := make([]string, len(cols))
			for i, c := range cols {
				names[i] = c.Name
			}
			columns[t.Name] = names
			fetched++
		}
		return schemaPrefetchMsg{database: database, columns: columns}
	}
}

// fetchColumnsForCompleteCmd describes exactly one table for a `table.`
// completion whose columns aren't cached yet (v5 3.2) — a single round trip,
// not the whole-schema walk prefetchColumnsCmd does. prefix rides along so
// the picker that opens on arrival filters the same way it would have
// synchronously.
func fetchColumnsForCompleteCmd(engine db.Engine, database, table, prefix string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), browseTimeout)
		defer cancel()
		cols, err := engine.DescribeTable(ctx, database, table)
		if err != nil {
			return completeColumnsMsg{table: table, prefix: prefix}
		}
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.Name
		}
		return completeColumnsMsg{table: table, prefix: prefix, columns: names}
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
		kind, ok := connect.KindOf(c.Engine)
		if !ok {
			return connectErrMsg{err: errors.New("unsupported engine for " + c.Name)}
		}

		target := connect.Target(context.Background(), c, kind)
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
