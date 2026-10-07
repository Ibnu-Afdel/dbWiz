package docker

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/debuglog"
)

// backupTimeout bounds a dump or restore. It is generous — a real database can
// take minutes to dump — but bounded so a wedged process can't hang forever.
const backupTimeout = 30 * time.Minute

// ExecOptions describes a command to run inside a running container via
// `docker exec`, streaming data through it. It backs dump/restore (v2 4.4), the
// one place DBWiz shells into a container: the database's own pg_dump/mysqldump
// and psql/mysql are the right tools and already live there.
//
// Env carries KEY=VALUE pairs the inner command needs — notably a password. They
// are set in this process's environment and forwarded to docker by name only
// (`-e KEY`, no value), so a secret never appears in the argv and can't be seen
// in `ps` or the debug log. Stdin (nil for none) feeds the command; Stdout
// receives its output, streamed rather than buffered so large dumps don't sit in
// memory.
type ExecOptions struct {
	Container string
	Env       []string
	Stdin     io.Reader
	Stdout    io.Writer
	Args      []string
}

// Exec runs the command inside the container, streaming stdin/stdout. A failure
// of the inner command surfaces as an Internal DockerError carrying its stderr,
// so the real reason (e.g. "database does not exist") reaches the user; a missing
// docker binary or dead daemon classifies the same as everywhere else.
func Exec(ctx context.Context, opts ExecOptions) error {
	ctx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()

	if _, err := exec.LookPath("docker"); err != nil {
		return errBinaryMissing(err)
	}

	args := dockerExecArgs(opts.Container, envNames(opts.Env), opts.Stdin != nil, opts.Args)
	debuglog.LogExec("docker", args) // args hold only env *names*, never values
	cmd := dockerCommand(ctx, envNames(opts.Env), args...)
	cmd.Env = append(os.Environ(), opts.Env...)
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return classifyExec(opts.Container, ctx, err, errb.Bytes())
	}
	return nil
}

// dockerExecArgs builds the `docker exec` argv: -i when a stdin stream is
// present, then `-e NAME` (name only — the value is passed through the process
// environment) for each forwarded var, then the container and the command to run
// inside it.
func dockerExecArgs(container string, envNames []string, wantStdin bool, command []string) []string {
	args := make([]string, 0, 3+2*len(envNames)+len(command))
	args = append(args, "exec")
	if wantStdin {
		args = append(args, "-i")
	}
	for _, n := range envNames {
		args = append(args, "-e", n)
	}
	args = append(args, container)
	return append(args, command...)
}

// envNames extracts the KEY from each KEY=VALUE pair, the form `docker exec -e`
// uses to forward a variable from the caller's environment without repeating its
// value on the command line.
func envNames(env []string) []string {
	names := make([]string, 0, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			names = append(names, kv[:i])
		} else if kv != "" {
			names = append(names, kv)
		}
	}
	return names
}

// classifyExec maps a failed `docker exec` to a typed error. Daemon/binary/
// timeout cases classify like every other docker call; a generic failure of the
// inner command surfaces its stderr as the detail so the real reason reaches the
// user instead of a bare exit code.
func classifyExec(container string, ctx context.Context, err error, stderr []byte) *DockerError {
	de := classifyRun("exec", ctx, err, stderr)
	if de == nil || de.Kind != DockerErrInternal {
		return de
	}
	detail := strings.TrimSpace(string(stderr))
	if detail == "" {
		detail = err.Error()
	}
	return &DockerError{
		Kind:   DockerErrInternal,
		Title:  "A command in container " + container + " failed",
		Detail: detail,
		Hint:   "check the database name and that the dump/restore tool exists in the container",
		Err:    err,
	}
}
