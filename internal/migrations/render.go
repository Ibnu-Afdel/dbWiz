package migrations

import (
	"fmt"
	"strings"
)

// Render writes a status as a plain-text report.
//
// Unstyled ASCII, like the query plan's report and the schema diff before it, so
// the same text serves the terminal, a pipe and a pasted bug report. The closing
// paragraph is not decoration: "24 applied" reads as "up to date" to exactly the
// beginner this feature is for, and DBWiz has no way to know whether it is.
func Render(s Status) string {
	var b strings.Builder

	where := s.Database
	if where == "" {
		where = "this database"
	}
	fmt.Fprintf(&b, "Migrations in %s (%s)\n", where, s.Engine)

	if len(s.Ledgers) == 0 {
		b.WriteString(`
No migration ledger found.

DBWiz looks for the tables the common migration tools leave behind — migrations,
schema_migrations, django_migrations, flyway_schema_history and a dozen more. This
database has none of them, so either nothing manages its schema, or the tool that
does keeps its state somewhere DBWiz can't see from a connection.
`)
		return b.String()
	}

	for _, l := range s.Ledgers {
		b.WriteString("\n")
		b.WriteString(renderLedger(l))
	}

	b.WriteString(`
DBWiz reads the database, so this is what has been applied to it. It cannot tell
you what is still pending: that lives in your migration files, which DBWiz never
looks at.
`)
	return b.String()
}

// renderLedger writes one ledger's heading, rows and footnotes.
func renderLedger(l Ledger) string {
	var b strings.Builder

	switch {
	case l.Applied == 0:
		fmt.Fprintf(&b, "%s · %s · empty\n", l.Label(), l.Ref())
	case l.Pointer:
		fmt.Fprintf(&b, "%s · %s · at version %s\n", l.Label(), l.Ref(), pointerVersion(l))
	default:
		fmt.Fprintf(&b, "%s · %s · %s\n", l.Label(), l.Ref(), plural(l.Applied, "migration applied", "migrations applied"))
	}

	if l.Guessed {
		b.WriteString("\n  This table looks like a migration ledger, but its columns match no layout\n" +
			"  DBWiz knows, so it is read as found rather than interpreted.\n")
	}

	if l.Applied == 0 {
		b.WriteString("\n  The table is there but has no rows — the tool has been set up, and nothing\n" +
			"  has been migrated yet.\n")
		return b.String()
	}

	// A pointer ledger's heading already carries its one version, so listing it
	// again would say the same thing twice; only its trouble is left to report.
	if l.Pointer {
		for _, e := range l.Latest {
			if e.Trouble != "" {
				fmt.Fprintf(&b, "\n  ! %s\n", e.Trouble)
			}
		}
		return b.String()
	}
	if len(l.Latest) > 0 {
		b.WriteString("\n")
		b.WriteString(renderEntries(l.Latest))
	}

	if !l.Pointer && l.Applied > len(l.Latest) {
		fmt.Fprintf(&b, "\n  Showing the %d most recent of %d.\n", len(l.Latest), l.Applied)
	}
	return b.String()
}

// renderEntries writes the rows: identifier, optional name, optional timestamp,
// each column padded to its widest entry so the block reads as a table without
// drawing one.
func renderEntries(entries []Entry) string {
	idW, nameW := 0, 0
	for _, e := range entries {
		idW = max(idW, len(e.ID))
		nameW = max(nameW, len(e.Name))
	}

	var b strings.Builder
	for _, e := range entries {
		line := "  " + pad(e.ID, idW)
		if nameW > 0 {
			line += "  " + pad(e.Name, nameW)
		}
		if e.At != "" {
			line += "  " + e.At
		}
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteString("\n")
		if e.Trouble != "" {
			fmt.Fprintf(&b, "  ! %s\n", e.Trouble)
		}
	}
	return b.String()
}

// pointerVersion is the single version a pointer-style ledger records.
func pointerVersion(l Ledger) string {
	if len(l.Latest) == 0 {
		return "unknown"
	}
	v := l.Latest[0].ID
	if v == "" {
		return "unknown"
	}
	return v
}

// pad right-pads s to width.
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// plural picks the singular or plural phrase for n and prints it with the count.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
