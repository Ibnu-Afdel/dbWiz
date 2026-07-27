package docker

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestClassifyRun(t *testing.T) {
	deadlineCtx, cancel := context.WithDeadline(context.Background(), timePast())
	defer cancel()

	tests := []struct {
		name   string
		ctx    context.Context
		err    error
		stderr string
		want   DockerErrKind
	}{
		{"nil error", context.Background(), nil, "", DockerErrInternal}, // classifyRun returns nil; handled separately
		{"binary missing", context.Background(), exec.ErrNotFound, "", DockerErrBinaryMissing},
		{"deadline", deadlineCtx, errors.New("signal: killed"), "", DockerErrTimeout},
		{"socket permission", context.Background(), errors.New("exit 1"),
			"permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock", DockerErrSocketPermission},
		{"daemon down", context.Background(), errors.New("exit 1"),
			"Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?", DockerErrDaemonDown},
		{"unclassified", context.Background(), errors.New("weird"), "some other stderr", DockerErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyRun("detect", tt.ctx, tt.err, []byte(tt.stderr))
			if tt.err == nil {
				if got != nil {
					t.Fatalf("classifyRun(nil) = %+v, want nil", got)
				}
				return
			}
			if got == nil || got.Kind != tt.want {
				t.Fatalf("classifyRun kind = %v, want %v", kindOf(got), tt.want)
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("DockerError should unwrap to the underlying error")
			}
		})
	}
}

func TestClassifyStart(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   DockerErrKind
	}{
		{"port allocated", "Error response from daemon: driver failed programming external connectivity: Bind for 0.0.0.0:3306 failed: port is already allocated", DockerErrPortConflict},
		{"address in use", "listen tcp 0.0.0.0:3306: bind: address already in use", DockerErrPortConflict},
		{"generic failure", "Error: No such container: nope", DockerErrStartFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyStart("mariadb11", 3306, context.Background(), errors.New("exit 1"), []byte(tt.stderr))
			if got == nil || got.Kind != tt.want {
				t.Fatalf("classifyStart kind = %v, want %v", kindOf(got), tt.want)
			}
		})
	}
	if got := classifyStart("x", 3306, context.Background(), nil, nil); got != nil {
		t.Fatalf("classifyStart(nil) = %+v, want nil", got)
	}
}

// TestConstructors ensures every typed kind is constructible and carries the
// user-facing text the UI renders verbatim.
func TestConstructors(t *testing.T) {
	errs := []*DockerError{
		errInternal("detect", errors.New("x")),
		errBinaryMissing(errors.New("x")),
		errDaemonDown(errors.New("x")),
		errSocketPermission(errors.New("x")),
		errNoContainers(false),
		errNoContainers(true),
		errUnreachable("postgres18", 5432),
		errPortConflict("mariadb11", 3306, errors.New("x")),
		errStartFailed("mysql8", errors.New("x")),
		errTimeout("detect", errors.New("x")),
	}
	for _, e := range errs {
		if e.Title == "" || e.Detail == "" || e.Hint == "" {
			t.Errorf("kind %d missing user-facing text: %+v", e.Kind, e)
		}
	}
	// NoContainers hint differs on Omarchy.
	if errNoContainers(true).Hint == errNoContainers(false).Hint {
		t.Error("expected a different empty-state hint on Omarchy")
	}
}

func kindOf(e *DockerError) any {
	if e == nil {
		return "nil"
	}
	return e.Kind
}
