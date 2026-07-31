package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// TestListTable renders detected containers as an aligned table, sorted by name,
// with the running/stopped state and an Omarchy marker.
func TestListTable(t *testing.T) {
	fakeDetect(t, []docker.Container{
		{Name: "pg-dev", Engine: docker.EnginePostgres, State: docker.StateRunning, HostPort: 5432, Image: "postgres:18", Source: docker.SourceOmarchy},
		{Name: "app-my", Engine: docker.EngineMySQL, State: docker.StateStopped, Image: "mysql:8"},
	}, nil)

	var out bytes.Buffer
	if err := runList(context.Background(), &out, false, ""); err != nil {
		t.Fatalf("runList: %v", err)
	}
	got := out.String()
	for _, want := range []string{"NAME", "ENGINE", "STATE", "pg-dev *", "app-my", "running", "stopped", "5432"} {
		if !strings.Contains(got, want) {
			t.Errorf("table missing %q:\n%s", want, got)
		}
	}
	// A stopped container with no published port shows "-", not "0".
	if strings.Contains(got, "\t0\t") || strings.Contains(got, " 0 ") {
		t.Errorf("expected a dash for the missing port, got:\n%s", got)
	}
	// Sorted by name: app-my before pg-dev.
	if strings.Index(got, "app-my") > strings.Index(got, "pg-dev") {
		t.Errorf("rows not sorted by name:\n%s", got)
	}
}

// TestListJSON emits a machine-readable array with no credentials in it.
func TestListJSON(t *testing.T) {
	fakeDetect(t, []docker.Container{
		{Name: "pg-dev", Engine: docker.EnginePostgres, State: docker.StateRunning, HostPort: 5432, Image: "postgres:18",
			Source: docker.SourceOmarchy, Creds: docker.Creds{User: "postgres", Password: "s3cret"}},
	}, nil)

	var out bytes.Buffer
	if err := runList(context.Background(), &out, true, ""); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if strings.Contains(out.String(), "s3cret") || strings.Contains(strings.ToLower(out.String()), "password") {
		t.Fatalf("list JSON must never carry credentials:\n%s", out.String())
	}
	var rows []listRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(rows) != 1 || rows[0].Name != "pg-dev" || rows[0].Engine != "postgres" ||
		rows[0].State != "running" || rows[0].Port != 5432 || !rows[0].Omarchy {
		t.Fatalf("unexpected row: %+v", rows)
	}
}

// TestListEmpty is a success with a clear notice (table) and a valid empty array
// (JSON), so a script distinguishes "nothing detected" from a scan failure by
// exit code.
func TestListEmpty(t *testing.T) {
	fakeDetect(t, nil, nil)

	var table bytes.Buffer
	if err := runList(context.Background(), &table, false, ""); err != nil {
		t.Fatalf("runList table: %v", err)
	}
	if !strings.Contains(table.String(), "No database containers") {
		t.Errorf("expected an empty-state notice, got %q", table.String())
	}

	var js bytes.Buffer
	if err := runList(context.Background(), &js, true, ""); err != nil {
		t.Fatalf("runList json: %v", err)
	}
	if strings.TrimSpace(js.String()) != "[]" {
		t.Errorf("expected an empty JSON array, got %q", js.String())
	}
}

// TestListRemote scans a remote Docker daemon over SSH with --ssh: the SSH
// target is parsed into a ssh:// DOCKER_HOST and the returned containers render
// like any local scan.
func TestListRemote(t *testing.T) {
	gotHost := fakeDetectRemote(t, []docker.Container{
		{Name: "pg-prod", Engine: docker.EnginePostgres, State: docker.StateRunning, HostPort: 5432, Image: "postgres:18"},
	}, nil)
	// The local scan must not run when --ssh is set.
	fakeDetect(t, []docker.Container{{Name: "should-not-appear"}}, nil)

	var out bytes.Buffer
	if err := runList(context.Background(), &out, false, "deploy@db.example.com"); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if *gotHost != "ssh://deploy@db.example.com:22" {
		t.Errorf("DOCKER_HOST = %q, want ssh://deploy@db.example.com:22", *gotHost)
	}
	got := out.String()
	if !strings.Contains(got, "pg-prod") || strings.Contains(got, "should-not-appear") {
		t.Errorf("expected the remote scan's containers, got:\n%s", got)
	}
}

// TestListRemoteBadTarget rejects a malformed --ssh value before dialing, so a
// typo is a clear error rather than a hung connection.
func TestListRemoteBadTarget(t *testing.T) {
	called := fakeDetectRemote(t, nil, nil)

	var out bytes.Buffer
	err := runList(context.Background(), &out, false, "   ")
	if err == nil {
		t.Fatal("expected an error for an empty SSH target")
	}
	if *called != "" {
		t.Errorf("remote scan should not run on a bad target, got host %q", *called)
	}
}
