package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Ibnu-Afdel/dbwiz/internal/connect"
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// cliTarget is a resolved scripting target: enough to open a live engine
// (kind + a db.Target with the credential ladder already run) and, for a Docker
// target, the container it came from (so dump/restore can `docker exec` into it).
// A SQLite target has an empty Container and sqlite=true.
type cliTarget struct {
	container docker.Container // zero Name for SQLite
	kind      db.Kind
	target    db.Target
	sqlite    bool
}

// label is a short human name for the target, used in confirmations.
func (t cliTarget) label() string {
	if t.sqlite {
		return t.target.Path
	}
	return t.container.Name
}

// newEngine opens a driver for a kind. It is a package var so tests can stand in
// a fake engine without a live database; production never reassigns it.
var newEngine = db.New

// resolveTarget decides which target a scripting subcommand acts on, mirroring
// how the TUI picks one but without any prompt:
//
//   - an explicit --target names a detected container (a typo self-corrects with
//     the list of names that were found);
//   - otherwise the last target pinned by `dbwiz use` (or last connected in the
//     TUI) is used — a SQLite file directly, a container re-resolved against a
//     fresh scan so a since-removed container fails clearly;
//   - otherwise, if exactly one container is detected, that one (the unambiguous
//     case needs no ceremony);
//   - otherwise it fails asking the user to pick one, so a script never acts on a
//     target the user didn't mean.
func resolveTarget(ctx context.Context, targetFlag string) (cliTarget, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if targetFlag != "" {
		return resolveContainer(ctx, targetFlag)
	}

	last := state.Load().Last
	if last != nil {
		if last.Kind == state.KindSQLite {
			if last.Path == "" {
				return cliTarget{}, errors.New("the last-used SQLite target has no path recorded; pass --target <container> or open it in `dbwiz`")
			}
			return cliTarget{sqlite: true, kind: db.KindSQLite, target: db.Target{Path: last.Path}}, nil
		}
		if last.Container != "" {
			return resolveContainer(ctx, last.Container)
		}
	}

	containers, err := detect(ctx)
	if err != nil {
		return cliTarget{}, fmt.Errorf("couldn't scan Docker for databases: %w", err)
	}
	switch len(containers) {
	case 0:
		return cliTarget{}, errors.New("no target: no database container detected. Start one, or open a SQLite file in `dbwiz`, then retry")
	case 1:
		return fromContainer(ctx, containers[0])
	default:
		return cliTarget{}, fmt.Errorf("multiple database containers detected (%s); pick one with `dbwiz use <name>` or pass --target", strings.Join(names(containers), ", "))
	}
}

// resolveContainer resolves a container by name against a fresh scan.
func resolveContainer(ctx context.Context, name string) (cliTarget, error) {
	containers, err := detect(ctx)
	if err != nil {
		return cliTarget{}, fmt.Errorf("couldn't scan Docker for databases: %w", err)
	}
	for _, c := range containers {
		if c.Name == name {
			return fromContainer(ctx, c)
		}
	}
	return cliTarget{}, unknownContainerError(name, containers)
}

// fromContainer runs the credential ladder for a container and packages the
// result. An engine DBWiz can't drive (should never happen post-detection) is a
// clear error rather than a later cryptic connect failure.
func fromContainer(ctx context.Context, c docker.Container) (cliTarget, error) {
	kind, ok := connect.KindOf(c.Engine)
	if !ok {
		return cliTarget{}, fmt.Errorf("container %q runs an engine DBWiz can't drive from the command line", c.Name)
	}
	if c.State != docker.StateRunning {
		return cliTarget{}, fmt.Errorf("container %q isn't running — start it first (`docker start %s`)", c.Name, c.Name)
	}
	return cliTarget{container: c, kind: kind, target: connect.Target(ctx, c, kind)}, nil
}

// open connects a live engine to the resolved target. password, when non-empty,
// overrides whatever the ladder recovered — the scripting equivalent of the
// masked prompt (a flag, or the DBWIZ_PASSWORD env var). The caller must Close
// the returned engine.
func open(ctx context.Context, t cliTarget, password string) (db.Engine, error) {
	target := t.target
	if password == "" {
		password = os.Getenv("DBWIZ_PASSWORD")
	}
	if password != "" {
		target.Password = password
	}
	engine, err := newEngine(t.kind)
	if err != nil {
		return nil, err
	}
	if err := engine.Connect(ctx, target); err != nil {
		_ = engine.Close()
		var dberr *db.DBError
		if errors.As(err, &dberr) && dberr.Kind == db.DBErrAuthFailed {
			return nil, fmt.Errorf("couldn't authenticate to %s: %w\n"+
				"DBWiz recovers credentials from the container, a cwd .env, or docker-compose.yml; "+
				"pass --password or set DBWIZ_PASSWORD if none apply", t.label(), err)
		}
		return nil, err
	}
	return engine, nil
}

// cliError turns a driver error into a one-line message that keeps the parts a
// DBError carries — Detail and Hint — which its own Error() drops (that method
// serves the TUI, which renders the fields separately). Non-DBError errors pass
// through unchanged.
func cliError(err error) error {
	var dberr *db.DBError
	if !errors.As(err, &dberr) {
		return err
	}
	msg := dberr.Title
	if dberr.Detail != "" {
		msg += ": " + dberr.Detail
	}
	if dberr.Hint != "" {
		msg += " (" + dberr.Hint + ")"
	}
	return errors.New(msg)
}

// names returns the detected container names, sorted, for error messages.
func names(containers []docker.Container) []string {
	out := make([]string, len(containers))
	for i, c := range containers {
		out[i] = c.Name
	}
	sort.Strings(out)
	return out
}
