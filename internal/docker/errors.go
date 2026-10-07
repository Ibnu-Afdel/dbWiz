package docker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// DockerErrKind classifies a docker-side failure into a stable set the UI can
// switch on to render the right [Error] screen. Raw CLI/stderr strings are
// matched exactly once — inside this package's classify functions — never in
// the tui layer.
type DockerErrKind int

const (
	DockerErrInternal         DockerErrKind = iota // unclassified / unexpected
	DockerErrBinaryMissing                         // docker not installed / not on PATH
	DockerErrDaemonDown                            // daemon not running
	DockerErrSocketPermission                      // permission denied on docker socket
	DockerErrNoContainers                          // no database containers found
	DockerErrUnreachable                           // container up but port not reachable
	DockerErrPortConflict                          // host port already in use
	DockerErrStartFailed                           // docker start returned an error
	DockerErrTimeout                               // operation exceeded its deadline
)

// DockerError is the typed error every docker operation returns on failure.
// Each kind carries plain-language text the UI shows verbatim: a Title, a
// Detail, and a Hint suggesting the next action/key. Err wraps the underlying
// error for logging.
type DockerError struct {
	Kind   DockerErrKind
	Title  string
	Detail string
	Hint   string
	Err    error
}

// Error implements the error interface.
func (e *DockerError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Title, e.Err)
	}
	return e.Title
}

// Unwrap exposes the wrapped error for errors.Is/As.
func (e *DockerError) Unwrap() error { return e.Err }

// classifyRun maps a failed docker subprocess into a typed DockerError. It is
// the single place raw exec/stderr strings are inspected: BinaryMissing from a
// missing PATH entry, Timeout from a deadline, SocketPermission and DaemonDown
// from well-known daemon stderr, everything else Internal. op names the
// operation for the wrapped error ("detect", "inspect", "start").
func classifyRun(op string, ctx context.Context, err error, stderr []byte) *DockerError {
	if err == nil {
		return nil
	}
	// A deadline is reported on the context; the process error itself is often
	// just "signal: killed", so check the context first.
	if ctx != nil && ctx.Err() == context.DeadlineExceeded {
		return errTimeout(op, err)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return errBinaryMissing(err)
	}
	s := string(stderr)
	switch {
	case sudoNeedsPassword(s):
		return errSudoExpired(err)
	case strings.Contains(s, "permission denied") && strings.Contains(s, "docker.sock"):
		return errSocketPermission(err)
	case strings.Contains(s, "Cannot connect to the Docker daemon"),
		strings.Contains(s, "Is the docker daemon running"):
		return errDaemonDown(err)
	}
	return errInternal(op, err)
}

// classifyStart maps a failed `docker start` into StartFailed or, when the host
// port is already taken (the classic mysql8-vs-mariadb11 3306 clash), the more
// specific PortConflict.
func classifyStart(name string, port int, ctx context.Context, err error, stderr []byte) *DockerError {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() == context.DeadlineExceeded {
		return errTimeout("start", err)
	}
	s := string(stderr)
	switch {
	case strings.Contains(s, "port is already allocated"),
		strings.Contains(s, "address already in use"),
		strings.Contains(s, "Bind for") && strings.Contains(s, "failed"):
		return errPortConflict(name, port, err)
	}
	return errStartFailed(name, err)
}

func errInternal(op string, err error) *DockerError {
	return &DockerError{
		Kind:   DockerErrInternal,
		Title:  "Couldn't talk to Docker",
		Detail: fmt.Sprintf("An unexpected error occurred while trying to %s containers.", op),
		Hint:   "press [r] to retry",
		Err:    err,
	}
}

func errBinaryMissing(err error) *DockerError {
	return &DockerError{
		Kind:   DockerErrBinaryMissing,
		Title:  "Docker isn't installed",
		Detail: "The docker command wasn't found on your PATH, so containers can't be discovered.",
		Hint:   "install Docker, or open a SQLite file instead",
		Err:    err,
	}
}

func errDaemonDown(err error) *DockerError {
	return &DockerError{
		Kind:   DockerErrDaemonDown,
		Title:  "Docker isn't running",
		Detail: "The docker daemon isn't responding. It may be stopped.",
		Hint:   "start it with: sudo systemctl start docker",
		Err:    err,
	}
}

func errSocketPermission(err error) *DockerError {
	return &DockerError{
		Kind:   DockerErrSocketPermission,
		Title:  "No permission to reach Docker",
		Detail: "Your user can't access the Docker socket, so containers can't be listed.",
		Hint:   permissionHint(),
		Err:    err,
	}
}

// errSudoExpired is the permission error in sudo mode: sudo's cached
// credentials ran out (or were never cached), and DBWiz won't prompt behind the
// TUI's back. It shares the SocketPermission kind so the same [s] key
// re-authorizes.
func errSudoExpired(err error) *DockerError {
	return &DockerError{
		Kind:   DockerErrSocketPermission,
		Title:  "Sudo needs your password again",
		Detail: "DBWiz reaches Docker through sudo this session, and sudo's cached password has expired.",
		Hint:   "press [s] to enter it again",
		Err:    err,
	}
}

// ErrNoContainers is the empty-state error the tui shows when Detect succeeds
// but returns no database containers (docker itself didn't fail, so Detect
// reports (nil, nil)). It resolves the Omarchy-specific hint here so callers
// don't have to.
func ErrNoContainers() *DockerError { return errNoContainers(onOmarchy()) }

// errNoContainers is produced by the caller (not classifyRun) when docker
// succeeded but found no database containers. The hint differs on Omarchy,
// where a single script provisions them.
func errNoContainers(onOmarchy bool) *DockerError {
	hint := "press [s] to set one up, or: docker run -d -p 5432:5432 -e POSTGRES_HOST_AUTH_METHOD=trust postgres"
	if onOmarchy {
		hint = "press [s] to set one up, or use Omarchy's installer: omarchy install docker dbs"
	}
	return &DockerError{
		Kind:   DockerErrNoContainers,
		Title:  "No database containers found",
		Detail: "Docker is running but none of your containers look like a SQL database.",
		Hint:   hint,
	}
}

func errUnreachable(name string, port int) *DockerError {
	return &DockerError{
		Kind:   DockerErrUnreachable,
		Title:  fmt.Sprintf("%q is running but unreachable", name),
		Detail: fmt.Sprintf("The container is Up but nothing is accepting connections on localhost:%d yet.", port),
		Hint:   "give it a moment and press [r] to retry, or check the container logs",
	}
}

func errPortConflict(name string, port int, err error) *DockerError {
	return &DockerError{
		Kind:   DockerErrPortConflict,
		Title:  fmt.Sprintf("Port %d is already in use", port),
		Detail: fmt.Sprintf("Can't start %q because another container already holds host port %d (MySQL and MariaDB both want 3306 — only one can run at a time).", name, port),
		Hint:   "stop the container using that port, then start this one",
		Err:    err,
	}
}

func errStartFailed(name string, err error) *DockerError {
	return &DockerError{
		Kind:   DockerErrStartFailed,
		Title:  fmt.Sprintf("Couldn't start %q", name),
		Detail: "docker start returned an error.",
		Hint:   "check the container logs with: docker logs " + name,
		Err:    err,
	}
}

func errTimeout(op string, err error) *DockerError {
	return &DockerError{
		Kind:   DockerErrTimeout,
		Title:  "Docker timed out",
		Detail: fmt.Sprintf("The %s operation took too long and was cancelled.", op),
		Hint:   "press [r] to retry",
		Err:    err,
	}
}
