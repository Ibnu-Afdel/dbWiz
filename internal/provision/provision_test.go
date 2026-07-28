package provision

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// TestRunArgsMatchOmarchy verifies the argv reproduces Omarchy's provisioning
// defaults (detached, restart, localhost port, name, dev-auth env) for each
// engine, so a DBWiz-created container is interchangeable with an Omarchy one.
func TestRunArgsMatchOmarchy(t *testing.T) {
	pg, _ := Lookup("postgres")
	want := []string{"run", "-d", "--restart", "unless-stopped", "-p", "127.0.0.1:5432:5432", "--name", "postgres18", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "postgres:18"}
	if got := pg.RunArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("postgres args = %v, want %v", got, want)
	}

	my, _ := Lookup("mysql")
	got := strings.Join(my.RunArgs(), " ")
	for _, must := range []string{"--name mysql8", "127.0.0.1:3306:3306", "MYSQL_ALLOW_EMPTY_PASSWORD=true", "mysql:8.4"} {
		if !strings.Contains(got, must) {
			t.Errorf("mysql args missing %q: %s", must, got)
		}
	}
}

// TestRunArgsNoSudo is the deliberate deviation from Omarchy: DBWiz never shells
// sudo.
func TestRunArgsNoSudo(t *testing.T) {
	for _, s := range Specs() {
		for _, a := range s.RunArgs() {
			if a == "sudo" {
				t.Fatalf("%s argv contains sudo: %v", s.Key, s.RunArgs())
			}
		}
	}
}

// TestLookupUnknown fails closed on an unrecognized engine keyword.
func TestLookupUnknown(t *testing.T) {
	if _, ok := Lookup("mongodb"); ok {
		t.Error("mongodb is not SQL and must not be provisionable")
	}
}

// TestCheckNameTaken catches an existing container of the same name before run.
func TestCheckNameTaken(t *testing.T) {
	defer swapReachable(func(context.Context, int) bool { return false })()
	pg, _ := Lookup("postgres")
	got := Check(context.Background(), pg, []docker.Container{{Name: "postgres18", State: docker.StateStopped}})
	if got == nil || got.Kind != NameTaken {
		t.Fatalf("want NameTaken, got %v", got)
	}
}

// TestCheckPortTakenByContainer flags a different container already on the port.
func TestCheckPortTakenByContainer(t *testing.T) {
	defer swapReachable(func(context.Context, int) bool { return false })()
	pg, _ := Lookup("postgis") // wants 5432
	got := Check(context.Background(), pg, []docker.Container{{Name: "postgres18", State: docker.StateRunning, HostPort: 5432}})
	if got == nil || got.Kind != PortTaken {
		t.Fatalf("want PortTaken, got %v", got)
	}
	if !strings.Contains(got.Detail, "postgres18") {
		t.Errorf("detail should name the port holder: %q", got.Detail)
	}
}

// TestCheckPortTakenByProcess flags an unrelated listener via the probe.
func TestCheckPortTakenByProcess(t *testing.T) {
	defer swapReachable(func(context.Context, int) bool { return true })()
	my, _ := Lookup("mysql")
	got := Check(context.Background(), my, nil)
	if got == nil || got.Kind != PortTaken {
		t.Fatalf("want PortTaken from the probe, got %v", got)
	}
}

// TestCheckClear returns nil when nothing clashes.
func TestCheckClear(t *testing.T) {
	defer swapReachable(func(context.Context, int) bool { return false })()
	pg, _ := Lookup("postgres")
	if got := Check(context.Background(), pg, nil); got != nil {
		t.Fatalf("want no conflict, got %v", got)
	}
}

// TestRunDelegates confirms Run forwards the spec's argv to the docker seam.
func TestRunDelegates(t *testing.T) {
	var gotArgs []string
	defer swapRun(func(_ context.Context, args []string) (string, error) {
		gotArgs = args
		return "cid", nil
	})()
	pg, _ := Lookup("postgres")
	id, err := Run(context.Background(), pg)
	if err != nil || id != "cid" {
		t.Fatalf("Run = %q, %v", id, err)
	}
	if !reflect.DeepEqual(gotArgs, pg.RunArgs()) {
		t.Errorf("Run passed %v, want %v", gotArgs, pg.RunArgs())
	}
}

func swapReachable(f func(context.Context, int) bool) func() {
	prev := reachable
	reachable = f
	return func() { reachable = prev }
}

func swapRun(f func(context.Context, []string) (string, error)) func() {
	prev := runContainer
	runContainer = f
	return func() { runContainer = prev }
}
