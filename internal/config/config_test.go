package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig points XDG_CONFIG_HOME at a temp dir and writes body to the config
// file there, returning the temp base. An empty body writes no file.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if body != "" {
		dir := filepath.Join(base, "dbwiz")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

// TestLoadMissingIsZero covers the core promise: no config file → zero Config,
// no error, app runs unchanged.
func TestLoadMissingIsZero(t *testing.T) {
	writeConfig(t, "")
	c, err := Load()
	if err != nil {
		t.Fatalf("missing config should not error, got %v", err)
	}
	if c.Theme != "" || c.DefaultRowLimit != 0 || len(c.Targets) != 0 {
		t.Errorf("missing config should be zero, got %+v", c)
	}
}

// TestLoadValid parses scalars and a saved target.
func TestLoadValid(t *testing.T) {
	writeConfig(t, `
theme = "high-contrast"
default_row_limit = 500

[[target]]
name = "prod-ro"
engine = "postgres"
host = "db.internal"
port = 5432
user = "readonly"
database = "appdb"
`)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Theme != "high-contrast" || c.DefaultRowLimit != 500 {
		t.Errorf("scalars = %q / %d", c.Theme, c.DefaultRowLimit)
	}
	if len(c.Targets) != 1 {
		t.Fatalf("want 1 target, got %d", len(c.Targets))
	}
	tg := c.Targets[0]
	if tg.Name != "prod-ro" || tg.Engine != "postgres" || tg.Host != "db.internal" ||
		tg.Port != 5432 || tg.User != "readonly" || tg.Database != "appdb" {
		t.Errorf("target parsed wrong: %+v", tg)
	}
}

// TestLoadEditorVim covers the opt-in modal-editor flag (v2 2.4): absent it's
// off, and the [editor] table turns it on.
func TestLoadEditorVim(t *testing.T) {
	writeConfig(t, "theme = \"x\"")
	if c, _ := Load(); c.Editor.Vim {
		t.Error("editor.vim should default off with no [editor] table")
	}
	writeConfig(t, "[editor]\nvim = true\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Editor.Vim {
		t.Errorf("[editor] vim = true should enable the modal editor, got %+v", c.Editor)
	}
}

// TestLoadParseErrorSurfaces covers that a malformed file is reported, not
// silently swallowed — a typo shouldn't look like "no config".
func TestLoadParseErrorSurfaces(t *testing.T) {
	writeConfig(t, "default_row_limit = = 3")
	if _, err := Load(); err == nil {
		t.Fatal("a malformed config should return an error")
	}
}

// TestValidAndValidTargets covers the per-target validation and filtering.
func TestValidAndValidTargets(t *testing.T) {
	cases := []struct {
		t    ManualTarget
		want bool
	}{
		{ManualTarget{Name: "ok", Engine: "postgres", Host: "h", Port: 5432, User: "u"}, true},
		{ManualTarget{Name: "", Engine: "postgres", Host: "h", Port: 5432, User: "u"}, false},
		{ManualTarget{Name: "n", Engine: "oracle", Host: "h", Port: 5432, User: "u"}, false},
		{ManualTarget{Name: "n", Engine: "mysql", Host: "", Port: 3306, User: "u"}, false},
		{ManualTarget{Name: "n", Engine: "mysql", Host: "h", Port: 0, User: "u"}, false},
		{ManualTarget{Name: "n", Engine: "mysql", Host: "h", Port: 3306, User: ""}, false},
	}
	valid := 0
	c := Config{}
	for _, tc := range cases {
		if ok, _ := tc.t.Valid(); ok != tc.want {
			t.Errorf("Valid(%+v) = %v, want %v", tc.t, ok, tc.want)
		}
		c.Targets = append(c.Targets, tc.t)
		if tc.want {
			valid++
		}
	}
	if got := c.ValidTargets(); len(got) != valid {
		t.Errorf("ValidTargets kept %d, want %d", len(got), valid)
	}
}

// TestPathRespectsXDG covers the location resolution.
func TestPathRespectsXDG(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "dbwiz", "config.toml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}
