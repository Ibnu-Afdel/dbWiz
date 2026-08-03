package migrations

import "strings"

// Convention is one migration tool's published table layout: the table it
// creates, the columns that prove it is that tool's table, and which of those
// columns hold the parts a report needs.
//
// Everything here is written lower-case and matched case-insensitively. The names
// that reach SQL are always the server's own spelling, resolved out of
// DescribeTable — SequelizeMeta and __EFMigrationsHistory keep their case on
// Postgres, so the catalogue is a lookup key and never a source of identifiers.
type Convention struct {
	// Tool is the name shown in the report ("Laravel", "Flyway").
	Tool string
	// Table is the conventional table name.
	Table string
	// Require lists columns that must all be present for this entry to match.
	Require []string
	// Absent lists columns that must not be present. This is what separates the
	// three unrelated tools that all call their table schema_migrations.
	Absent []string
	// Alongside names another table that must exist in the same database. Rails
	// and dbmate write an identical schema_migrations; ar_internal_metadata is the
	// only thing in the database that tells them apart.
	Alongside string

	// ID is the column holding the migration identifier — a version string, a
	// serial, a timestamped filename.
	ID string
	// Name is an optional column holding a human name, when the identifier isn't
	// one already.
	Name string
	// At is an optional column holding when the migration was applied.
	At string
	// Order lists the columns to sort by, most significant first. The read takes
	// the largest ones, so this must sort oldest-to-newest.
	Order []string

	// Pointer marks a ledger that records the single version the database is *at*
	// rather than a history of everything applied. "1 migration applied" would be
	// the wrong sentence for those.
	Pointer bool

	// Flags are the per-row states worth alarming about — a migration that
	// started and never finished, or that the tool has marked dirty.
	Flags []Flag
}

// Flag detects a row that didn't apply cleanly. Column is the column to read;
// Bad decides; Says is the sentence shown against that row.
type Flag struct {
	Column string
	Bad    func(cell any) bool
	Says   string
}

// catalog is every layout DBWiz recognises, ordered most-specific first: the
// first entry that matches a table wins it, so the entries that pin a shared
// table name down by an extra column come before the bare one.
//
// Adding a tool here is data, not code — which is the point. DBWiz doesn't
// integrate with any of these; it recognises the table they leave behind, the
// same way the credential ladder recognises a .env file (D8).
var catalog = []Convention{
	// --- migrations ---------------------------------------------------------
	{
		Tool: "Laravel", Table: "migrations",
		Require: []string{"migration", "batch"},
		ID:      "id", Name: "migration", Order: []string{"id"},
	},
	{
		Tool: "TypeORM", Table: "migrations",
		Require: []string{"timestamp", "name"},
		ID:      "id", Name: "name", Order: []string{"timestamp"},
	},

	// --- schema_migrations, the crowded one ---------------------------------
	{
		Tool: "golang-migrate", Table: "schema_migrations",
		Require: []string{"version", "dirty"},
		ID:      "version", Order: []string{"version"},
		Pointer: true,
		Flags: []Flag{{
			Column: "dirty", Bad: truthy,
			Says: "marked dirty — a migration failed part-way and golang-migrate will refuse to run until it's resolved",
		}},
	},
	{
		Tool: "Ecto", Table: "schema_migrations",
		Require: []string{"version", "inserted_at"},
		ID:      "version", At: "inserted_at", Order: []string{"version"},
	},
	{
		Tool: "Rails", Table: "schema_migrations",
		Require: []string{"version"}, Absent: []string{"dirty", "inserted_at"},
		Alongside: "ar_internal_metadata",
		ID:        "version", Order: []string{"version"},
	},
	{
		Tool: "dbmate", Table: "schema_migrations",
		Require: []string{"version"}, Absent: []string{"dirty", "inserted_at"},
		ID: "version", Order: []string{"version"},
	},

	// --- one tool, one table ------------------------------------------------
	{
		Tool: "Django", Table: "django_migrations",
		Require: []string{"app", "name"},
		ID:      "id", Name: "name", At: "applied", Order: []string{"id"},
	},
	{
		Tool: "Prisma", Table: "_prisma_migrations",
		Require: []string{"migration_name", "started_at"},
		ID:      "migration_name", At: "finished_at", Order: []string{"started_at"},
		Flags: []Flag{
			{Column: "rolled_back_at", Bad: notNull, Says: "rolled back"},
			{Column: "finished_at", Bad: isNull, Says: "started but never finished — it may still be running, or it failed"},
		},
	},
	{
		Tool: "Knex", Table: "knex_migrations",
		Require: []string{"name", "batch"},
		ID:      "id", Name: "name", At: "migration_time", Order: []string{"id"},
	},
	{
		Tool: "Flyway", Table: "flyway_schema_history",
		Require: []string{"installed_rank", "version"},
		ID:      "version", Name: "description", At: "installed_on", Order: []string{"installed_rank"},
		Flags: []Flag{{
			Column: "success", Bad: falsy,
			Says: "recorded as failed — Flyway will not migrate further until this row is repaired",
		}},
	},
	{
		Tool: "Liquibase", Table: "databasechangelog",
		Require: []string{"filename", "orderexecuted"},
		ID:      "id", Name: "filename", At: "dateexecuted", Order: []string{"orderexecuted"},
	},
	{
		Tool: "Alembic", Table: "alembic_version",
		Require: []string{"version_num"},
		ID:      "version_num", Order: []string{"version_num"},
		Pointer: true,
	},
	{
		Tool: "goose", Table: "goose_db_version",
		Require: []string{"version_id", "is_applied"},
		ID:      "version_id", At: "tstamp", Order: []string{"id"},
		Flags: []Flag{{
			Column: "is_applied", Bad: falsy,
			Says: "recorded but not applied",
		}},
	},
	{
		Tool: "Sequelize", Table: "sequelizemeta",
		Require: []string{"name"},
		ID:      "name", Order: []string{"name"},
	},
	{
		Tool: "Entity Framework", Table: "__efmigrationshistory",
		Require: []string{"migrationid"},
		ID:      "migrationid", Order: []string{"migrationid"},
	},
	{
		Tool: "Phinx", Table: "phinxlog",
		Require: []string{"version", "migration_name"},
		ID:      "version", Name: "migration_name", At: "start_time", Order: []string{"version"},
	},
	{
		Tool: "Doctrine", Table: "doctrine_migration_versions",
		Require: []string{"version", "executed_at"},
		ID:      "version", At: "executed_at", Order: []string{"executed_at"},
	},
	{
		Tool: "Atlas", Table: "atlas_schema_revisions",
		Require: []string{"version", "executed_at"},
		ID:      "version", Name: "description", At: "executed_at", Order: []string{"version"},
		Flags: []Flag{{
			Column: "error", Bad: nonEmpty,
			Says: "the revision recorded an error",
		}},
	},
	{
		Tool: "node-pg-migrate", Table: "pgmigrations",
		Require: []string{"name", "run_on"},
		ID:      "id", Name: "name", At: "run_on", Order: []string{"id"},
	},
	{
		Tool: "Diesel", Table: "__diesel_schema_migrations",
		Require: []string{"version", "run_on"},
		ID:      "version", At: "run_on", Order: []string{"version"},
	},
	{
		Tool: "Yii", Table: "migration",
		Require: []string{"version", "apply_time"},
		ID:      "version", At: "apply_time", Order: []string{"apply_time"},
	},
	{
		Tool: "FluentMigrator", Table: "versioninfo",
		Require: []string{"version", "appliedon"},
		ID:      "version", Name: "description", At: "appliedon", Order: []string{"version"},
	},
}

// ledgerWords are the words in a table's name that suggest it records applied
// migrations. They drive the shape pass, which only runs when nothing in the
// catalogue matched.
var ledgerWords = []string{"migration", "migrate", "changelog", "schema_version"}

// idColumns are the columns a hand-rolled ledger plausibly identifies a migration
// by, in the order they'd be preferred.
var idColumns = []string{"version", "version_num", "migration", "migration_name", "name", "filename", "script", "id"}

// timeColumns are exact names worth treating as "when it was applied"; anything
// else is judged by looksLikeTime.
var timeColumns = []string{"applied_at", "executed_at", "inserted_at", "created_at", "run_on", "applied", "executed", "timestamp"}

// matches reports whether c describes cols — every Require present, every Absent
// missing. tables is the rest of the database, for the Alongside test.
func (c Convention) matches(cols []string, tables map[string]bool) bool {
	for _, want := range c.Require {
		if !hasColumn(cols, want) {
			return false
		}
	}
	for _, no := range c.Absent {
		if hasColumn(cols, no) {
			return false
		}
	}
	if c.Alongside != "" && !tables[strings.ToLower(c.Alongside)] {
		return false
	}
	return true
}

// hasColumn reports whether cols contains name, ignoring case.
func hasColumn(cols []string, name string) bool {
	return resolveColumn(cols, name) != ""
}

// resolveColumn returns the server's own spelling of name, or "" when the table
// hasn't got it. Every column name that reaches SQL goes through here first.
func resolveColumn(cols []string, name string) string {
	for _, c := range cols {
		if strings.EqualFold(c, name) {
			return c
		}
	}
	return ""
}

// looksLikeLedger reports whether a table's name suggests it records migrations.
func looksLikeLedger(table string) bool {
	lower := strings.ToLower(table)
	for _, w := range ledgerWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// looksLikeTime reports whether a column name reads as a timestamp. It is
// deliberately loose: the shape pass is already labelled as a guess, and showing
// a column that turns out to be something else is a smaller failure than showing
// a ledger with no dates in it at all.
func looksLikeTime(col string) bool {
	lower := strings.ToLower(col)
	for _, exact := range timeColumns {
		if lower == exact {
			return true
		}
	}
	return strings.HasSuffix(lower, "_at") || strings.HasSuffix(lower, "_on") ||
		strings.HasSuffix(lower, "_time") || strings.HasSuffix(lower, "_date")
}

// shapeConvention builds a best-effort Convention for a table that looks like a
// ledger but matches nothing in the catalogue. Tool is left empty, which is what
// marks the result as a guess all the way through to the report.
func shapeConvention(table string, cols []string) (Convention, bool) {
	c := Convention{Table: table}
	for _, want := range idColumns {
		if real := resolveColumn(cols, want); real != "" {
			c.ID = real
			break
		}
	}
	if c.ID == "" {
		return Convention{}, false // a ledger has to identify its migrations somehow
	}
	for _, col := range cols {
		if looksLikeTime(col) {
			c.At = col
			break
		}
	}
	// Order by the timestamp when there is one, since a hand-rolled identifier
	// need not sort; otherwise the identifier is all there is.
	if c.At != "" {
		c.Order = []string{c.At}
	} else {
		c.Order = []string{c.ID}
	}
	return c, true
}
