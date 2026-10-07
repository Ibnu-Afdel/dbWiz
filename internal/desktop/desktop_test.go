package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func opts(t *testing.T, omarchy bool) Options {
	t.Helper()
	home := t.TempDir()
	return Options{
		Exe:      "/usr/bin/dbwiz",
		Omarchy:  omarchy,
		Menu:     true,
		Home:     home,
		DataHome: filepath.Join(home, ".local", "share"),
	}
}

func TestEntryGeneric(t *testing.T) {
	e := Entry(Options{Exe: "/home/me/go/bin/dbwiz"})
	for _, want := range []string{"Exec=/home/me/go/bin/dbwiz\n", "Terminal=true", "Icon=dbwiz", managedKey} {
		if !strings.Contains(e, want) {
			t.Errorf("entry missing %q:\n%s", want, e)
		}
	}
}

func TestEntryOmarchyLaunchOrFocus(t *testing.T) {
	e := Entry(Options{Exe: "/usr/bin/dbwiz", Omarchy: true, Sudo: true})
	if !strings.Contains(e, "Exec=omarchy-launch-or-focus-tui /usr/bin/dbwiz --sudo\n") {
		t.Errorf("Omarchy entry should launch-or-focus with --sudo:\n%s", e)
	}
	if !strings.Contains(e, "Terminal=false") {
		t.Errorf("Omarchy entry opens its own terminal:\n%s", e)
	}
}

// Omarchy's launch-or-focus evals its command, so a path needing quotes falls
// back to the plain entry rather than risk a broken split.
func TestEntryOmarchyUnsafePathFallsBack(t *testing.T) {
	e := Entry(Options{Exe: "/home/me/my apps/dbwiz", Omarchy: true})
	if !strings.Contains(e, `Exec="/home/me/my apps/dbwiz"`) || !strings.Contains(e, "Terminal=true") {
		t.Errorf("unsafe path should use a quoted plain entry:\n%s", e)
	}
	if LaunchCommand(Options{Exe: "/a b/dbwiz", Omarchy: true}) != "" {
		t.Error("LaunchCommand should refuse an unsafe path")
	}
}

func TestMenuBlockParsesAsOmarchyMenu(t *testing.T) {
	block := MenuBlock(Options{Exe: "/usr/bin/dbwiz", Omarchy: true, Sudo: true})
	if err := validateMenu("{\n" + block + "\n}"); err != nil {
		t.Fatalf("block doesn't parse: %v\n%s", err, block)
	}
	for _, want := range []string{`"dbwiz": {`, `omarchy-launch-or-focus-tui /usr/bin/dbwiz --sudo`, `'/usr/bin/dbwiz --sudo list'`} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}
}

func TestUpsertMenuBlock(t *testing.T) {
	block := MenuBlock(Options{Exe: "/usr/bin/dbwiz", Omarchy: true})
	cases := map[string]string{
		"comments only (Omarchy's stock file)": "{\n  // Extend the menu.\n  // \"personal\": {\"label\":\"Personal\"},\n}\n",
		"entry without trailing comma":         "{\n  \"personal\": {\"icon\":\"x\",\"label\":\"Personal\"}\n}\n",
		"entry with trailing comma":            "{\n  \"personal\": {\"icon\":\"x\",\"label\":\"Personal\"},\n  // a note\n}\n",
		"empty object":                         "{}",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := upsertMenuBlock(src, block)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateMenu(out); err != nil {
				t.Fatalf("result doesn't parse: %v\n%s", err, out)
			}
			// Idempotent: a second install changes nothing.
			again, err := upsertMenuBlock(out, block)
			if err != nil || again != out {
				t.Fatalf("second upsert changed the file:\n%s\n---\n%s", out, again)
			}
			// Removal leaves no DBWiz trace and a parsable menu.
			back, found := removeMenuBlock(out)
			if !found || strings.Contains(back, "dbwiz") {
				t.Fatalf("remove left DBWiz behind:\n%s", back)
			}
			if err := validateMenu(back); err != nil {
				t.Fatalf("menu after remove doesn't parse: %v\n%s", err, back)
			}
		})
	}
}

func TestUpsertMenuBlockRefusesBrokenMenu(t *testing.T) {
	if _, err := upsertMenuBlock("{\n  \"a\": {\"label\":\"A\"} \"b\": {}\n}", "  \"dbwiz\": {},"); err == nil {
		t.Fatal("should refuse to edit a menu that already doesn't parse")
	}
}

func TestInstallAndRemove(t *testing.T) {
	o := opts(t, true)
	p := PathsFor(o)
	original := "{\n  \"personal\": {\"icon\":\"x\",\"label\":\"Personal\"}\n}\n"
	os.MkdirAll(filepath.Dir(p.Menu), 0o755)
	os.WriteFile(p.Menu, []byte(original), 0o600)

	r, err := Install(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changed) != 3 {
		t.Fatalf("changed = %v, want icon, entry, menu", r.Changed)
	}
	for _, f := range []string{p.Entry, p.Icon, p.Menu + ".dbwiz.bak"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	if bak, _ := os.ReadFile(p.Menu + ".dbwiz.bak"); string(bak) != original {
		t.Error("backup isn't the original menu")
	}
	if info, _ := os.Stat(p.Menu); info.Mode().Perm() != 0o600 {
		t.Errorf("menu mode = %v, want the original 0600 kept", info.Mode().Perm())
	}

	// Re-install leaves the menu untouched.
	r, err = Install(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Changed {
		if c == p.Menu {
			t.Error("re-install rewrote an up-to-date menu")
		}
	}

	if _, err := Remove(o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Entry); !os.IsNotExist(err) {
		t.Error("entry not removed")
	}
	if _, err := os.Stat(p.Icon); !os.IsNotExist(err) {
		t.Error("icon not removed")
	}
	menu, _ := os.ReadFile(p.Menu)
	if strings.Contains(string(menu), "dbwiz") || validateMenu(string(menu)) != nil {
		t.Fatalf("menu after remove:\n%s", menu)
	}
}

func TestInstallWontClobberForeignEntry(t *testing.T) {
	o := opts(t, false)
	p := PathsFor(o)
	os.MkdirAll(filepath.Dir(p.Entry), 0o755)
	os.WriteFile(p.Entry, []byte("[Desktop Entry]\nName=Mine\n"), 0o644)
	if _, err := Install(o); err == nil {
		t.Fatal("overwrote a hand-made launcher")
	}
	r, err := Remove(o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Entry); err != nil || len(r.Skipped) == 0 {
		t.Fatal("remove should leave a hand-made launcher in place")
	}
}

func TestInstallNoMenuOffOmarchy(t *testing.T) {
	o := opts(t, false)
	if _, err := Install(o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PathsFor(o).Menu); !os.IsNotExist(err) {
		t.Fatal("wrote an Omarchy menu on a non-Omarchy machine")
	}
}
