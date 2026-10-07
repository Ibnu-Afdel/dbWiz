package docker

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/debuglog"
	"github.com/Ibnu-Afdel/dbwiz/internal/omarchy"
)

// Timeouts bound every docker subprocess so a hung docker binary can never
// freeze the app. Detection reads are quick; starting a container and waiting
// for it to accept connections is allowed longer.
const (
	detectTimeout = 5 * time.Second
	startTimeout  = 15 * time.Second
	// remoteOpTimeout bounds a docker command run against a remote daemon over
	// SSH — longer than the local read because each call pays an SSH handshake.
	remoteOpTimeout = 25 * time.Second
)

// dockerRunner runs a docker subcommand and returns its captured stdout and
// stderr plus the process error. It is an injection seam: production uses
// dockerCLI, tests supply canned output without needing Docker installed.
type dockerRunner func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// dockerCLI is the default runner: it shells out to the local docker binary.
func dockerCLI(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
	return runDocker(ctx, nil, args...)
}

// remoteCLI is a runner pointed at a remote Docker daemon over SSH via
// DOCKER_HOST=ssh://user@host:port, so the same ps/inspect commands run on the
// server (v3 3.3). The docker CLI performs the SSH itself.
func remoteCLI(dockerHost string) dockerRunner {
	return func(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
		return runDocker(ctx, []string{"DOCKER_HOST=" + dockerHost}, args...)
	}
}

// runDocker shells out to the docker binary, optionally with extra environment
// (the remote runner sets DOCKER_HOST). It pre-checks PATH so a missing binary
// surfaces as exec.ErrNotFound, which classifyRun turns into a BinaryMissing
// error.
func runDocker(ctx context.Context, extraEnv []string, args ...string) (stdout, stderr []byte, err error) {
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		return nil, nil, lookErr
	}
	debuglog.LogExec("docker", args)
	cmd := dockerCommand(ctx, envKeys(extraEnv), args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

// OnOmarchy reports whether this looks like an Omarchy machine. The tui uses it
// to tailor empty-state hints and label stock containers; detection uses the
// unexported form internally.
func OnOmarchy() bool { return onOmarchy() }

// onOmarchy is a seam over omarchy.Detect, which lets detection attach Omarchy's
// known default credentials to its stock containers.
var onOmarchy = omarchy.Detect

// envKeys returns the variable names from KEY=VALUE pairs, for forwarding
// through sudo by name.
func envKeys(env []string) []string {
	keys := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		keys = append(keys, k)
	}
	return keys
}
