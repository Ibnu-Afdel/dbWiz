# DBWiz

**Find, browse, administer, and query your local SQL databases from the terminal — without memorizing container names or `docker exec` incantations.**

DBWiz detects the databases running in your local Docker containers (running *and* stopped), lets you browse tables and rows without writing SQL, do the common admin chores, and run real queries — all from one friendly TUI. It has first-class support for [Omarchy](https://omarchy.org)'s Docker database setup and works with any Docker container on any Linux, plus standalone SQLite files.

<!-- TODO(Ibnu): add a demo GIF and a couple of screenshots here before tagging v1.0.0
     e.g. docs/demo.gif (detect → dashboard → query) -->

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
go install github.com/Ibnu-Afdel/dbwiz@latest
```

Or grab a prebuilt static binary from the [Releases](https://github.com/Ibnu-Afdel/dbwiz/releases) page and put it on your `PATH`.

**Requirements:** Linux, the `docker` CLI, and your user in the `docker` group (DBWiz tells you how if not). SQLite files need nothing but the file.

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

On [Omarchy](https://omarchy.org) machines DBWiz recognizes the stock database containers, tags them with an `[omarchy]` badge, and uses their known default credentials — so `dbwiz` → pick a database → you're in, with no prompts. If you have no database containers yet, the empty state points you at `omarchy-install-docker-dbs`.

## Keys

The help bar at the bottom always reflects where you are; press `?` for the full keymap. The essentials:

| Key | Action |
|---|---|
| `↑`/`↓` (or `k`/`j`) | move / scroll rows |
| `←`/`→` (or `h`/`l`) | move across result columns |
| `Enter` | select / open · **inspect the selected cell** (results) |
| `Tab` | cycle panes (dashboard) |
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

Planning and roadmap live in a local `plan/` directory (gitignored).

## License

MIT
