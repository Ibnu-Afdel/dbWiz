package docker

import (
	"reflect"
	"strings"
	"testing"
)

// TestDockerExecArgs checks the argv: -i only when streaming stdin, -e NAME
// (name only, never a value) for each forwarded var, then container and command.
func TestDockerExecArgs(t *testing.T) {
	got := dockerExecArgs("pg", []string{"PGPASSWORD"}, false, []string{"pg_dump", "-U", "postgres", "shop"})
	want := []string{"exec", "-e", "PGPASSWORD", "pg", "pg_dump", "-U", "postgres", "shop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dump args = %v, want %v", got, want)
	}

	got = dockerExecArgs("my", []string{"MYSQL_PWD"}, true, []string{"mysql", "-u", "root", "shop"})
	want = []string{"exec", "-i", "-e", "MYSQL_PWD", "my", "mysql", "-u", "root", "shop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("restore args = %v, want %v", got, want)
	}
}

// TestDockerExecArgsNoSecretInArgv is the security property: a password value in
// the Env pairs must never reach the argv — only its name does.
func TestDockerExecArgsNoSecretInArgv(t *testing.T) {
	names := envNames([]string{"PGPASSWORD=s3cret", "PGUSER=postgres"})
	args := dockerExecArgs("pg", names, false, []string{"pg_dump", "shop"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "s3cret") {
		t.Fatalf("password leaked into argv: %v", args)
	}
	if !strings.Contains(joined, "PGPASSWORD") || !strings.Contains(joined, "PGUSER") {
		t.Errorf("env names not forwarded: %v", args)
	}
}

// TestEnvNames extracts KEY from KEY=VALUE pairs, tolerating a bare name.
func TestEnvNames(t *testing.T) {
	got := envNames([]string{"A=1", "B=x=y", "C", ""})
	want := []string{"A", "B", "C"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("envNames = %v, want %v", got, want)
	}
}
