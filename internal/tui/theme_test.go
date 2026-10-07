package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

const colorsA = "accent = \"#112233\"\nforeground = \"#eeeeee\"\nbackground = \"#000000\"\n"
const colorsB = "accent = \"#445566\"\nforeground = \"#eeeeee\"\nbackground = \"#000000\"\n"

func TestThemeWatchFollowsChanges(t *testing.T) {
	t.Cleanup(func() { styles.Apply("default") })
	path := filepath.Join(t.TempDir(), "colors.toml")
	if err := os.WriteFile(path, []byte(colorsA), 0o644); err != nil {
		t.Fatal(err)
	}

	w, ok := applyOmarchyTheme(path)
	if !ok {
		t.Fatal("palette didn't apply")
	}
	if styles.Accent != lipgloss.Color("#112233") {
		t.Fatalf("accent = %v after first apply", styles.Accent)
	}
	if w.tick() == nil {
		t.Fatal("a followed theme should schedule a poll")
	}

	// Unchanged file: refresh is a no-op.
	if w2 := w.refresh(); w2 != w {
		t.Fatalf("refresh changed watch without a file change: %+v", w2)
	}

	// `omarchy theme set` rewrites the file; the next poll picks it up.
	if err := os.WriteFile(path, []byte(colorsB), 0o644); err != nil {
		t.Fatal(err)
	}
	later := w.mod.Add(time.Second)
	os.Chtimes(path, later, later)
	w = w.refresh()
	if styles.Accent != lipgloss.Color("#445566") {
		t.Fatalf("accent = %v after the theme changed", styles.Accent)
	}

	// Mid-swap the file can vanish; the current colors stay.
	os.Remove(path)
	if w2 := w.refresh(); w2 != w || styles.Accent != lipgloss.Color("#445566") {
		t.Fatal("a missing file mid-swap should keep the current palette")
	}
}

func TestApplyThemeExplicitNamesSkipOmarchy(t *testing.T) {
	t.Cleanup(func() { styles.Apply("default") })
	if w := applyTheme("warm"); w.path != "" {
		t.Fatalf("an explicit named theme shouldn't follow Omarchy: %+v", w)
	}
	if w := (themeWatch{}); w.tick() != nil {
		t.Fatal("the zero watch shouldn't poll")
	}
}
