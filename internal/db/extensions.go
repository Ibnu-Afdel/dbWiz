package db

import "context"

// Extension is a Postgres extension as the connected server sees it (v3 1.4).
// Availability reflects the image's capability: pg_available_extensions only
// lists extensions whose control files ship in the running image, so postgis
// appears only on a PostGIS image and pgvector only on a pgvector image. That is
// the "image-capability awareness" the feature promises — the server itself is
// the source of truth, no image-name guessing needed.
type Extension struct {
	Name             string
	DefaultVersion   string
	InstalledVersion string // empty when the extension is available but not installed
	Comment          string
}

// Installed reports whether the extension is currently installed in the database.
func (e Extension) Installed() bool { return e.InstalledVersion != "" }

// Extensioner is implemented by engines with a pluggable-extension system — only
// Postgres among the engines DBWiz drives. It's an optional interface callers
// type-assert (eng.(db.Extensioner)) rather than a method on Engine, so MySQL and
// SQLite don't carry a feature they don't have.
type Extensioner interface {
	// ListExtensions returns every extension the image makes available in database,
	// each marked with its installed version (empty if not installed).
	ListExtensions(ctx context.Context, database string) ([]Extension, error)
	// CreateExtension installs name in database (CREATE EXTENSION IF NOT EXISTS).
	// A name the image doesn't ship fails at the server; callers can pre-check
	// against ListExtensions to explain that before trying.
	CreateExtension(ctx context.Context, database, name string) error
}
