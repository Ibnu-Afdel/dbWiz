package migrations

import (
	"context"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// TestRenderReport pins the shape of the common answer: which tool, which table,
// how many, and the most recent ones newest-first.
func TestRenderReport(t *testing.T) {
	status, err := Detect(context.Background(), laravelSource(), "shop", 2)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	report := Render(status)

	for _, want := range []string{
		"Migrations in shop (PostgreSQL)",
		"Laravel · migrations · 3 migrations applied",
		"2026_07_14_120000_create_orders_table",
		"Showing the 2 most recent of 3.",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
	// The oldest of the three wasn't read, so it must not appear.
	if strings.Contains(report, "create_users_table") {
		t.Errorf("report shows a migration outside the limit:\n%s", report)
	}
}

// TestRenderSaysWhatItCannotKnow: "3 applied" reads as "up to date" to exactly
// the beginner this feature is for, and DBWiz — which never looks at the
// migration files — has no way to know whether it is. The caveat is part of the
// deliverable, not decoration.
func TestRenderSaysWhatItCannotKnow(t *testing.T) {
	status, err := Detect(context.Background(), laravelSource(), "shop", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	report := Render(status)
	if !strings.Contains(report, "pending") || !strings.Contains(report, "migration files") {
		t.Errorf("report must say it can't report pending migrations:\n%s", report)
	}
}

// TestRenderNoLedger: the empty answer has to explain itself, since "nothing
// found" is otherwise indistinguishable from "the feature is broken".
func TestRenderNoLedger(t *testing.T) {
	report := Render(Status{Database: "shop", Engine: "PostgreSQL"})
	for _, want := range []string{
		"No migration ledger found.",
		"schema_migrations",
		"nothing manages its schema",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "pending") {
		t.Errorf("the pending caveat only makes sense with a ledger to caveat:\n%s", report)
	}
}

// TestRenderGuessed: a shape match must read as a guess on the page, not just in
// the struct.
func TestRenderGuessed(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"data_migrations": {cols: []string{"id", "name", "run_at"}, rows: [][]any{{int64(1), "backfill totals", "2026-07-14 12:05"}}},
	}}
	status, err := Detect(context.Background(), src, "shop", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	report := Render(status)
	if !strings.Contains(report, "unrecognised layout · data_migrations") {
		t.Errorf("report should label the guess:\n%s", report)
	}
	if !strings.Contains(report, "match no layout") {
		t.Errorf("report should explain what a guess means:\n%s", report)
	}
}

// TestRenderTrouble: the reason a beginner opens this at all is a migration that
// half-ran, so the flagged row has to be impossible to miss.
func TestRenderTrouble(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"schema_migrations": {cols: []string{"version", "dirty"}, rows: [][]any{{"20260714120000", true}}},
	}}
	status, err := Detect(context.Background(), src, "app", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	report := Render(status)
	if !strings.Contains(report, "! marked dirty") {
		t.Errorf("report should flag the dirty row:\n%s", report)
	}
	if !strings.Contains(report, "20260714120000") {
		t.Errorf("report should name the version it's stuck on:\n%s", report)
	}
}

// TestRenderPointerSaysTheVersionOnce: the heading of a pointer ledger already
// carries its one version, so the row list underneath would only repeat it.
func TestRenderPointerSaysTheVersionOnce(t *testing.T) {
	src := &fakeSource{kind: db.KindPostgres, tables: map[string]fakeTable{
		"schema_migrations": {cols: []string{"version", "dirty"}, rows: [][]any{{"20260714120000", true}}},
	}}
	status, err := Detect(context.Background(), src, "app", 0)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	report := Render(status)
	if n := strings.Count(report, "20260714120000"); n != 1 {
		t.Errorf("version appears %d times, want once:\n%s", n, report)
	}
	if !strings.Contains(report, "! marked dirty") {
		t.Errorf("the trouble still has to show:\n%s", report)
	}
}
