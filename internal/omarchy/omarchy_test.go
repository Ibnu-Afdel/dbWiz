package omarchy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectIn(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	orig := systemPath
	t.Cleanup(func() { systemPath = orig })

	t.Run("nothing installed", func(t *testing.T) {
		systemPath = missing
		if detectIn("", t.TempDir()) {
			t.Fatal("detected Omarchy on a bare home")
		}
	})
	t.Run("OMARCHY_PATH set", func(t *testing.T) {
		systemPath = missing
		if !detectIn(t.TempDir(), t.TempDir()) {
			t.Fatal("ignored $OMARCHY_PATH")
		}
	})
	t.Run("OMARCHY_PATH stale", func(t *testing.T) {
		systemPath = missing
		if detectIn(missing, t.TempDir()) {
			t.Fatal("trusted an $OMARCHY_PATH that doesn't exist")
		}
	})
	t.Run("Omarchy 4 system package", func(t *testing.T) {
		systemPath = t.TempDir()
		if !detectIn("", t.TempDir()) {
			t.Fatal("missed /usr/share/omarchy")
		}
	})
	t.Run("Omarchy 3 per-user checkout", func(t *testing.T) {
		systemPath = missing
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".local", "share", "omarchy"), 0o755); err != nil {
			t.Fatal(err)
		}
		if !detectIn("", home) {
			t.Fatal("missed ~/.local/share/omarchy")
		}
	})
}
