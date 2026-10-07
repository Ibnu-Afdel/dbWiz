package docker

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/unix"

	"github.com/Ibnu-Afdel/dbwiz/internal/omarchy"
)

// Docker access. The daemon's socket is root-owned, and membership in the
// docker group is root-equivalent, so plenty of Linux setups — Omarchy 4 by
// default — deliberately leave the user out of it and reach Docker through sudo
// or polkit instead. DBWiz never escalates on its own: it can only use sudo when
// the user asks (the TUI's [s] key on the permission error, or `--sudo`), and
// then only for its own docker subprocesses, never for the whole app.

// sudoMode routes every docker subprocess through `sudo -n` once the user has
// authorized it for this session. Atomic because detection runs in tea.Cmd
// goroutines.
var sudoMode atomic.Bool

// SetSudo turns sudo routing for docker subprocesses on or off.
func SetSudo(on bool) { sudoMode.Store(on) }

// SudoEnabled reports whether docker subprocesses run through sudo.
func SudoEnabled() bool { return sudoMode.Load() }

// sudoPrompt is the password prompt sudo shows when DBWiz asks it to
// authorize, so the user knows why DBWiz wants it (%u is sudo's username escape).
const sudoPrompt = "[dbwiz] password for %u to reach Docker: "

// AuthorizeSudo returns the `sudo -v` command that caches the user's
// credentials so later `sudo -n docker …` calls run without prompting. The
// caller attaches it to the terminal (tea.ExecProcess in the TUI, the process's
// own stdio on the CLI) and calls SetSudo(true) once it succeeds.
func AuthorizeSudo() *exec.Cmd {
	return exec.Command("sudo", "-v", "-p", sudoPrompt)
}

// SudoAvailable reports whether a sudo binary is on PATH, so the UI only offers
// the sudo route where it can work.
func SudoAvailable() bool {
	_, err := exec.LookPath("sudo")
	return err == nil
}

// dockerCommand builds the docker subprocess, prefixed with `sudo -n` in sudo
// mode. -n makes sudo fail instead of prompting — a prompt would corrupt the
// TUI — and classifyRun turns that failure into "authorize again". keepEnv
// names variables the command needs from this process's environment (dump and
// restore pass a password that way); sudo resets the environment, so they are
// forwarded by name with --preserve-env.
func dockerCommand(ctx context.Context, keepEnv []string, args ...string) *exec.Cmd {
	if !SudoEnabled() {
		return exec.CommandContext(ctx, "docker", args...)
	}
	sudoArgs := []string{"-n"}
	if len(keepEnv) > 0 {
		sudoArgs = append(sudoArgs, "--preserve-env="+strings.Join(keepEnv, ","))
	}
	sudoArgs = append(sudoArgs, "docker")
	return exec.CommandContext(ctx, "sudo", append(sudoArgs, args...)...)
}

// sudoNeedsPassword reports whether stderr is sudo refusing a non-interactive
// run because its cached credentials expired (or were never cached).
func sudoNeedsPassword(stderr string) bool {
	return strings.Contains(stderr, "sudo: a password is required") ||
		strings.Contains(stderr, "sudo: a terminal is required")
}

// socketAccess describes why the Docker socket can't be reached, which picks
// the hint the permission error shows.
type socketAccess int

const (
	accessDenied        socketAccess = iota // not in the docker group at all
	accessPendingReboot                     // in the group, but this session predates it
)

// currentSocketAccess works out which socketAccess applies. The account's
// configured groups (from the group database) can differ from this session's,
// which are fixed at login: after `usermod -aG docker` the account is in the
// group, but nothing started before the next login is.
func currentSocketAccess() socketAccess {
	if inGroupConfigured("docker") && !inGroupSession("docker") {
		return accessPendingReboot
	}
	return accessDenied
}

// inGroupConfigured reports whether the account is a member of the named group
// according to the group database.
func inGroupConfigured(name string) bool {
	g, err := user.LookupGroup(name)
	if err != nil {
		return false
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false
	}
	return slices.Contains(ids, g.Gid)
}

// inGroupSession reports whether this process actually holds the named group.
func inGroupSession(name string) bool {
	g, err := user.LookupGroup(name)
	if err != nil {
		return false
	}
	gids, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, gid := range gids {
		if strconv.Itoa(gid) == g.Gid {
			return true
		}
	}
	return strconv.Itoa(os.Getegid()) == g.Gid
}

// permissionHint is the next step the socket-permission error suggests. It is
// a var so tests can pin it without depending on the machine's groups.
var permissionHint = func() string {
	return permissionHintFor(omarchy.Detect(), currentSocketAccess(), SudoAvailable())
}

// PermissionFix is the durable fix for a socket-permission failure on this
// machine, without the TUI's key hint — the CLI prints it after its own --sudo
// suggestion.
func PermissionFix() string { return permissionFix(omarchy.Detect(), currentSocketAccess()) }

// permissionFix picks the durable fix. On Omarchy it is Omarchy's own opt-in
// rather than a raw usermod, so the root-equivalence warning Omarchy shows is
// kept in the loop.
func permissionFix(onOmarchy bool, access socketAccess) string {
	switch {
	case access == accessPendingReboot:
		return "you're in the docker group, but it applies after a reboot (or a fresh login)"
	case onOmarchy:
		return "Omarchy keeps Docker behind sudo — to drop that, run: omarchy setup security sudoless docker"
	default:
		return "add yourself to the docker group: sudo usermod -aG docker $USER (then re-login)"
	}
}

// permissionHintFor composes the TUI hint: the durable fix for this setup, plus
// the one-key sudo route when sudo exists.
func permissionHintFor(onOmarchy bool, access socketAccess, sudo bool) string {
	fix := permissionFix(onOmarchy, access)
	if sudo {
		return "press [s] to use sudo for this session · or " + fix
	}
	return fix
}

// SocketReachable reports whether this process can use the Docker daemon
// directly: a writable unix socket, or a non-socket DOCKER_HOST (tcp://,
// ssh://) whose access sudo wouldn't change anyway. It mirrors Omarchy's
// omarchy-sudo-docker check, including rootless Docker via DOCKER_HOST.
func SocketReachable() bool {
	host := os.Getenv("DOCKER_HOST")
	path := "/var/run/docker.sock"
	switch {
	case strings.HasPrefix(host, "unix://"):
		path = strings.TrimPrefix(host, "unix://")
	case host != "":
		return true
	}
	return unix.Access(path, unix.W_OK) == nil
}
