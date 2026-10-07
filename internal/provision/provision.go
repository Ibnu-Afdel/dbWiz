// Package provision creates database containers, not just databases (v3 1.1). It
// mirrors Omarchy's omarchy-install-docker-dbs defaults — same names, images,
// host ports, and dev-friendly auth — so a container
// DBWiz sets up and one Omarchy sets up are interchangeable, and either connects
// with zero configuration.
//
// It differs from Omarchy in one deliberate way: it never prompts for sudo by
// itself. A user whose shell can't reach the Docker socket gets DBWiz's usual
// SocketPermission error with the fix; sudo is used only once the user opts in
// (--sudo, or [s] in the TUI), via the docker package's sudo mode.
//
// It imports docker but never tui/db: the dependency direction is
// tui/cmd → provision → docker.
package provision

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// Spec is the recipe for one provisionable database server: the container to
// create and the dev-auth it's created with. The values match Omarchy's stock
// containers so detection recognizes and zero-config-connects the result.
type Spec struct {
	Key       string        // the `dbwiz setup <key>` keyword
	Engine    docker.Engine // engine detection will classify the result as
	Name      string        // container name (stable, Omarchy-compatible)
	Image     string        // image to pull + run
	HostPort  int           // 127.0.0.1:<HostPort> binding
	Container int           // container-side port the image listens on
	Env       []string      // -e KEY=VALUE dev-auth pairs
	Auth      string        // one-line description of the auth this sets up
}

// specs is the provisionable set. Postgres/MySQL/MariaDB match Omarchy exactly;
// postgis is DBWiz's addition — the same trust-auth Postgres but on a PostGIS
// image so the postgis extension is actually available (v3 1.4). It intentionally
// shares Postgres's 5432, so setting up both is a real port conflict DBWiz
// explains rather than a silent failure.
var specs = []Spec{
	{
		Key: "postgres", Engine: docker.EnginePostgres,
		Name: "postgres18", Image: "postgres:18",
		HostPort: 5432, Container: 5432,
		Env:  []string{"POSTGRES_HOST_AUTH_METHOD=trust"},
		Auth: "trust auth — user postgres, no password",
	},
	{
		Key: "mysql", Engine: docker.EngineMySQL,
		Name: "mysql8", Image: "mysql:8.4",
		HostPort: 3306, Container: 3306,
		Env:  []string{"MYSQL_ROOT_PASSWORD=", "MYSQL_ALLOW_EMPTY_PASSWORD=true"},
		Auth: "root with an empty password",
	},
	{
		Key: "mariadb", Engine: docker.EngineMariaDB,
		Name: "mariadb11", Image: "mariadb:11.8",
		HostPort: 3306, Container: 3306,
		Env:  []string{"MARIADB_ROOT_PASSWORD=", "MARIADB_ALLOW_EMPTY_ROOT_PASSWORD=true"},
		Auth: "root with an empty password",
	},
	{
		Key: "postgis", Engine: docker.EnginePostgres,
		Name: "postgis", Image: "postgis/postgis:18-3.5",
		HostPort: 5432, Container: 5432,
		Env:  []string{"POSTGRES_HOST_AUTH_METHOD=trust"},
		Auth: "trust auth — user postgres, no password; PostGIS preinstalled",
	},
}

// Specs returns the provisionable set in menu order.
func Specs() []Spec {
	out := make([]Spec, len(specs))
	copy(out, specs)
	return out
}

// Lookup finds a spec by its `dbwiz setup <key>` keyword.
func Lookup(key string) (Spec, bool) {
	for _, s := range specs {
		if s.Key == key {
			return s, true
		}
	}
	return Spec{}, false
}

// Keys returns the recognized setup keywords, sorted, for usage and error text.
func Keys() []string {
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i] = s.Key
	}
	sort.Strings(out)
	return out
}

// RunArgs builds the `docker run` argv (without the leading "docker"). It matches
// Omarchy's invocation — detached, restart-unless-stopped, localhost-bound port,
// stable name, dev-auth env — minus sudo.
func (s Spec) RunArgs() []string {
	args := []string{
		"run", "-d",
		"--restart", "unless-stopped",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", s.HostPort, s.Container),
		"--name", s.Name,
	}
	for _, e := range s.Env {
		args = append(args, "-e", e)
	}
	return append(args, s.Image)
}

// ConflictKind classifies why a spec can't be provisioned as-is right now.
type ConflictKind int

const (
	NoConflict ConflictKind = iota
	NameTaken               // a container of this name already exists
	PortTaken               // another container/process holds the host port
)

// Conflict explains a pre-flight clash `docker run` would hit, with a
// plain-language reason so the user can act (start the existing one, stop the
// port holder) instead of decoding a docker error.
type Conflict struct {
	Kind   ConflictKind
	Detail string
}

// reachable is an injection seam over docker.Reachable so the port pre-check is
// testable without a real socket. Production never reassigns it.
var reachable = docker.Reachable

// runContainer is an injection seam over docker.Run so Run is testable without a
// real Docker. Production never reassigns it.
var runContainer = docker.Run

// Check looks for anything that would make provisioning fail: a container of the
// same name already present (NameTaken), another detected database container
// already publishing the host port (PortTaken), or an unrelated process bound to
// it (PortTaken). containers is a fresh Detect scan the caller already has. It is
// a courtesy pre-flight — docker.Run still classifies a clash that appears
// between the check and the run.
func Check(ctx context.Context, s Spec, containers []docker.Container) *Conflict {
	for _, c := range containers {
		if c.Name == s.Name {
			state := "stopped"
			if c.State == docker.StateRunning {
				state = "running"
			}
			return &Conflict{Kind: NameTaken, Detail: fmt.Sprintf("a %s container named %q already exists", state, s.Name)}
		}
	}
	for _, c := range containers {
		if c.State == docker.StateRunning && c.HostPort == s.HostPort && c.Name != s.Name {
			return &Conflict{Kind: PortTaken, Detail: fmt.Sprintf("container %q already holds host port %d", c.Name, s.HostPort)}
		}
	}
	if reachable(ctx, s.HostPort) {
		return &Conflict{Kind: PortTaken, Detail: fmt.Sprintf("something is already listening on 127.0.0.1:%d", s.HostPort)}
	}
	return nil
}

// Run pulls the image (if needed) and starts the container. It returns the new
// container id. Errors are the typed docker.DockerError set (port/name clash,
// socket permission, dead daemon), so callers render them the same way as any
// other docker failure.
func Run(ctx context.Context, s Spec) (string, error) {
	return runContainer(ctx, s.RunArgs())
}

// readyPoll is how often WaitReady re-checks the port.
const readyPoll = 300 * time.Millisecond

// WaitReady blocks until the container accepts TCP connections on its host port
// or timeout elapses, reporting whether it became ready. A freshly started
// database is Up before it's listening, so callers wait on this rather than
// trusting `docker run` returning.
func WaitReady(ctx context.Context, port int, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if reachable(ctx, port) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(readyPoll):
		}
	}
}
