package docker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestDockerCommandPlain(t *testing.T) {
	SetSudo(false)
	cmd := dockerCommand(context.Background(), []string{"PGPASSWORD"}, "ps", "-a")
	if got := cmd.Args; !slices.Equal(got, []string{"docker", "ps", "-a"}) {
		t.Fatalf("args = %q", got)
	}
}

func TestDockerCommandSudo(t *testing.T) {
	SetSudo(true)
	t.Cleanup(func() { SetSudo(false) })

	cmd := dockerCommand(context.Background(), nil, "ps", "-a")
	if got := cmd.Args; !slices.Equal(got, []string{"sudo", "-n", "docker", "ps", "-a"}) {
		t.Fatalf("args = %q", got)
	}
	// Secrets are forwarded by name through sudo, never as argv values.
	cmd = dockerCommand(context.Background(), []string{"PGPASSWORD", "MYSQL_PWD"}, "exec", "-e", "PGPASSWORD", "pg", "pg_dump")
	want := []string{"sudo", "-n", "--preserve-env=PGPASSWORD,MYSQL_PWD", "docker", "exec", "-e", "PGPASSWORD", "pg", "pg_dump"}
	if got := cmd.Args; !slices.Equal(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestEnvKeys(t *testing.T) {
	got := envKeys([]string{"DOCKER_HOST=ssh://u@h", "BARE"})
	if !slices.Equal(got, []string{"DOCKER_HOST", "BARE"}) {
		t.Fatalf("envKeys = %q", got)
	}
}

func TestClassifySudoExpired(t *testing.T) {
	got := classifyRun("detect", context.Background(), errors.New("exit 1"),
		[]byte("sudo: a password is required\n"))
	if got.Kind != DockerErrSocketPermission {
		t.Fatalf("kind = %v, want SocketPermission", got.Kind)
	}
	if !strings.Contains(got.Hint, "[s]") {
		t.Fatalf("hint %q doesn't offer the re-authorize key", got.Hint)
	}
}

func TestPermissionHintFor(t *testing.T) {
	omarchyHint := permissionHintFor(true, accessDenied, true)
	if !strings.Contains(omarchyHint, "omarchy setup security sudoless docker") {
		t.Errorf("Omarchy hint should point at Omarchy's own opt-in: %q", omarchyHint)
	}
	if strings.Contains(omarchyHint, "usermod") {
		t.Errorf("Omarchy hint shouldn't bypass Omarchy's warning with a raw usermod: %q", omarchyHint)
	}
	if generic := permissionHintFor(false, accessDenied, true); !strings.Contains(generic, "usermod -aG docker") {
		t.Errorf("generic hint = %q", generic)
	}
	if pending := permissionHintFor(true, accessPendingReboot, true); !strings.Contains(pending, "reboot") {
		t.Errorf("pending hint should say a reboot applies it: %q", pending)
	}
	if noSudo := permissionHintFor(false, accessDenied, false); strings.Contains(noSudo, "[s]") {
		t.Errorf("hint offers [s] without sudo installed: %q", noSudo)
	}
}
