# DBWiz cheatsheet

The keys that do the heavy lifting. Full list: `?` (here), `F1` (everything).
Everything below fires on the dashboard, after `dbwiz` → pick a database.

## If you remember five keys

| Key | Does |
|---|---|
| `Tab` | Cycle panes (Databases → Tables → Users → Results) |
| `f` | Type-to-jump to a table/database/user — don't scroll |
| `e` | Jump into the SQL editor from anywhere |
| `^R` / `F5` | Run the statement |
| `Esc` | Back / cancel |

## Looking at data (no SQL)

| Key | Does |
|---|---|
| `i` | Describe the selected table — columns, types, keys |
| `Enter` | Open a table · inspect a cell's full value |
| `/` | Filter the preview (a WHERE, without writing one) |
| `v` | See a whole row, one field per line — for tables too wide for the grid |
| `#` | Exact row count (not the estimate) |
| `y` / `Y` | Copy the selected cell / row |

## Writing SQL

| Key | Does |
|---|---|
| `^space` | Autocomplete — table, `table.column`, or a function |
| `⌥p` | Show the query plan in plain language, what's slow flagged |
| `⌥f` | Reformat the statement — readable, one clause per line |
| `^p` / `^n` | Step through statements you've already run |

## Changing data

| Key | Does |
|---|---|
| `u` | Edit a cell (shows the `UPDATE` first) |
| `n` | Insert a row via a generated form |
| `x` | Delete the selected row (shows the `DELETE` first) |

## Everything else, only when you need it

`c`/`D` create/delete a database or user (type-name-to-confirm) · `g` grant ·
`B` backup/restore · `S` compare schemas · `M` migration status · `R` reload
the current list · `^T`/`^W` open/close a tab · `^C` quit from anywhere.

Full detail on any of these: [`LEARN.md`](LEARN.md).
