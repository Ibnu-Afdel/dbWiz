// Package schema captures the structure of a database and compares two captures
// (v4 Phase 1). It is a consumer of the engine abstraction, not part of it:
// everything here is built on the browse methods v1 Phase 5 already gave every
// engine (ListTables + DescribeTable), so a structural diff works on Postgres,
// MySQL/MariaDB and SQLite without a single new Engine method — and keeps
// working for any engine added later.
//
// Dependency direction is tui/cmd → schema → db. It never imports docker or tui.
//
// A snapshot deliberately records *structure only* — tables and their columns.
// Row counts and other volatile facts are left out so that capturing the same
// database twice produces byte-identical output, which is what makes the diff
// usable in a shell chain or CI.
package schema

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// Column is one column of a captured table. It mirrors db.Column; schema keeps
// its own type so a snapshot is a plain value that can be compared, rendered and
// marshalled without dragging engine types around.
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Key      string `json:"key,omitempty"` // "PRI" etc., engine-specific label
}

// Table is one captured table. Columns stay in the engine's ordinal order —
// column order is part of a table's structure, so it is preserved, not sorted.
type Table struct {
	Schema  string   `json:"schema,omitempty"` // empty for engines without schemas
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

// Qualified is the table's display name: "schema.name" where the engine has
// schemas, plain "name" where it doesn't. It is also the key a diff matches on.
func (t Table) Qualified() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// Column returns the named column, if the table has one.
func (t Table) Column(name string) (Column, bool) {
	for _, c := range t.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

// Snapshot is the captured structure of one database. Label is a human name for
// where it came from ("staging/app") used in reports; Kind is recorded when the
// source could report it, so a report can warn about comparing across engines.
type Snapshot struct {
	Label    string  `json:"label"`
	Database string  `json:"database"`
	Kind     db.Kind `json:"-"`
	Tables   []Table `json:"tables"`
}

// Table returns the table with this qualified name, if present.
func (s Snapshot) Table(qualified string) (Table, bool) {
	for _, t := range s.Tables {
		if t.Qualified() == qualified {
			return t, true
		}
	}
	return Table{}, false
}

// Source is the narrow slice of db.Engine that Capture needs. Taking an
// interface this small (rather than the full Engine) is what lets the package be
// tested against a handful of literal tables instead of a live database — the
// same pattern the scripting commands use in cmd/.
type Source interface {
	ListTables(ctx context.Context, database string) ([]db.Table, error)
	DescribeTable(ctx context.Context, database, table string) ([]db.Column, error)
}

// kinder is the optional half of Source: every real engine implements Kind(), a
// test fake need not.
type kinder interface{ Kind() db.Kind }

// Capture reads the structure of database from src.
//
// Two engine quirks are normalised here so that a snapshot means the same thing
// everywhere:
//
//   - MySQL reports each table's schema as the database itself (they are the same
//     namespace there). That would make every table look "moved" when comparing
//     two MySQL databases, so a schema equal to the database name is dropped.
//     Postgres schemas — public, app, … — are real subdivisions and are kept.
//   - Where a schema survives that rule, the table is described by its qualified
//     "schema.table" name. Postgres resolves that form; the other engines never
//     see it, because their schema was just dropped.
//
// Tables come back sorted by (schema, name) so repeated captures are identical.
func Capture(ctx context.Context, src Source, database string) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tables, err := src.ListTables(ctx, database)
	if err != nil {
		return Snapshot{}, fmt.Errorf("listing tables in %q: %w", database, err)
	}

	snap := Snapshot{Label: database, Database: database}
	if k, ok := src.(kinder); ok {
		snap.Kind = k.Kind()
	}

	for _, t := range tables {
		schemaName := t.Schema
		if schemaName == database {
			schemaName = "" // MySQL: schema and database are one namespace
		}
		ref := t.Name
		if schemaName != "" {
			ref = schemaName + "." + t.Name
		}
		cols, err := src.DescribeTable(ctx, database, ref)
		if err != nil {
			return Snapshot{}, fmt.Errorf("describing table %q in %q: %w", ref, database, err)
		}
		captured := Table{Schema: schemaName, Name: t.Name}
		for _, c := range cols {
			captured.Columns = append(captured.Columns, Column{
				Name:     c.Name,
				Type:     strings.TrimSpace(c.Type),
				Nullable: c.Nullable,
				Key:      c.Key,
			})
		}
		snap.Tables = append(snap.Tables, captured)
	}

	sort.Slice(snap.Tables, func(i, j int) bool {
		a, b := snap.Tables[i], snap.Tables[j]
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}
		return a.Name < b.Name
	})
	return snap, nil
}
