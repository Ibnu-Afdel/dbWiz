package docker

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestRunContainerSuccess returns the trimmed container id docker prints.
func TestRunContainerSuccess(t *testing.T) {
	var gotArgs []string
	run := func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		gotArgs = args
		return []byte("deadbeef1234\n"), nil, nil
	}
	id, err := runContainer(context.Background(), run, []string{"run", "-d", "--name", "postgres18", "postgres:18"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "deadbeef1234" {
		t.Errorf("id = %q, want trimmed container id", id)
	}
	want := []string{"run", "-d", "--name", "postgres18", "postgres:18"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

// TestRunContainerPortConflict classifies a port-already-allocated failure as
// PortConflict with docker's own reason kept as the detail.
func TestRunContainerPortConflict(t *testing.T) {
	stderr := "docker: Error response from daemon: driver failed programming external connectivity on endpoint mysql8: Bind for 127.0.0.1:3306 failed: port is already allocated."
	_, err := runContainer(context.Background(), fakeRunner("", stderr, errors.New("exit 125")), []string{"run"})
	var de *DockerError
	if !errors.As(err, &de) || de.Kind != DockerErrPortConflict {
		t.Fatalf("want PortConflict, got %v", err)
	}
	if de.Detail == "" {
		t.Errorf("expected docker's stderr preserved as detail")
	}
}

// TestRunContainerNameConflict classifies a duplicate-name failure clearly.
func TestRunContainerNameConflict(t *testing.T) {
	stderr := `docker: Error response from daemon: Conflict. The container name "/postgres18" is already in use by container "abc".`
	_, err := runContainer(context.Background(), fakeRunner("", stderr, errors.New("exit 125")), []string{"run"})
	var de *DockerError
	if !errors.As(err, &de) || de.Kind != DockerErrStartFailed {
		t.Fatalf("want StartFailed for a name clash, got %v", err)
	}
}

// TestRunContainerSocketPermission defers to the shared classifier so the
// no-sudo/docker-group case reads the same as everywhere else.
func TestRunContainerSocketPermission(t *testing.T) {
	stderr := "permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock"
	_, err := runContainer(context.Background(), fakeRunner("", stderr, errors.New("exit 1")), []string{"run"})
	var de *DockerError
	if !errors.As(err, &de) || de.Kind != DockerErrSocketPermission {
		t.Fatalf("want SocketPermission, got %v", err)
	}
}
