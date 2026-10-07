package docker

import (
	"context"
	"strings"
	"time"
)

// provisionTimeout bounds a `docker run`, which on first use pulls the image —
// potentially hundreds of megabytes — so it is far more generous than the quick
// detection reads. It is still bounded so a wedged pull can't hang forever.
const provisionTimeout = 10 * time.Minute

// Run executes `docker <args...>` — in practice a `docker run …` that provisions
// a new database container (v3 1.1) — and returns the trimmed stdout, which for
// `docker run -d` is the new container's id.
//
// Failures classify into the same typed DockerError set as the rest of the
// package: a host-port clash is PortConflict, a socket-permission denial (the
// user isn't in the docker group and sudo mode is off) is SocketPermission, a
// dead daemon or missing binary surface as themselves. The raw docker stderr is
// preserved as the Detail so the real reason reaches the user.
func Run(ctx context.Context, args []string) (string, error) {
	return runContainer(ctx, dockerCLI, args)
}

func runContainer(ctx context.Context, run dockerRunner, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, provisionTimeout)
	defer cancel()

	stdout, stderr, err := run(ctx, args...)
	if err != nil {
		return "", classifyProvision(ctx, err, stderr)
	}
	return strings.TrimSpace(string(stdout)), nil
}

// classifyProvision maps a failed `docker run` into a typed DockerError. Beyond
// the shared daemon/binary/permission/timeout cases it recognizes the two
// clashes provisioning hits: a host port already bound (PortConflict) and a
// container name already taken (StartFailed with a name-clash message).
func classifyProvision(ctx context.Context, err error, stderr []byte) *DockerError {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() == context.DeadlineExceeded {
		return errTimeout("run", err)
	}
	s := string(stderr)
	switch {
	case strings.Contains(s, "port is already allocated"),
		strings.Contains(s, "address already in use"),
		strings.Contains(s, "Bind for") && strings.Contains(s, "failed"):
		return errRunPortConflict(err, s)
	case strings.Contains(s, "is already in use by container"),
		strings.Contains(s, "Conflict. The container name"):
		return errNameConflict(err, s)
	}
	// Otherwise defer to the shared classifier (binary/daemon/permission/internal),
	// keeping docker's own stderr as the detail for an unexpected failure.
	de := classifyRun("run", ctx, err, stderr)
	if de.Kind == DockerErrInternal {
		if detail := firstLine(s); detail != "" {
			de.Detail = detail
		}
	}
	return de
}

func errRunPortConflict(err error, stderr string) *DockerError {
	return &DockerError{
		Kind:   DockerErrPortConflict,
		Title:  "That host port is already in use",
		Detail: firstLine(stderr),
		Hint:   "stop whatever holds the port (another database container?), then retry",
		Err:    err,
	}
}

func errNameConflict(err error, stderr string) *DockerError {
	return &DockerError{
		Kind:   DockerErrStartFailed,
		Title:  "A container with that name already exists",
		Detail: firstLine(stderr),
		Hint:   "it may already be set up — start it from the home screen, or remove it: docker rm -f <name>",
		Err:    err,
	}
}

// firstLine returns the first non-empty, trimmed line of docker's stderr — the
// human-readable reason, without the multi-line noise docker sometimes appends.
func firstLine(s string) string {
	for ln := range strings.SplitSeq(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			return ln
		}
	}
	return ""
}
