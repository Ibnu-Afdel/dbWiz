package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/keymap"
)

// TestKeysCommandPrintsTheCatalogue: `dbwiz keys` is the cheat-sheet form of the
// F1 overlay, and reads the same keymap — so if it ever prints something the UI
// doesn't answer to, internal/keymap's own tests fail first. Here we only check
// it reaches the page.
func TestKeysCommandPrintsTheCatalogue(t *testing.T) {
	var out bytes.Buffer
	c := NewKeysCommand()
	c.SetOut(&out)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs(nil)
	if err := c.Execute(); err != nil {
		t.Fatalf("dbwiz keys: %v", err)
	}

	got := out.String()
	for _, g := range keymap.Groups() {
		if !strings.Contains(got, g.Title) {
			t.Errorf("output missing group %q:\n%s", g.Title, got)
		}
	}
	// A couple of specific keys, so a catalogue that silently emptied itself would
	// still be caught here.
	for _, want := range []string{"ctrl+c", "F1", "⌥p", "Compare this database's structure"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestKeysCommandNeedsNoTarget: reading the keys must not require Docker, a
// container, or any connection — it's documentation.
func TestKeysCommandNeedsNoTarget(t *testing.T) {
	fakeDetect(t, nil, nil) // no containers detected at all

	var out bytes.Buffer
	c := NewKeysCommand()
	c.SetOut(&out)
	c.SetArgs(nil)
	if err := c.Execute(); err != nil {
		t.Fatalf("dbwiz keys should work with nothing running: %v", err)
	}
	if out.Len() == 0 {
		t.Error("expected the keymap, got nothing")
	}
}

// TestKeysCommandRejectsArguments: `dbwiz keys select` is a mistake worth naming
// rather than ignoring.
func TestKeysCommandRejectsArguments(t *testing.T) {
	c := NewKeysCommand()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"unexpected"})
	if err := c.Execute(); err == nil {
		t.Error("an unexpected argument should be reported")
	}
}
