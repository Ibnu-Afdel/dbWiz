# Changelog

All notable changes to DBWiz are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## v1.1.0 — tabs, remote targets, schema tools, and built in for Omarchy 4

Everything since v1.0.0: work with several databases at once, change data from
the browser, reach databases beyond local Docker, script it all from the CLI,
and feel at home on Omarchy 4.

### Added

- **Tabs** — several connections open at once (`Ctrl+T` / `Ctrl+W`, `1`–`9` to
  switch, tmux-friendly). DBWiz remembers the last target and offers to continue
  on relaunch, and keeps your recent SQLite files.
- **Change data from the browser** — edit a cell, insert a row from a generated
  form, or delete a row, always previewing the exact `UPDATE` / `INSERT` /
  `DELETE` first. Plus truncate, exact row counts, and a quick `WHERE` filter.
- **Query workflow** — autocomplete for tables, columns, and functions; a SQL
  formatter; copy a cell or row; a full-row viewer for wide tables; and an
  optional vim-style editor (`[editor] vim = true`).
- **Query plans in plain language** — `⌥p` (or `dbwiz explain`) turns `EXPLAIN`
  into a readable tree with the slow parts flagged.
- **Schema tools** — capture a schema, diff two of them, and export DDL
  (`dbwiz schema`); framework-aware hints show which migration a database is on
  (`dbwiz migrations`).
- **Backup and restore** — from the TUI (`B`) or `dbwiz dump` / `dbwiz restore`.
- **Beyond local Docker** — connect any host/port Postgres or MySQL, tunnel
  through SSH, and scan a server's Docker over SSH (`dbwiz list --ssh`).
  Non-local targets wear a REMOTE badge and ask for an extra confirmation
  before anything destructive.
- **Provisioning and health** — `dbwiz setup` creates Omarchy-compatible
  database containers; `dbwiz health` runs a connectivity self-check; `dbwiz url`
  prints a connection URL; `dbwiz ext` manages Postgres extensions.
- **Scripting surface** — `dbwiz use`, `list`, `query`, `create`, `drop`, and
  `keys` for shell scripts and CI; `F1` shows every key, from the same catalogue
  the handlers use.
- **Opt-in config** — `~/.config/dbwiz/config.toml` for theme, row limit, and
  saved targets.
- **Follows the Omarchy theme, live** — on Omarchy, DBWiz paints with the active
  theme's palette and recolors as soon as you switch themes. `theme = "default"`
  (or any named theme) opts out.
- **`dbwiz desktop install` / `remove`** — an app-launcher entry and icon on any
  Linux desktop. On Omarchy it opens via launch-or-focus and adds a Databases
  section to the Omarchy menu (`--no-menu` to skip).
- **`--sudo` (or `DBWIZ_SUDO=1`)** — when your user can't reach the Docker socket,
  authorize sudo once and DBWiz runs only its `docker` calls through it. In the
  TUI, press `s` on the permission screen to do the same.

### Changed

- **Dashboard navigation and SQL editing** reworked: type-to-jump with `f`,
  `e` to reach the editor from anywhere.
- **Omarchy 4 detection** — recognizes the packaged install (`$OMARCHY_PATH`,
  `/usr/share/omarchy`) as well as the Omarchy 3 layout.
- **Docker permission errors** now suggest Omarchy's own opt-in
  (`omarchy setup security sudoless docker`) on Omarchy, and notice a docker-group
  change that's still waiting on a reboot.
- The empty state points at `omarchy install docker dbs`.

## v1.0.0 — MVP: find it, browse it, admin it, query it

The first release. Launch `dbwiz` on a machine with a Docker database (or a
SQLite file) and, with zero configuration, manage it from a friendly terminal UI.

### Added

- **Zero-config Docker detection** — finds PostgreSQL / MySQL / MariaDB
  containers automatically, running or stopped; start a stopped one with a
  keypress. First-class [Omarchy](https://omarchy.org) support (badges + known
  default credentials).
- **Home menu routes** — use an existing database, create a new database and
  continue straight into it, or open a SQLite file.
- **Table browser** — databases → tables → columns and rows in a scrollable
  grid, no SQL required; NULLs shown distinctly; `i` describes a table.
- **Admin** — create/delete databases and users, grant/revoke a user's access to
  a database. Every destructive action requires typing the name to confirm.
- **Query editor** — multi-line SQL; run with `Ctrl+Enter` / `F5`, cancel with
  `Esc`, in-memory history via `Ctrl+P`/`Ctrl+N`; results in the shared grid.
- **Credential ladder** — Omarchy defaults → `docker inspect` env → project
  `.env`/`DATABASE_URL` → masked prompt, so you usually type nothing.
- **Beginner safety** — plain-language errors that always offer a next step, a
  persistent help bar (`?` for the full keymap), a first-run hint, and no
  dead ends (`Ctrl+C` quits from anywhere).
- **Opt-in debug log** — `DBWIZ_DEBUG=1` writes `~/.local/state/dbwiz/dbwiz.log`
  (docker commands + SQL, **passwords redacted**).
- **`--version`** flag; single static binary (`CGO_ENABLED=0`, pure-Go drivers).

### Engines

| Engine | Detect & connect | Browse & query | Users & grants |
|---|:---:|:---:|:---:|
| PostgreSQL | ✅ | ✅ | ✅ |
| MySQL / MariaDB | ✅ | ✅ | ✅ |
| SQLite | ✅ | ✅ | — (no user model) |

### Known limitations

- Query results are fetched in full; a `SELECT` on a very large table with no
  `LIMIT` can use significant memory. Prefer `LIMIT` for large tables (a capped
  fetch + notice is planned for v2).
- Remote/TLS databases, saved queries, tabs, export, and a full permission
  matrix are v2+.
