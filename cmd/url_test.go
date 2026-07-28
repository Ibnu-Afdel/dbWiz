package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

func pgCliTarget() cliTarget {
	return cliTarget{kind: db.KindPostgres, target: db.Target{Host: "127.0.0.1", Port: 5432, User: "postgres", Database: "postgres"}}
}

// TestRunURLDefault prints a DATABASE_URL and honors the database-name override.
func TestRunURLDefault(t *testing.T) {
	var out, errb bytes.Buffer
	if err := runURL(context.Background(), &out, &errb, pgCliTarget(), "shop", "", "url", false); err != nil {
		t.Fatalf("runURL: %v", err)
	}
	if !strings.Contains(out.String(), "DATABASE_URL=postgresql://postgres@127.0.0.1:5432/shop") {
		t.Errorf("unexpected output: %q", out.String())
	}
}

// TestRunURLAll renders every shape.
func TestRunURLAll(t *testing.T) {
	var out, errb bytes.Buffer
	if err := runURL(context.Background(), &out, &errb, pgCliTarget(), "", "", "all", false); err != nil {
		t.Fatalf("runURL: %v", err)
	}
	for _, must := range []string{"DATABASE_URL=", "DB_CONNECTION=postgres", "jdbc:postgresql://"} {
		if !strings.Contains(out.String(), must) {
			t.Errorf("all output missing %q:\n%s", must, out.String())
		}
	}
}

// TestRunURLPasswordOverride injects a password into the string.
func TestRunURLPasswordOverride(t *testing.T) {
	var out, errb bytes.Buffer
	if err := runURL(context.Background(), &out, &errb, pgCliTarget(), "", "hunter2", "url", false); err != nil {
		t.Fatalf("runURL: %v", err)
	}
	if !strings.Contains(out.String(), "postgres:hunter2@") {
		t.Errorf("password not applied: %q", out.String())
	}
}

// TestRunURLBadFormat rejects an unknown --format.
func TestRunURLBadFormat(t *testing.T) {
	var out, errb bytes.Buffer
	err := runURL(context.Background(), &out, &errb, pgCliTarget(), "", "", "toml", false)
	if err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Fatalf("expected an unknown-format error, got %v", err)
	}
}

// TestRunURLClipboard copies the same text it prints and notes it on stderr.
func TestRunURLClipboard(t *testing.T) {
	var copied string
	old := setClipboard
	setClipboard = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { setClipboard = old })

	var out, errb bytes.Buffer
	if err := runURL(context.Background(), &out, &errb, pgCliTarget(), "", "", "url", true); err != nil {
		t.Fatalf("runURL: %v", err)
	}
	if !strings.Contains(copied, "DATABASE_URL=") {
		t.Errorf("clipboard got %q", copied)
	}
	if !strings.Contains(errb.String(), "Copied to clipboard") {
		t.Errorf("expected a stderr confirmation, got %q", errb.String())
	}
}
