package docker

import (
	"context"
	"net"
	"strconv"
	"time"
)

// pollInterval is how often StartContainer re-checks the port while waiting for
// a freshly started container to accept connections.
const pollInterval = 250 * time.Millisecond

// probe reports whether a TCP connection to a localhost port succeeds. It is an
// injection seam so start-and-wait can be tested without real sockets.
type probe func(ctx context.Context, port int) bool

// Reachable reports whether something is accepting TCP connections on
// localhost:port. This is the truth of "can I connect" — a container can be Up
// yet not reachable, so state alone is never trusted.
func Reachable(ctx context.Context, port int) bool {
	if port == 0 {
		return false
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// StartContainer runs `docker start <name>` and waits until port accepts
// connections or the start deadline passes. A port already held by another
// container (the mysql8-vs-mariadb11 3306 clash) surfaces as PortConflict;
// exceeding the deadline while Up-but-unreachable surfaces as Timeout.
func StartContainer(ctx context.Context, name string, port int) error {
	return startContainer(ctx, dockerCLI, name, port, Reachable)
}

func startContainer(ctx context.Context, run dockerRunner, name string, port int, ready probe) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	if _, stderr, err := run(ctx, "start", name); err != nil {
		return classifyStart(name, port, ctx, err, stderr)
	}

	// Without a known host port we can't verify readiness; the start command
	// itself succeeded, so report success.
	if port == 0 {
		return nil
	}

	for {
		if ready(ctx, port) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errUnreachable(name, port)
		case <-time.After(pollInterval):
		}
	}
}
