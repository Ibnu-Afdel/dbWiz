package docker

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	if !Reachable(context.Background(), port) {
		t.Errorf("open port %d should be reachable", port)
	}

	ln.Close()
	if Reachable(context.Background(), port) {
		t.Errorf("closed port %d should be unreachable", port)
	}
	if Reachable(context.Background(), 0) {
		t.Error("port 0 should be treated as unreachable")
	}
}

func TestStartContainerWaitsForReady(t *testing.T) {
	calls := 0
	ready := func(ctx context.Context, port int) bool {
		calls++
		return calls >= 3 // becomes reachable on the third poll
	}
	err := startContainer(context.Background(), fakeRunner("", "", nil), "postgres18", 5432, ready)
	if err != nil {
		t.Fatalf("expected success once ready, got %v", err)
	}
	if calls < 3 {
		t.Errorf("expected polling until ready, only %d checks", calls)
	}
}

func TestStartContainerPortConflict(t *testing.T) {
	stderr := "Bind for 0.0.0.0:3306 failed: port is already allocated"
	err := startContainer(context.Background(), fakeRunner("", stderr, errors.New("exit 125")), "mariadb11", 3306, neverReady)
	var de *DockerError
	if !errors.As(err, &de) || de.Kind != DockerErrPortConflict {
		t.Fatalf("want PortConflict, got %v", err)
	}
}

func TestStartContainerUnreachableTimesOut(t *testing.T) {
	// Start succeeds but the port never opens; a cancelled context ends the wait
	// as Unreachable rather than hanging.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := startContainer(ctx, fakeRunner("", "", nil), "postgres18", 5432, neverReady)
	var de *DockerError
	if !errors.As(err, &de) || de.Kind != DockerErrUnreachable {
		t.Fatalf("want Unreachable, got %v", err)
	}
}

func TestStartContainerNoPortSkipsWait(t *testing.T) {
	// No known host port: start success is enough, no polling.
	err := startContainer(context.Background(), fakeRunner("", "", nil), "x", 0, neverReady)
	if err != nil {
		t.Fatalf("port 0 should return nil on start success, got %v", err)
	}
}

func neverReady(ctx context.Context, port int) bool { return false }
