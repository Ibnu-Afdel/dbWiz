package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/health"
)

// TestRunHealthNonZeroOnFail returns an error (→ exit 1) after printing, when a
// check fails. We use a SQLite target pointed at a missing file so auth fails
// deterministically without Docker.
func TestRunHealthNonZeroOnFail(t *testing.T) {
	ht := health.Target{SQLite: true, Kind: db.KindSQLite, DBTarget: db.Target{Path: "/nonexistent/dir/definitely-missing.db"}}
	var out bytes.Buffer
	err := runHealth(context.Background(), &out, ht, false)
	// The report is printed regardless.
	if !strings.Contains(out.String(), "CHECK") || !strings.Contains(out.String(), "Authentication") {
		t.Errorf("expected a rendered table, got:\n%s", out.String())
	}
	if err == nil {
		t.Errorf("expected a non-zero (error) result when a check fails")
	}
}

// TestRunHealthJSON emits a valid machine-readable report.
func TestRunHealthJSON(t *testing.T) {
	ht := health.Target{SQLite: true, Kind: db.KindSQLite, DBTarget: db.Target{Path: "/nonexistent/dir/missing.db"}}
	var out bytes.Buffer
	_ = runHealth(context.Background(), &out, ht, true)
	var payload struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Name, Status, Detail string
		} `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(payload.Checks) == 0 {
		t.Error("expected checks in the JSON report")
	}
}
