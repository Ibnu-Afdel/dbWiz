package docker

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/debuglog"
)

// Timeouts bound every docker subprocess so a hung docker binary can never
// freeze the app. Detection reads are quick; starting a container and waiting
// for it to accept connections is allowed longer.
const (
	detectTimeout = 5 * time.Second
	startTimeout  = 15 * time.Second
)

// dockerRunner runs a docker subcommand and returns its captured stdout and
// stderr plus the process error. It is an injection seam: production uses
// dockerCLI, tests supply canned output without needing Docker installed.
type dockerRunner func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// dockerCLI is the default runner: it shells out to the docker binary. It
// pre-checks PATH so a missing binary surfaces as exec.ErrNotFound, which
// classifyRun turns into a BinaryMissing error.
func dockerCLI(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
	if _, lookErr := exec.LookPath("docker"); lookErr != nil {
		return nil, nil, lookErr
	}
	debuglog.LogExec("docker", args)
	cmd := exec.CommandContext(ctx, "docker", args...)
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

// onOmarchy reports whether this looks like an Omarchy machine, which lets
// detection attach Omarchy's known default credentials to its stock containers.
func onOmarchy() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return omarchyDirExists(home)
}

// omarchyDirExists is the testable core of onOmarchy: Omarchy installs its
// payload under ~/.local/share/omarchy.
func omarchyDirExists(home string) bool {
	info, err := os.Stat(filepath.Join(home, ".local", "share", "omarchy"))
	return err == nil && info.IsDir()
}
