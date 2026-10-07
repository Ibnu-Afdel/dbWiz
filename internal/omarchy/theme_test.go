package omarchy

import (
	"os"
	"path/filepath"
	"testing"
)

func writeColors(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "colors.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadPaletteNamedDialect(t *testing.T) {
	p, err := ReadPalette(writeColors(t, `
mode = "light"
accent = "#509475"
foreground = "#C1C497"
background = "#111c18"
muted = "#53685B"
red = "#FF5345"
green = "#549e6a"
yellow = "#E5C736"
`))
	if err != nil {
		t.Fatal(err)
	}
	want := Palette{Accent: "#509475", Foreground: "#C1C497", Background: "#111c18", Muted: "#53685B",
		Red: "#FF5345", Green: "#549e6a", Yellow: "#E5C736", Light: true}
	if p != want {
		t.Fatalf("palette = %+v, want %+v", p, want)
	}
}

// Palettes Omarchy generates from an alacritty.toml use color0–color15.
func TestReadPaletteNumberedDialect(t *testing.T) {
	p, err := ReadPalette(writeColors(t, `
accent = "#7aa2f7"
background = "#1a1b26"
foreground = "#c0caf5"
color1 = "#f7768e"
color2 = "#9ece6a"
color3 = "#e0af68"
color8 = "#414868"
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Red != "#f7768e" || p.Green != "#9ece6a" || p.Yellow != "#e0af68" || p.Muted != "#414868" {
		t.Fatalf("numbered fallbacks not used: %+v", p)
	}
}

func TestReadPaletteDerivesMuted(t *testing.T) {
	p, err := ReadPalette(writeColors(t, `
accent = "#0000ff"
foreground = "#ffffff"
background = "#000000"
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Muted != "#808080" {
		t.Fatalf("muted = %q, want the fg/bg midpoint #808080", p.Muted)
	}
}

func TestReadPaletteRejectsIncomplete(t *testing.T) {
	for name, body := range map[string]string{
		"no accent": `foreground = "#ffffff"` + "\n" + `background = "#000000"`,
		"bad hex":   `accent = "blue"` + "\n" + `foreground = "#ffffff"` + "\n" + `background = "#000000"`,
		"not toml":  `accent = `,
	} {
		if _, err := ReadPalette(writeColors(t, body)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestThemeColorsPathIn(t *testing.T) {
	home := t.TempDir()
	if got := themeColorsPathIn(home); got != "" {
		t.Fatalf("empty home gave %q", got)
	}
	legacy := filepath.Join(home, ".config", "omarchy", "current", "theme")
	os.MkdirAll(legacy, 0o755)
	os.WriteFile(filepath.Join(legacy, "colors.toml"), nil, 0o644)
	if got := themeColorsPathIn(home); got != filepath.Join(legacy, "colors.toml") {
		t.Fatalf("legacy path not found: %q", got)
	}
	// Omarchy 4's state path wins over the legacy one.
	current := filepath.Join(home, ".local", "state", "omarchy", "current", "theme")
	os.MkdirAll(current, 0o755)
	os.WriteFile(filepath.Join(current, "colors.toml"), nil, 0o644)
	if got := themeColorsPathIn(home); got != filepath.Join(current, "colors.toml") {
		t.Fatalf("Omarchy 4 path not preferred: %q", got)
	}
}
