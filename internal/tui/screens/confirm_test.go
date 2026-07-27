package screens

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// typeString feeds each rune of s to the confirm model as a key press.
func typeConfirm(c confirmModel, s string) confirmModel {
	for _, r := range s {
		c, _, _ = c.update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return c
}

// TestConfirmGatesOnExactMatch is the core safety contract of Step 6.2: Enter
// does nothing until the typed text matches the target exactly.
func TestConfirmGatesOnExactMatch(t *testing.T) {
	c := newConfirm("Delete database", "This can't be undone.", "appdb")

	// Enter before typing: inert.
	if _, res, _ := c.update(tea.KeyPressMsg{Code: tea.KeyEnter}); res != confirmPending {
		t.Fatalf("empty enter: result = %d, want pending", res)
	}

	// A near-miss still doesn't arm it.
	c = typeConfirm(c, "appd")
	if c.matched() {
		t.Error("partial text should not match")
	}
	if _, res, _ := c.update(tea.KeyPressMsg{Code: tea.KeyEnter}); res != confirmPending {
		t.Fatalf("near-miss enter: result = %d, want pending", res)
	}

	// Complete the name → enter accepts.
	c = typeConfirm(c, "b")
	if !c.matched() {
		t.Fatal("exact text should match")
	}
	if _, res, _ := c.update(tea.KeyPressMsg{Code: tea.KeyEnter}); res != confirmAccepted {
		t.Fatalf("exact enter: result = %d, want accepted", res)
	}
}

// TestConfirmEscAborts verifies esc always aborts, even mid-typing.
func TestConfirmEscAborts(t *testing.T) {
	c := newConfirm("Delete user", "msg", "bob")
	c = typeConfirm(c, "bo")
	if _, res, _ := c.update(tea.KeyPressMsg{Code: tea.KeyEsc}); res != confirmAborted {
		t.Fatalf("esc: result = %d, want aborted", res)
	}
}
