package docker

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// timePast is a deadline already in the past, used to force DeadlineExceeded.
func timePast() time.Time { return time.Now().Add(-time.Hour) }

// fakeRunner returns a dockerRunner that always yields the given output.
func fakeRunner(stdout, stderr string, err error) dockerRunner {
	return func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		return []byte(stdout), []byte(stderr), err
	}
}

func TestClassifyImage(t *testing.T) {
	tests := []struct {
		image string
		want  Engine
	}{
		{"postgres:18", EnginePostgres},
		{"postgres", EnginePostgres},
		{"pgvector/pgvector:pg16", EnginePostgres},
		{"postgis/postgis:16-3.4-alpine", EnginePostgres},
		{"timescale/timescaledb:latest-pg16", EnginePostgres},
		{"bitnami/postgresql:16", EnginePostgres},
		{"docker.io/library/postgres:16", EnginePostgres},
		{"mysql:8.4", EngineMySQL},
		{"percona:8.0", EngineMySQL},
		{"bitnami/mysql:8.4", EngineMySQL},
		{"mariadb:11.8", EngineMariaDB},
		// Must NOT classify: an app that merely mentions postgres in its tag.
		{"ghcr.io/umami-software/umami:postgresql-latest", EngineUnknown},
		{"redis:alpine", EngineUnknown},
		{"mongo:noble", EngineUnknown},
		{"mcr.microsoft.com/mssql/server:2022-latest", EngineUnknown},
		{"myregistry.local:5000/postgres:16", EnginePostgres}, // registry port isn't a tag
		{"postgres@sha256:abc123", EnginePostgres},            // digest stripped
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := classifyImage(tt.image); got != tt.want {
				t.Errorf("classifyImage(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		ports         string
		containerPort int
		want          int
	}{
		{"0.0.0.0:5432->5432/tcp, [::]:5432->5432/tcp", 5432, 5432},
		{"127.0.0.1:3307->3306/tcp", 3306, 3307},
		{"0.0.0.0:3007->3000/tcp, [::]:3007->3000/tcp", 5432, 0}, // app port, not a db mapping
		{"", 5432, 0},                       // stopped container, no ports
		{"5432/tcp", 5432, 0},               // exposed-only, not published
		{"[::]:5432->5432/tcp", 5432, 5432}, // IPv6-only binding still yields the port
	}
	for _, tt := range tests {
		t.Run(tt.ports, func(t *testing.T) {
			if got := parseHostPort(tt.ports, tt.containerPort); got != tt.want {
				t.Errorf("parseHostPort(%q, %d) = %d, want %d", tt.ports, tt.containerPort, got, tt.want)
			}
		})
	}
}

func TestOmarchyCreds(t *testing.T) {
	if c, ok := omarchyCreds("postgres18", EnginePostgres); !ok || c.User != "postgres" || c.Password != "" {
		t.Errorf("postgres18: got %+v ok=%v, want trust-auth postgres", c, ok)
	}
	if c, ok := omarchyCreds("mysql8", EngineMySQL); !ok || c.User != "root" || c.Password != "" {
		t.Errorf("mysql8: got %+v ok=%v, want empty-password root", c, ok)
	}
	if c, ok := omarchyCreds("mariadb11", EngineMariaDB); !ok || c.User != "root" {
		t.Errorf("mariadb11: got %+v ok=%v, want root", c, ok)
	}
	// Name/engine mismatch must not attach creds (name never classifies).
	if _, ok := omarchyCreds("postgres18", EngineMySQL); ok {
		t.Error("postgres18 with MySQL engine must not match")
	}
	if _, ok := omarchyCreds("some-app-postgres18-thing", EnginePostgres); ok {
		t.Error("only the exact stock name should match")
	}
}

// realMachineFixture is the verified `docker ps -a` from 01-RESEARCH.md §2,
// as newline-delimited JSON (the format docker emits).
const realMachineFixture = `{"Image":"pgvector/pgvector:pg16","Names":"fawz-postgres","Ports":"0.0.0.0:5432->5432/tcp, [::]:5432->5432/tcp","State":"running"}
{"Image":"ghcr.io/umami-software/umami:postgresql-latest","Names":"fawz-umami","Ports":"0.0.0.0:3007->3000/tcp","State":"running"}
{"Image":"mysql:8.4","Names":"tena-inventory-mysql-1","Ports":"","State":"exited"}
{"Image":"postgis/postgis:16-3.4-alpine","Names":"postgres16","Ports":"","State":"exited"}
{"Image":"mysql:8.4","Names":"mysql8","Ports":"","State":"exited"}
{"Image":"redis:alpine","Names":"tena-inventory-redis-1","Ports":"","State":"exited"}
`

func TestDetectRealMachine(t *testing.T) {
	got, err := detect(context.Background(), fakeRunner(realMachineFixture, "", nil), true /* on Omarchy */)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	// umami (app) and redis (non-SQL) are excluded; 4 DB containers remain.
	want := []Container{
		{Name: "fawz-postgres", Image: "pgvector/pgvector:pg16", Engine: EnginePostgres, HostPort: 5432, State: StateRunning, Source: SourceGeneric},
		{Name: "tena-inventory-mysql-1", Image: "mysql:8.4", Engine: EngineMySQL, HostPort: 0, State: StateStopped, Source: SourceGeneric},
		{Name: "postgres16", Image: "postgis/postgis:16-3.4-alpine", Engine: EnginePostgres, HostPort: 0, State: StateStopped, Source: SourceGeneric},
		{Name: "mysql8", Image: "mysql:8.4", Engine: EngineMySQL, HostPort: 0, State: StateStopped, Source: SourceOmarchy, Creds: Creds{User: "root"}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d containers, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("container[%d]:\n got  %+v\n want %+v", i, got[i], want[i])
		}
	}
}

func TestDetectComposeNamingMatchesByImage(t *testing.T) {
	// Sail-style "<project>-mysql-1" must classify by image, not by name.
	fixture := `{"Image":"mysql:8.4","Names":"myapp-mysql-1","Ports":"127.0.0.1:3306->3306/tcp","State":"running"}` + "\n"
	got, err := detect(context.Background(), fakeRunner(fixture, "", nil), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Engine != EngineMySQL || got[0].HostPort != 3306 {
		t.Fatalf("got %+v, want one MySQL on 3306", got)
	}
}

func TestDetectErrorsAreTyped(t *testing.T) {
	_, err := detect(context.Background(), fakeRunner("", "Cannot connect to the Docker daemon", errors.New("exit 1")), false)
	var de *DockerError
	if !errors.As(err, &de) || de.Kind != DockerErrDaemonDown {
		t.Fatalf("want typed DaemonDown, got %v", err)
	}

	_, err = detect(context.Background(), fakeRunner("", "", exec.ErrNotFound), false)
	if !errors.As(err, &de) || de.Kind != DockerErrBinaryMissing {
		t.Fatalf("want typed BinaryMissing, got %v", err)
	}
}

func TestDetectEmptyIsNotAnError(t *testing.T) {
	got, err := detect(context.Background(), fakeRunner("", "", nil), false)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty docker ps should yield (nil, nil); got %+v, %v", got, err)
	}
}
