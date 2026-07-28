// Package remote reaches databases that live behind an SSH host (v3 3.2). It
// dials an SSH connection (agent or key-file auth, verified against
// known_hosts) and opens a local port-forward to a database address as seen
// from that host, so the rest of DBWiz connects to a plain 127.0.0.1:<port>
// and needs no knowledge of the tunnel.
//
// It is a leaf package: it imports golang.org/x/crypto/ssh and the standard
// library, never DBWiz's db/docker/tui layers. Callers rewrite a db.Target's
// Host/Port to the tunnel's local address; the tunnel's lifetime is tied to the
// connection by closing it when the engine closes.
package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// dialTimeout bounds the TCP dial and SSH handshake so an unreachable or wedged
// host fails fast instead of hanging the connect flow.
const dialTimeout = 10 * time.Second

// SSHSpec is where and as whom to open the SSH connection: user@host[:port].
// Port defaults to 22.
type SSHSpec struct {
	User string
	Host string
	Port int
}

// ParseSSH reads "user@host", "user@host:port", or "host" into an SSHSpec. A
// missing user defaults to the current OS user; a missing port defaults to 22.
// It rejects empty input and a missing host so a malformed config value is
// caught rather than dialed.
func ParseSSH(s string) (SSHSpec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return SSHSpec{}, errors.New("empty SSH target")
	}
	spec := SSHSpec{Port: 22}
	if at := strings.LastIndex(s, "@"); at >= 0 {
		spec.User = s[:at]
		s = s[at+1:]
	} else if u := os.Getenv("USER"); u != "" {
		spec.User = u
	}
	// Split host[:port]. Host may be an IPv6 literal in brackets.
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		// No port present — the whole thing is the host.
		host = s
		port = ""
	}
	spec.Host = strings.Trim(host, "[]")
	if spec.Host == "" {
		return SSHSpec{}, fmt.Errorf("SSH target %q has no host", s)
	}
	if port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return SSHSpec{}, fmt.Errorf("SSH target %q has an invalid port", s)
		}
		spec.Port = p
	}
	if spec.User == "" {
		return SSHSpec{}, fmt.Errorf("SSH target %q has no user (use user@host)", s)
	}
	return spec, nil
}

// Addr is the host:port to dial.
func (s SSHSpec) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// String renders the spec back as user@host[:port] for labels and errors.
func (s SSHSpec) String() string {
	if s.Port == 22 || s.Port == 0 {
		return s.User + "@" + s.Host
	}
	return fmt.Sprintf("%s@%s:%d", s.User, s.Host, s.Port)
}

// DockerHost renders the spec as a DOCKER_HOST value, so the docker CLI talks to
// the remote daemon over SSH (v3 3.3). The port is always explicit.
func (s SSHSpec) DockerHost() string {
	port := s.Port
	if port == 0 {
		port = 22
	}
	return fmt.Sprintf("ssh://%s@%s:%d", s.User, s.Host, port)
}

// authMethodsFn and hostKeyFn are seams: production reads the SSH agent, the
// user's key files, and ~/.ssh/known_hosts; tests substitute a fixed key and
// host-key check so the tunnel can be exercised against an in-process server.
var (
	authMethodsFn = defaultAuthMethods
	hostKeyFn     = defaultHostKey
)

// Client is a live SSH connection that can open tunnels.
type Client struct{ c *ssh.Client }

// Dial opens an SSH connection to spec using agent/key auth and known_hosts
// verification. The returned Client must be Closed (a Tunnel that owns it does
// this for you).
func Dial(ctx context.Context, spec SSHSpec) (*Client, error) {
	auths, err := authMethodsFn()
	if err != nil {
		return nil, err
	}
	hostKey, err := hostKeyFn()
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            spec.User,
		Auth:            auths,
		HostKeyCallback: hostKey,
		Timeout:         dialTimeout,
	}

	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", spec.Addr())
	if err != nil {
		return nil, fmt.Errorf("can't reach SSH host %s: %w", spec, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, spec.Addr(), cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH connection to %s failed: %w", spec, err)
	}
	return &Client{c: ssh.NewClient(sshConn, chans, reqs)}, nil
}

// Close ends the SSH connection.
func (c *Client) Close() error {
	if c == nil || c.c == nil {
		return nil
	}
	return c.c.Close()
}

// Tunnel is a local TCP listener that forwards every accepted connection to a
// fixed remote address through the SSH connection. It owns the Client, so
// closing the tunnel closes the SSH connection too.
type Tunnel struct {
	ln         net.Listener
	client     *Client
	remoteAddr string
	done       chan struct{}
	closeOnce  sync.Once
}

// Tunnel opens a local forward to remoteHost:remotePort as reachable from the
// SSH host (so remoteHost is usually 127.0.0.1 — the database bound to the SSH
// host's loopback). It listens on 127.0.0.1 with an OS-chosen port; read it
// with LocalPort.
func (c *Client) Tunnel(remoteHost string, remotePort int) (*Tunnel, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("can't open a local tunnel port: %w", err)
	}
	t := &Tunnel{
		ln:         ln,
		client:     c,
		remoteAddr: net.JoinHostPort(remoteHost, strconv.Itoa(remotePort)),
		done:       make(chan struct{}),
	}
	go t.serve()
	return t, nil
}

func (t *Tunnel) serve() {
	for {
		local, err := t.ln.Accept()
		if err != nil {
			return // listener closed (Close) or a fatal accept error
		}
		go t.forward(local)
	}
}

// forward pipes one accepted local connection to the remote address over SSH,
// copying in both directions until either side closes.
func (t *Tunnel) forward(local net.Conn) {
	defer local.Close()
	remote, err := t.client.c.Dial("tcp", t.remoteAddr)
	if err != nil {
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
	<-done
}

// LocalHost is the loopback address the tunnel listens on.
func (t *Tunnel) LocalHost() string { return "127.0.0.1" }

// LocalPort is the OS-chosen port the tunnel listens on — the port a driver
// connects to.
func (t *Tunnel) LocalPort() int { return t.ln.Addr().(*net.TCPAddr).Port }

// Close stops accepting connections and closes the owned SSH client. It is safe
// to call more than once.
func (t *Tunnel) Close() error {
	var err error
	t.closeOnce.Do(func() {
		close(t.done)
		err = t.ln.Close()
		_ = t.client.Close()
	})
	return err
}

// defaultAuthMethods gathers the usable auth methods: the running SSH agent (if
// SSH_AUTH_SOCK is set) plus any unencrypted default identity files. It errors
// only when nothing at all is available, with a hint, rather than dialing a
// connection that can't authenticate.
func defaultAuthMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}
	if signers := loadKeyFiles(); len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	if len(methods) == 0 {
		return nil, errors.New("no SSH auth available — start an ssh-agent (SSH_AUTH_SOCK) or add an unencrypted key at ~/.ssh/id_ed25519 or ~/.ssh/id_rsa")
	}
	return methods, nil
}

// loadKeyFiles parses the conventional default identity files, skipping any that
// are missing or passphrase-protected (those are the agent's job).
func loadKeyFiles() []ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var signers []ssh.Signer
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		data, err := os.ReadFile(filepath.Join(home, ".ssh", name))
		if err != nil {
			continue
		}
		if s, err := ssh.ParsePrivateKey(data); err == nil {
			signers = append(signers, s)
		}
	}
	return signers
}

// defaultHostKey verifies the server against ~/.ssh/known_hosts, so a
// man-in-the-middle or an unknown host is refused. A missing known_hosts file
// is a clear, actionable error rather than a silent downgrade to no
// verification.
func defaultHostKey() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	cb, err := knownhosts.New(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no %s — ssh to the host once from your shell to trust it, then retry", path)
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return cb, nil
}
