package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestParseSSH(t *testing.T) {
	t.Setenv("USER", "me")
	cases := []struct {
		in      string
		want    SSHSpec
		wantErr bool
	}{
		{"deploy@1.2.3.4", SSHSpec{User: "deploy", Host: "1.2.3.4", Port: 22}, false},
		{"deploy@db.example.com:2222", SSHSpec{User: "deploy", Host: "db.example.com", Port: 2222}, false},
		{"host-only.example.com", SSHSpec{User: "me", Host: "host-only.example.com", Port: 22}, false},
		{"  deploy@host  ", SSHSpec{User: "deploy", Host: "host", Port: 22}, false},
		{"", SSHSpec{}, true},
		{"deploy@host:0", SSHSpec{}, true},
		{"deploy@host:99999", SSHSpec{}, true},
		{"deploy@host:notaport", SSHSpec{}, true},
	}
	for _, c := range cases {
		got, err := ParseSSH(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSSH(%q) = %+v, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSSH(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSSH(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSSHSpecString(t *testing.T) {
	if s := (SSHSpec{User: "a", Host: "h", Port: 22}).String(); s != "a@h" {
		t.Errorf("default port should be hidden, got %q", s)
	}
	if s := (SSHSpec{User: "a", Host: "h", Port: 2222}).String(); s != "a@h:2222" {
		t.Errorf("non-default port should show, got %q", s)
	}
}

func TestSSHSpecDockerHost(t *testing.T) {
	if h := (SSHSpec{User: "deploy", Host: "1.2.3.4"}).DockerHost(); h != "ssh://deploy@1.2.3.4:22" {
		t.Errorf("DockerHost default port = %q", h)
	}
	if h := (SSHSpec{User: "deploy", Host: "h", Port: 2222}).DockerHost(); h != "ssh://deploy@h:2222" {
		t.Errorf("DockerHost = %q", h)
	}
}

// TestTunnelForwards is the end-to-end proof: Dial an in-process SSH server, open
// a tunnel to a local echo backend through it, and confirm bytes round-trip.
// After Close the local port stops accepting.
func TestTunnelForwards(t *testing.T) {
	echoAddr := startEcho(t)
	_, echoPortStr, _ := net.SplitHostPort(echoAddr)
	var echoPort int
	fmt.Sscanf(echoPortStr, "%d", &echoPort)

	sshAddr := startSSHServer(t)
	_, sshPortStr, _ := net.SplitHostPort(sshAddr)
	var sshPort int
	fmt.Sscanf(sshPortStr, "%d", &sshPort)

	// Point the auth/host-key seams at the throwaway server (none-auth, ignore key).
	origAuth, origHost := authMethodsFn, hostKeyFn
	authMethodsFn = func() ([]ssh.AuthMethod, error) { return []ssh.AuthMethod{}, nil }
	hostKeyFn = func() (ssh.HostKeyCallback, error) { return ssh.InsecureIgnoreHostKey(), nil }
	t.Cleanup(func() { authMethodsFn, hostKeyFn = origAuth, origHost })

	client, err := Dial(context.Background(), SSHSpec{User: "tester", Host: "127.0.0.1", Port: sshPort})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	tun, err := client.Tunnel("127.0.0.1", echoPort)
	if err != nil {
		t.Fatalf("Tunnel: %v", err)
	}
	if tun.LocalHost() != "127.0.0.1" || tun.LocalPort() == 0 {
		t.Fatalf("bad local address %s:%d", tun.LocalHost(), tun.LocalPort())
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tun.LocalPort()))
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo = %q, want ping", buf)
	}
	conn.Close()

	localPort := tun.LocalPort()
	if err := tun.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 500*time.Millisecond); err == nil {
		c.Close()
		t.Error("tunnel still accepting after Close")
	}
}

// startEcho stands up a TCP echo server and returns its address.
func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { defer c.Close(); io.Copy(c, c) }(c)
		}
	}()
	return ln.Addr().String()
}

// startSSHServer stands up an in-process SSH server that services direct-tcpip
// (local forward) channels by dialing the requested address, and returns its
// listen address.
func startSSHServer(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSHConn(c, cfg)
		}
	}()
	return ln.Addr().String()
}

func serveSSHConn(c net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "direct-tcpip" {
			_ = nc.Reject(ssh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		go serveDirectTCPIP(nc)
	}
}

func serveDirectTCPIP(nc ssh.NewChannel) {
	var payload struct {
		DestAddr string
		DestPort uint32
		SrcAddr  string
		SrcPort  uint32
	}
	if err := ssh.Unmarshal(nc.ExtraData(), &payload); err != nil {
		_ = nc.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	remote, err := net.Dial("tcp", net.JoinHostPort(payload.DestAddr, fmt.Sprint(payload.DestPort)))
	if err != nil {
		ch.Close()
		return
	}
	go func() { io.Copy(remote, ch); remote.Close() }()
	io.Copy(ch, remote)
	ch.Close()
}
