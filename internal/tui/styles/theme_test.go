package styles

import "testing"

// TestApplySwitchesAndRebuilds covers v2 3.3: Apply swaps the palette and
// rebuilds the styles that derive from it, and an unknown/blank name falls back
// to the default rather than erroring.
func TestApplySwitchesAndRebuilds(t *testing.T) {
	t.Cleanup(func() { Apply("default") }) // restore for other tests in the package

	Apply("default")
	def := Title.GetForeground()

	Apply("warm")
	warm := Title.GetForeground()
	if warm == def {
		t.Error("switching to the warm theme should change the Title foreground")
	}
	if Accent != themes["warm"].accent {
		t.Error("Apply should update the Accent palette var")
	}

	// Unknown and blank names both fall back to default (no error, no panic).
	Apply("no-such-theme")
	if Accent != themes["default"].accent {
		t.Error("an unknown theme should fall back to default")
	}
	Apply("")
	if Accent != themes["default"].accent {
		t.Error("a blank theme should fall back to default")
	}
}
