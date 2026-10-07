# DBWiz

**Find, browse, administer, and query your local SQL databases from the terminal — without memorizing container names or `docker exec` incantations.**

DBWiz detects the databases running in your local Docker containers (running *and* stopped), lets you browse tables and rows without writing SQL, do the common admin chores, and run real queries — all from one friendly TUI. It has first-class support for [Omarchy](https://omarchy.org)'s Docker database setup and works with any Docker container on any Linux, plus standalone SQLite files.

## Features

- **Zero-config detection** — finds your PostgreSQL / MySQL / MariaDB containers automatically. Stopped ones are listed too and can be **started with a keypress**.
- **A home menu, not a maze** — use an existing database, create a new one and drop straight into it, or open a SQLite file.
- **Browse without SQL** — databases → tables → columns and rows, in a scrollable grid. NULLs are shown distinctly.
- **Admin the safe way** — create/delete databases and users, grant a user access to a database. Every destructive action requires you to **type the name to confirm**.
- **A real query editor** — multi-line SQL, run with `Ctrl+Enter` (or `F5`), cancel a running statement with `Esc`, cycle previous statements with `Ctrl+P`/`Ctrl+N`. Results land in the same scrollable grid.
- **Credentials recovered for you** — Omarchy defaults → `docker inspect` env → a project `.env`/`DATABASE_URL` → a masked prompt only if all else fails. Usually you type nothing.
- **Beginner-friendly by design** — plain-language errors that always offer a next step, a persistent help bar (`?` for the full keymap), and no assumed `psql` knowledge. Nothing dead-ends.
- **One static binary** — pure-Go drivers, `CGO_ENABLED=0`, no runtime dependencies beyond the `docker` CLI.

## Supported engines

| Engine | Detect & connect | Browse & query | Users & grants |
|---|:---:|:---:|:---:|
| **PostgreSQL** (first-class) | ✅ | ✅ | ✅ |
| **MySQL / MariaDB** | ✅ | ✅ | ✅ |
| **SQLite** (open a file) | ✅ | ✅ | — (no user model) |

SQLite has no user/permission concept, so that UI is simply absent for it — not greyed out.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Ibnu-Afdel/dbWiz/main/install.sh | sh
```

The script downloads the static binary for your CPU (x86_64 or arm64), verifies its SHA-256 checksum, and installs it to `~/.local/bin` — no sudo. ([Read it first](install.sh) if you like; `DBWIZ_VERSION` and `DBWIZ_INSTALL_DIR` override the defaults.)

On Arch or Omarchy, from the [AUR](https://aur.archlinux.org/packages/dbwiz-bin) (includes shell completions):

```bash
yay -S dbwiz-bin                 # or any AUR helper
omarchy pkg aur add dbwiz-bin    # the Omarchy way
```

With Go:

```bash
go install github.com/Ibnu-Afdel/dbwiz@latest
```

Or grab a binary from the [Releases](https://github.com/Ibnu-Afdel/dbWiz/releases) page and put it on your `PATH`.

Then, optionally, add it to your app launcher (any Linux desktop; on Omarchy it also joins the Omarchy menu):

```bash
dbwiz desktop install   # launcher entry + icon (+ Omarchy menu section)
dbwiz desktop remove    # take it all back out
```

**Requirements:** Linux and the `docker` CLI. If your user can't reach the Docker socket — the default on Omarchy 4, which keeps you out of the root-equivalent `docker` group — run `dbwiz --sudo` (or press `s` on the permission screen): DBWiz asks sudo for your password once and runs only its `docker` calls through it. SQLite files need nothing but the file.

## Quickstart (30 seconds)

```bash
dbwiz
```

1. DBWiz scans Docker and drops you on the **home menu** with your databases listed.
2. Pick **Use an existing database** (or press `s` to start a stopped one first).
3. On the **dashboard**: `Tab` cycles panes, `Enter` opens a table, `i` describes it, `e` focuses the SQL editor, `?` shows every key.
4. No containers? Choose **Open a SQLite file…** and point it at a `.sqlite`/`.db` file.

That's it — no config files, no connection strings.

## Omarchy

On [Omarchy](https://omarchy.org) machines (Omarchy 4 and the older 3.x layout) DBWiz recognizes the stock database containers from `omarchy install docker dbs`, tags them with an `[omarchy]` badge, and uses their known default credentials — so `dbwiz` → pick a database → you're in, with no prompts. `dbwiz setup` creates the exact same containers, so the two are interchangeable.

**Follows your theme.** DBWiz paints with the active Omarchy theme's palette (accent, text, muted, red/green/yellow) and recolors live when you switch themes — no restart. Set `theme = "default"` (or another named theme) in the config to opt out.

**In the launcher and the menu.** `dbwiz desktop install` puts DBWiz in the app launcher, opening through `omarchy-launch-or-focus-tui` — so it starts in your default terminal, or jumps to the DBWiz window that's already open. It also adds a **Databases** section to the Omarchy menu (Open DBWiz · List Databases · Health Check · Add a Database Server) via `~/.config/omarchy/extensions/omarchy-menu.jsonc`; the block is clearly marked, your original file is backed up to `omarchy-menu.jsonc.dbwiz.bak`, and every edit is checked against Omarchy's own parser rules before it's written. Use `--no-menu` to skip the menu. For a dedicated key, the install prints a ready-to-paste `bindings.lua` line (`SUPER + SHIFT + ALT + D` is free by default; `SUPER + SHIFT + D` is Omarchy's Docker TUI).

**Docker access.** Omarchy 4 deliberately leaves your user out of the `docker` group and reaches Docker through a sudo/polkit prompt. DBWiz respects that: it never escalates on its own. Either launch it as `dbwiz --sudo` (one password prompt, used only for DBWiz's `docker` calls), press `s` when it says it can't reach Docker, or opt in to sudoless Docker the Omarchy way — `omarchy setup security sudoless docker` (Setup › Security › Sudoless Docker), then reboot.

## Configuration (optional)

DBWiz needs no configuration — everything works out of the box. But you can drop an opt-in file at `~/.config/dbwiz/config.toml` (respects `$XDG_CONFIG_HOME`) to set a theme, change the default preview row limit, and save **manual targets**: databases DBWiz can't find through Docker (a remote or host-native server you reach by host/port). Saved targets appear on the home menu; DBWiz still prompts for the password (it's never stored on disk).

```toml
# ~/.config/dbwiz/config.toml — every field is optional

theme = "omarchy"          # "omarchy", "default", "high-contrast", or "warm" (unset = auto)
default_row_limit = 200    # rows a table preview pulls

[[target]]
name = "prod read-replica"
engine = "postgres"        # "postgres", "mysql", or "mariadb"
host = "db.internal"
port = 5432
user = "readonly"
database = "appdb"         # optional initial database

[[target]]
name = "staging mysql"
engine = "mysql"
host = "127.0.0.1"
port = 3306
user = "root"
```

A malformed file is reported on stderr and DBWiz launches with defaults; a missing file is not an error.

## Keys

The help bar at the bottom always reflects where you are; press `?` for the keys on the current screen, `F1` (or `dbwiz keys`) for all of them. For a one-page reference see [`cheatsheet.md`](cheatsheet.md), and for a guided tour of every feature, [`LEARN.md`](LEARN.md). The essentials:

| Key | Action |
|---|---|
| `↑`/`↓` (or `k`/`j`) | move / scroll rows |
| `←`/`→` (or `h`/`l`) | move across result columns |
| `Enter` | select / open · **inspect the selected cell** (results) |
| `Tab` | cycle panes (dashboard) |
| `c` / `D` | create / delete (databases · users) |
| `g` / `a` | grant matrix · edit user (users pane) |
| `e` | focus the SQL editor |
| `Ctrl+R` / `F5` | run the query |
| `Esc` | back / cancel |
| `?` | full keymap |
| `Ctrl+C` | quit (from anywhere) |

## Troubleshooting

**Query won't run inside tmux?** Under tmux, `Ctrl+Enter` collapses to a plain
Enter (a newline), so use **`Ctrl+R`** or **`F5`** to run — both work everywhere.
To make `Ctrl+Enter` work too, add `set -g extended-keys on` to your `tmux.conf`.

**A wide result gets cut off?** Move across columns with `←`/`→` (or `h`/`l`)
while the Results pane is focused (the footer shows `cols X–Y of Z`), and press
**`Enter`** on any cell to open it in a scrollable overlay that shows the full,
untruncated value — handy for long descriptions or JSON.

Set `DBWIZ_DEBUG=1` to append a diagnostic log — the docker commands and SQL DBWiz runs, **with passwords redacted** — to `~/.local/state/dbwiz/dbwiz.log` (respects `XDG_STATE_HOME`). It's off by default and never records credentials.

## Stack

Go · [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) · `pgx` / `go-sql-driver/mysql` / `modernc.org/sqlite` (pure Go — single static binary). The `docker` CLI is shelled out for container detection only.

## Development

```bash
go run .                     # launch the TUI
CGO_ENABLED=0 go build ./... # static build
go test ./...                # unit tests (no Docker needed)
go test -tags integration ./internal/db/...  # live DB tests (need running containers)
```

The integration tests default to a local `postgres` superuser on `127.0.0.1:5432` (what `dbwiz setup postgres` or `omarchy install docker dbs` gives you); point them elsewhere with `DBWIZ_PG_HOST`, `DBWIZ_PG_PORT`, `DBWIZ_PG_USER`, `DBWIZ_PG_PASS`, `DBWIZ_PG_DB`, and enable the MySQL/MariaDB suite with `DBWIZ_MY=1` (`DBWIZ_MY_*` for its connection).

## License

[MIT](LICENSE)
