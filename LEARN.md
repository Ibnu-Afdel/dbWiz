# Learning DBWiz

A guided tour of DBWiz, in the order you'll actually use it: launch → look
around → browse data → write SQL → change data → admin the server → script it
from the CLI. Sections near the top are the ones you'll reach for every
session; sections near the bottom are for the days you need them.

Two rules that make everything else easier to remember:

- **`?`** expands the bottom help bar to exactly the keys that work on the
  screen you're looking at, right now.
- **`F1`** opens the *entire* keymap (this same list, rendered inside the
  app) — a fixed reference, unlike `?` which changes with context.

Both read from the same source the key handlers use, so nothing documented
here can be stale or wrong about what a key does — including the CLI's own
`dbwiz keys`, which prints this exact list to your terminal.

---

## 1. The two-minute start

```bash
dbwiz
```

1. DBWiz scans Docker (running **and** stopped containers) and lands you on
   the **home menu** with whatever it found.
2. Pick **Use an existing database** — or press `s` first to start a stopped
   container.
3. You're on the **dashboard**. `Tab` cycles its panes (Databases → Tables →
   Users → Results), `Enter` opens whatever's selected, `i` describes a
   table, `e` jumps into the SQL editor, `?` shows what works right here.
4. No containers? **Open a SQLite file…** points at any `.sqlite`/`.db`
   file — no Docker needed.

No config, no connection string, usually no password prompt either — DBWiz
recovers credentials for you (Omarchy defaults → `docker inspect` env → a
project `.env`/`DATABASE_URL` → a masked prompt only as a last resort).

## 2. The mental model

```
home menu ──▶ dashboard (one tab per target)
                 ├─ Databases pane ─▶ Tables pane ─▶ Results grid (rows)
                 ├─ Users pane      (grants, create/edit/delete)
                 └─ SQL editor      (write, run, plan, history, save)
```

Every destructive action — dropping a database, truncating a table, deleting
a row — shows you what it's about to do and, for the ones that can't be
undone, makes you **type the name to confirm**. There's no "are you sure?"
you can reflexively dismiss.

You can have several **tabs** open at once, each its own connection to its
own target — handy for comparing two databases or keeping a query running in
one while you browse another.

## 3. Keys you'll use in every session

These work from anywhere on the dashboard.

| Key | Does |
|---|---|
| `?` | Expand the help bar to the keys that work right here |
| `F1` | Open the full keymap (every key, in one list) |
| `Tab` | Cycle focus between panes |
| `↑`/`↓` (`k`/`j`) | Move the selection |
| `Enter` | Open what's selected |
| `Esc` | Go back a step (or cancel a running statement) |
| `R` | Reload the focused list from the server |
| `e` | Jump into the SQL editor from anywhere |
| `^R` / `F5` | Run the statement in the editor |
| `^C` | Quit — works even mid-operation |
| `^T` / `^W` | Open / close a tab |
| `1`–`9` | Jump to a tab by number (once more than one is open) |

> **Under tmux**, `Ctrl+Enter` collapses to a plain `Enter` (a newline), so
> `Ctrl+R` or `F5` are the keys that always work to run a query. Add
> `set -g extended-keys on` to `tmux.conf` to get `Ctrl+Enter` working too.

## 4. Browsing data — no SQL required

This is the fast path for "let me just look at the table":

| Key | Where | Does |
|---|---|---|
| `f` | Databases / Tables / Users pane | Type part of a name to jump straight to it instead of scrolling |
| `i` | Tables pane | Describe the selected table (columns, types, keys) |
| `/` | Preview | Add a `WHERE` condition without writing a query |
| `#` | Preview | Count the table's rows exactly, instead of the stored estimate |
| `v` | Results pane | Show every column of the selected row as a scrollable list — for tables too wide for the grid |
| `y` | Results pane | Copy the selected cell (works over SSH, via OSC 52) |
| `Y` | Results pane | Copy the whole selected row |
| `⌥e` | Results pane | Export the current result to CSV or JSON |
| `←`/`→` (`h`/`l`) | Results pane | Scroll across columns — the footer shows `cols X–Y of Z` |

A wide result that gets cut off is normal — scroll with `←`/`→`, or hit
`Enter` on any cell to open its full, untruncated value in an overlay (great
for long text or JSON columns).

## 5. Changing data

Same grid, a few more keys — each one shows you the SQL it's about to run
before it runs it.

| Key | Where | Does |
|---|---|---|
| `u` | Results pane | Edit the selected cell (generates and shows the `UPDATE`) |
| `n` | Tables pane | Insert a row through a generated form, one field per column |
| `x` | Results pane | Delete the selected row (shows the `DELETE` first) |
| `T` | Tables pane | Truncate the table completely — asks for confirmation |

## 6. Writing SQL

`e` from anywhere on the dashboard drops you into the editor. It's
multi-line; nothing here is a single-line REPL.

| Key | Does |
|---|---|
| `^R` / `F5` (or `Ctrl+Enter`, `Alt+Enter`) | Run the statement |
| `Esc` | While running: cancel it, server-side |
| `Esc` | While idle: return to the panes |
| `^space` | Autocomplete a table/column name, a `table.column` once you type the dot, or a function — schema-wide, not just tables you've browsed |
| `⌥p` | Show the query plan — how the engine will run this, in plain language, with what's expensive flagged |
| `⌥f` | Reformat the statement into clause-per-line, keyword-cased SQL |
| `^p` / `^n` | Step back/forward through statements you've already run |
| `⌥h` | Search your full history for this target (not just the last few) |
| `⌥w` | Save the current statement under a name |
| `⌥s` | Open your saved queries |

The `⌥`-prefixed keys (history search, save, saved list, export, plan,
format) are all modifier combos on purpose — they need to work *while
you're mid-type* in the editor, so plain letters would just insert text.

### Vim mode (opt-in)

Off by default. Turn it on with `vim = true` in `~/.config/dbwiz/config.toml`
and the editor opens **modal**, like real vim: you land in normal mode and
have to press `i`/`a`/`o`/`O` to start typing, `Esc` to stop. It's a
deliberately small subset — enough for editing a statement, not a full
editor:

| Key | Mode | Does |
|---|---|---|
| `i` / `a` | normal → insert | Insert before / after the cursor |
| `I` / `A` | normal → insert | Insert at line start / end |
| `o` / `O` | normal → insert | Open a new line below / above |
| `Esc` | insert → normal | Stop typing, back to motions |
| `h`/`j`/`k`/`l` | normal | Move by character/line |
| `w` / `b` | normal | Jump forward / back a word |
| `0` / `$` | normal | Jump to line start / end |
| `gg` / `G` | normal | Jump to buffer start / end |
| `x` | normal | Delete the character under the cursor |
| `D` | normal | Delete to end of line |
| `dd` | normal | Delete the whole line |
| `Esc` | normal | Leave the editor for the panes (same exit as non-vim) |

`^p`/`^n` (history cycling) still work in insert mode. Everything outside
the editor — the panes, overlays, admin keys — is unaffected; vim mode only
changes how typing behaves once you're inside the SQL editor.

## 7. Databases and users (admin)

Focus decides what these act on: the Databases pane or the Users pane.

| Key | Where | Does |
|---|---|---|
| `c` | Databases / Users pane | Create a database or a user |
| `D` | Databases / Users pane | Drop the selected one — you must type its name to confirm |
| `g` | Users pane | Grant/revoke a user's permissions on a database |
| `a` | Users pane | Edit the selected user's flags or password |
| `y` | Databases pane | Yank a ready-to-paste connection URL to the clipboard |
| `B` | Databases pane | Dump the selected database to a file, or restore one into it |
| `S` | Databases pane | Compare this database's structure against another on the same server |
| `M` | Databases pane | Show the migration ledger — which tool manages it, how far it's got |

`y` doing two different things (copy-cell vs. yank-URL) isn't a conflict —
it fires on whichever pane has focus, and only one of the two panes has a
selected cell at all.

## 8. Finding your way in on the home screen

| Key | Does |
|---|---|
| `r` | Rescan Docker for database containers |
| `s` | Start the selected stopped container |
| `r` (error screen) | Retry whatever just failed |
| `i` (error screen) | Show the details behind the failure |

## 9. Scripting it — the CLI companion commands

Everything DBWiz can do interactively also has a scriptable, non-interactive
form. Most useful first:

| Command | Does |
|---|---|
| `dbwiz` | Launch the interactive TUI (everything above) |
| `dbwiz list [--ssh user@host]` | List detected containers — locally, or scan a remote Docker daemon over SSH |
| `dbwiz use <container>` | Set the target DBWiz opens next, so you skip the picker |
| `dbwiz query <sql> [--json\|--csv]` | Run one statement and print the result — for scripts and pipes |
| `dbwiz url [database] [-f url\|env\|jdbc\|all] [--clipboard]` | Print (or copy) a connection string |
| `dbwiz health` | Check a target end-to-end; exits non-zero if anything's wrong — good for CI |
| `dbwiz explain <sql> [--analyze] [--json\|--raw]` | Show how the engine will run a statement, without opening the TUI |
| `dbwiz schema dump [database] [--table x] [-o file]` | Export a database's structure as DDL |
| `dbwiz schema diff <a> <b> [--against-target ...]` | Compare two databases' structure, even across containers |
| `dbwiz migrations [database] [-n limit] [--all] [--json]` | Show what the migration ledger says |
| `dbwiz create <database> [--user] [--user-password ...]` | Create a database, optionally with a matching user |
| `dbwiz drop <database> --yes` | Drop a database (the `--yes` is mandatory — no accidental drops) |
| `dbwiz dump <database> [-o file]` | Back up a database via the container's own `pg_dump`/`mysqldump` |
| `dbwiz restore <database> <file> --yes` | Restore a dump (again, `--yes` required) |
| `dbwiz ext list [--installed]` / `dbwiz ext create <name>` | List or install PostgreSQL extensions |
| `dbwiz setup [postgres\|mysql\|mariadb\|postgis] [--no-wait]` | Provision a fresh database container, Omarchy-compatible |
| `dbwiz keys` | Print this same keymap to your terminal |

Common flags across most of these: `-t/--target <container>` (defaults to
the one you `use`d, or the only one found), `--password` (or set
`DBWIZ_PASSWORD` instead of typing it on the command line), `-d/--database`.

## 10. Configuration (all optional)

DBWiz needs nothing to work. To customize it, drop a file at
`~/.config/dbwiz/config.toml` (respects `$XDG_CONFIG_HOME`):

```toml
theme = "default"          # "default", "high-contrast", or "warm"
default_row_limit = 200    # rows a table preview pulls

[[target]]
name = "prod read-replica"
engine = "postgres"        # "postgres", "mysql", or "mariadb"
host = "db.internal"
port = 5432
user = "readonly"
database = "appdb"         # optional

[[target]]
name = "staging mysql"
engine = "mysql"
host = "127.0.0.1"
port = 3306
user = "root"
```

`[[target]]` entries are for databases DBWiz can't find via Docker — a
remote server or a host-native one. They show up on the home menu; DBWiz
still prompts for the password (never stored on disk). A malformed file is
reported on stderr and DBWiz falls back to defaults; a missing file is fine.

**Environment variables:**

| Variable | Does |
|---|---|
| `DBWIZ_PASSWORD` | Supply a password without typing it or putting it in shell history |
| `DBWIZ_DEBUG=1` | Append a diagnostic log (Docker commands, SQL run, passwords redacted) to `~/.local/state/dbwiz/dbwiz.log` |

## 11. The order that actually works

If you only remember one path through all of this:

1. `dbwiz` → pick a database (`s` to start it if it's stopped).
2. `Tab` to the Tables pane, `f` to jump to the one you want, `Enter` to
   open it, `i` if you first want to know its shape.
3. Look at rows in the grid; `/` to filter, `#` for an exact count, `v` on a
   row that's too wide.
4. Need to change something? `u` to edit a cell, `n` to insert, `x` to
   delete — each shows you the SQL first.
5. Need more than the grid can do? `e` into the editor, write it, `^R`/`F5`
   to run, `⌥p` if you're unsure how expensive it'll be.
6. Forgot a key? `?` for what works here, `F1` for everything.
