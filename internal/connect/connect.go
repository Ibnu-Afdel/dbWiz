// Package connect resolves how to reach a detected database container: it runs
// the non-interactive rungs of DBWiz's credential ladder (D11) — Omarchy
// defaults already on the container → docker inspect env → a cwd .env /
// DATABASE_URL → a cwd docker-compose.yml — and backfills engine defaults, so a
// container name becomes a concrete db.Target without prompting.
//
// It is the one place that ladder lives. Both the TUI (which then adds a masked
// password prompt as the final, interactive rung) and the scripting subcommands
// (which stay non-interactive) build on it, so a container connects the same way
// however DBWiz is driven.
//
// It imports db and docker but never tui — the dependency direction is
// tui/cmd → connect → {db, docker}.
package connect

import (
	"context"
	"os"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// Target builds the connection target for a container from the non-interactive
// rungs of the credential ladder: Omarchy defaults (already on the container
// when detected) → docker inspect env → the cwd .env file → a cwd
// docker-compose.yml. Later rungs only fill gaps left by earlier ones. Engine
// defaults backfill a user/maintenance-db when nothing recovered one.
//
// The returned target may still lack a password (nothing recovered one); callers
// decide what to do then — the TUI prompts, the CLI can accept an override or
// fail with a clear auth error.
func Target(ctx context.Context, c docker.Container, kind db.Kind) db.Target {
	if ctx == nil {
		ctx = context.Background()
	}
	creds := c.Creds
	port := c.HostPort

	// Rung 2: docker inspect env — recovers a port and/or creds we don't have.
	if creds.User == "" || creds.Password == "" || port == 0 {
		if p, ic, err := docker.Inspect(ctx, c.Name, c.Engine); err == nil {
			if port == 0 {
				port = p
			}
			creds = mergeCreds(creds, ic)
		}
	}

	// Rung 3: a .env / DATABASE_URL in the working directory.
	if creds.User == "" || creds.Password == "" {
		if hint, ok := docker.ReadEnvFile(cwd()); ok {
			creds = mergeCreds(creds, docker.Creds{
				User:     hint.User,
				Password: hint.Password,
				Database: hint.Database,
			})
		}
	}

	// Rung 4: a docker-compose.yml in the working directory (v2 3.4). Same generic,
	// framework-neutral shape as the .env rung, and lowest priority — it only fills
	// gaps the container, inspect, and .env left, and can also supply the host port
	// when nothing else recovered one.
	if creds.User == "" || creds.Password == "" || port == 0 {
		if hint, ok := docker.ReadComposeFile(cwd()); ok {
			creds = mergeCreds(creds, docker.Creds{
				User:     hint.User,
				Password: hint.Password,
				Database: hint.Database,
			})
			if port == 0 {
				port = hint.Port
			}
		}
	}

	target := db.Target{
		Host:     "127.0.0.1",
		Port:     port,
		User:     creds.User,
		Password: creds.Password,
		Database: creds.Database,
	}
	ApplyEngineDefaults(&target, kind)
	return target
}

// KindOf maps a detected docker engine to the db engine kind. SQLite is never
// produced by container detection, so it has no mapping here.
func KindOf(e docker.Engine) (db.Kind, bool) {
	switch e {
	case docker.EnginePostgres:
		return db.KindPostgres, true
	case docker.EngineMySQL:
		return db.KindMySQL, true
	case docker.EngineMariaDB:
		return db.KindMariaDB, true
	}
	return 0, false
}

// mergeCreds fills empty fields of base from extra without overwriting anything
// base already recovered from a higher-priority rung.
func mergeCreds(base, extra docker.Creds) docker.Creds {
	if base.User == "" {
		base.User = extra.User
	}
	if base.Password == "" {
		base.Password = extra.Password
	}
	if base.Database == "" {
		base.Database = extra.Database
	}
	return base
}

// ApplyEngineDefaults backfills the conventional admin user and maintenance
// database when the ladder recovered none, so an Omarchy-less container can
// still connect without prompting for a username.
func ApplyEngineDefaults(t *db.Target, kind db.Kind) {
	switch kind {
	case db.KindPostgres:
		if t.User == "" {
			t.User = "postgres"
		}
		if t.Database == "" {
			t.Database = "postgres"
		}
	case db.KindMySQL, db.KindMariaDB:
		if t.User == "" {
			t.User = "root"
		}
	}
}

// cwd returns the working directory, or "." when it can't be determined, so the
// .env and compose rungs degrade gracefully.
func cwd() string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return "."
}
