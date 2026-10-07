# Changelog

All notable changes to DBWiz are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## Unreleased — built in for Omarchy 4

### Added

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
