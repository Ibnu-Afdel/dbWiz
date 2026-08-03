package keymap

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

// helpID identifies a binding by what the user sees, which is unique even where
// two bindings share a key (r is both "rescan" and "retry", on screens that can't
// both be showing).
func helpID(b key.Binding) string {
	return b.Help().Key + "\x00" + b.Help().Desc
}

// TestCatalogueCoversEveryBinding is the guard that makes one keymap worth
// having: add a binding to KeyMap and forget to describe it, and this fails.
// Without it the F1 list and `dbwiz keys` would quietly fall behind the handlers.
func TestCatalogueCoversEveryBinding(t *testing.T) {
	described := map[string]bool{}
	for _, g := range Groups() {
		for _, e := range g.Entries {
			described[helpID(e.Binding)] = true
		}
	}

	v := reflect.ValueOf(Keys)
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		b, ok := v.Field(i).Interface().(key.Binding)
		if !ok {
			t.Fatalf("KeyMap.%s is not a key.Binding — the reflection guard needs updating", name)
		}
		if !described[helpID(b)] {
			t.Errorf("KeyMap.%s (%q %q) is not in Groups(); add it so F1 and `dbwiz keys` stay honest",
				name, b.Help().Key, b.Help().Desc)
		}
	}
}

// TestCatalogueDescribesNothingExtra is the other direction: a documented key
// that no longer exists in the keymap is worse than a missing one, because it
// tells the user to press something inert.
func TestCatalogueDescribesNothingExtra(t *testing.T) {
	real := map[string]bool{}
	v := reflect.ValueOf(Keys)
	for i := range v.NumField() {
		if b, ok := v.Field(i).Interface().(key.Binding); ok {
			real[helpID(b)] = true
		}
	}

	for _, g := range Groups() {
		for _, e := range g.Entries {
			if !real[helpID(e.Binding)] {
				t.Errorf("%s lists %q %q, which is not in KeyMap", g.Title, e.Key(), e.Label())
			}
		}
	}
}

// TestCatalogueEntriesAreUsable: every row has to carry a key, a short label and
// a real sentence — a blank one is a row that teaches nothing.
func TestCatalogueEntriesAreUsable(t *testing.T) {
	for _, g := range Groups() {
		if g.Title == "" {
			t.Error("a group has no title")
		}
		if len(g.Entries) == 0 {
			t.Errorf("group %q is empty", g.Title)
		}
		for _, e := range g.Entries {
			switch {
			case e.Key() == "":
				t.Errorf("%s: an entry has no key", g.Title)
			case e.Label() == "":
				t.Errorf("%s: %q has no short label", g.Title, e.Key())
			case len(e.About) < 15:
				t.Errorf("%s: %q has no real explanation (%q)", g.Title, e.Key(), e.About)
			case !strings.HasSuffix(e.About, "."):
				t.Errorf("%s: %q's explanation should read as a sentence (%q)", g.Title, e.Key(), e.About)
			}
		}
	}
}

// TestCatalogueEntriesAreUnique: the same binding described twice means two
// places to keep in sync, which is what this package exists to avoid.
func TestCatalogueEntriesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, g := range Groups() {
		for _, e := range g.Entries {
			id := helpID(e.Binding)
			if prev, dup := seen[id]; dup {
				t.Errorf("%q %q is listed in both %s and %s", e.Key(), e.Label(), prev, g.Title)
			}
			seen[id] = g.Title
		}
	}
}

// TestRenderListsEverything covers the CLI's output: every group heading and
// every key reaches the page.
func TestRenderListsEverything(t *testing.T) {
	out := Render()

	if !strings.HasPrefix(out, "DBWiz keys\n") {
		t.Errorf("output should start with a heading, got:\n%s", out)
	}
	for _, g := range Groups() {
		if !strings.Contains(out, g.Title) {
			t.Errorf("missing group %q", g.Title)
		}
		for _, e := range g.Entries {
			if !strings.Contains(out, e.About) {
				t.Errorf("missing explanation for %q", e.Key())
			}
		}
	}
	// Both ways in are named, since the whole point is that they differ.
	if !strings.Contains(out, "F1") || !strings.Contains(out, "?") {
		t.Errorf("the footer should point at both ? and F1:\n%s", out)
	}
}

// TestRenderIsPlainText: the listing gets piped into files and pagers, so it must
// not carry styling.
func TestRenderIsPlainText(t *testing.T) {
	if strings.Contains(Render(), "\x1b[") {
		t.Error("`dbwiz keys` output must be unstyled so it survives a pipe")
	}
}

// TestKeyListIsNotHelp guards the decision behind the two keys: "?" answers what
// works here, F1 answers what exists at all, so they must not be the same key.
func TestKeyListIsNotHelp(t *testing.T) {
	if Keys.KeyList.Help().Key == Keys.Help.Help().Key {
		t.Error("the context help and the full catalogue need distinct keys")
	}
}
