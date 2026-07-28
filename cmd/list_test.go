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
	if err := runList(context.Background(), &out, false); err != nil {
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
	if err := runList(context.Background(), &out, true); err != nil {
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
	if err := runList(context.Background(), &table, false); err != nil {
		t.Fatalf("runList table: %v", err)
	}
	if !strings.Contains(table.String(), "No database containers") {
		t.Errorf("expected an empty-state notice, got %q", table.String())
	}

	var js bytes.Buffer
	if err := runList(context.Background(), &js, true); err != nil {
		t.Fatalf("runList json: %v", err)
	}
	if strings.TrimSpace(js.String()) != "[]" {
		t.Errorf("expected an empty JSON array, got %q", js.String())
	}
}
