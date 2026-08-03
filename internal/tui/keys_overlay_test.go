package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/tui/screens"
)

// sizedModel builds a root model on a screen with no I/O, laid out for a normal
// terminal.
func sizedModel(t *testing.T, w, h int) model {
	t.Helper()
	m := newModelWith(screens.NewSQLiteOpen())
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(model)
}

// pressKey sends one key to the root model.
func pressKey(m model, msg tea.KeyPressMsg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

var f1 = tea.KeyPressMsg{Code: tea.KeyF1}

// TestKeysOverlayOpensAndCloses covers the F1 catalogue: it lists every key with
// its explanation, and both esc and F1 dismiss it.
func TestKeysOverlayOpensAndCloses(t *testing.T) {
	m := pressKey(sizedModel(t, 100, 30), f1)
	if !m.showKeys {
		t.Fatal("F1 should open the catalogue")
	}

	view := m.View().Content
	for _, want := range []string{
		"All keys",
		"Always available",
		"Moving around",
		"Expand the bottom bar to the keys that work right here",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("catalogue missing %q:\n%s", want, view)
		}
	}

	if closed := pressKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}); closed.showKeys {
		t.Error("esc should close the catalogue")
	}
	if toggled := pressKey(m, f1); toggled.showKeys {
		t.Error("F1 should toggle the catalogue closed")
	}
}

// TestKeysOverlayIsModal: while it's open, keys must not reach the screen behind
// it — otherwise reading the help would act on the app.
func TestKeysOverlayIsModal(t *testing.T) {
	m := pressKey(sizedModel(t, 100, 30), f1)

	// sqlite_open takes free text, so a stray "r" would show up in its path field
	// if the key got through. Screens aren't comparable values, so compare what
	// they render.
	before := m.top().View(100, 20)
	after := pressKey(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	if after.top().View(100, 20) != before {
		t.Error("a key press reached the screen behind the catalogue")
	}
	if !after.showKeys {
		t.Error("an unrelated key should not close the catalogue")
	}
}

// TestKeysOverlayStillQuits: modal or not, ctrl+c always works — the app's
// anti-dead-end rule (Phase 8.5) has no exceptions.
func TestKeysOverlayStillQuits(t *testing.T) {
	m := pressKey(sizedModel(t, 100, 30), f1)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c should quit from the catalogue")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c should produce tea.QuitMsg")
	}
}

// TestKeysOverlayScrolls: the catalogue is longer than a terminal, so it has to
// scroll — and never past its own end.
func TestKeysOverlayScrolls(t *testing.T) {
	m := pressKey(sizedModel(t, 100, 24), f1)

	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.keysOffset != 1 {
		t.Errorf("down should scroll by one, offset=%d", m.keysOffset)
	}
	first := m.View().Content

	for range 50 {
		m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	last := m.View().Content
	if last == first {
		t.Error("paging down should move the catalogue")
	}
	// The last group has to be reachable, and the box must not run off the window.
	if !strings.Contains(last, "Databases and users") {
		t.Errorf("scrolling to the end should reach the last group:\n%s", last)
	}
	if got := len(strings.Split(last, "\n")); got > 24 {
		t.Errorf("the catalogue rendered %d lines into a 24-row window", got)
	}

	m = pressKey(m, tea.KeyPressMsg{Code: tea.KeyHome})
	if m.keysOffset != 0 {
		t.Errorf("home should return to the top, offset=%d", m.keysOffset)
	}
}

// TestExpandedHelpFitsTheWindow is the regression test for the bug that started
// this: View reserved a hard-coded single row for the help bar, so pressing "?"
// — which stacks one row per binding — pushed the layout past the bottom of the
// window and the list came out truncated.
func TestExpandedHelpFitsTheWindow(t *testing.T) {
	for _, height := range []int{24, 30, 40} {
		m := sizedModel(t, 100, height)

		collapsed := len(strings.Split(m.View().Content, "\n"))
		if collapsed > height {
			t.Errorf("height %d: collapsed help already overflows at %d lines", height, collapsed)
		}

		expanded := pressKey(m, tea.KeyPressMsg{Code: '?', Text: "?"})
		if !expanded.help.ShowAll {
			t.Fatal("? should expand the help bar")
		}
		if got := len(strings.Split(expanded.View().Content, "\n")); got > height {
			t.Errorf("height %d: expanded help renders %d lines and gets cut off", height, got)
		}
	}
}

// TestExpandedHelpShowsEveryBinding: the point of "?" is that nothing hides
// behind the "…", so every key the screen offers has to be on screen.
func TestExpandedHelpShowsEveryBinding(t *testing.T) {
	m := pressKey(sizedModel(t, 100, 30), tea.KeyPressMsg{Code: '?', Text: "?"})
	view := m.View().Content

	for _, b := range m.helpBindings() {
		if !strings.Contains(view, b.Help().Desc) {
			t.Errorf("expanded help omits %q (%s):\n%s", b.Help().Desc, b.Help().Key, view)
		}
	}
}

// TestHelpBarOffersTheCatalogue: F1 has to be discoverable from the footer, or
// nobody finds it.
func TestHelpBarOffersTheCatalogue(t *testing.T) {
	m := sizedModel(t, 120, 30)
	if !strings.Contains(m.View().Content, "all keys") {
		t.Errorf("the help bar should advertise F1:\n%s", m.View().Content)
	}
}
