package omarchy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Palette is the active Omarchy theme reduced to the roles DBWiz paints with.
// Every field is a "#rrggbb" hex string.
type Palette struct {
	Accent     string
	Foreground string
	Background string
	Muted      string
	Red        string
	Green      string
	Yellow     string
	Light      bool // the theme declares mode = "light"
}

// ThemeColorsPath returns the active theme's colors.toml: Omarchy 4 keeps it in
// ~/.local/state/omarchy/current/theme, Omarchy 3 in
// ~/.config/omarchy/current/theme. It returns "" when neither exists.
func ThemeColorsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return themeColorsPathIn(home)
}

func themeColorsPathIn(home string) string {
	for _, p := range []string{
		filepath.Join(home, ".local", "state", "omarchy", "current", "theme", "colors.toml"),
		filepath.Join(home, ".config", "omarchy", "current", "theme", "colors.toml"),
	} {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// LoadPalette reads the active theme's palette.
func LoadPalette() (Palette, error) {
	path := ThemeColorsPath()
	if path == "" {
		return Palette{}, fmt.Errorf("no Omarchy theme colors found")
	}
	return ReadPalette(path)
}

// ReadPalette parses a colors.toml. Omarchy writes two dialects: stock themes
// name their roles (accent, red, muted, …), while palettes generated from an
// alacritty.toml use color0–color15. Each role falls back across both, and only
// the roles DBWiz can't do without — accent, foreground, background — are
// required.
func ReadPalette(path string) (Palette, error) {
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return Palette{}, fmt.Errorf("reading %s: %w", path, err)
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := raw[k].(string); ok && hexColor.MatchString(s) {
				return s
			}
		}
		return ""
	}

	p := Palette{
		Accent:     pick("accent", "color4", "blue"),
		Foreground: pick("foreground", "color7"),
		Background: pick("background", "color0"),
		Muted:      pick("muted", "color8", "bright_black"),
		Red:        pick("red", "color1"),
		Green:      pick("green", "color2"),
		Yellow:     pick("yellow", "color3", "orange"),
		Light:      raw["mode"] == "light",
	}
	if p.Accent == "" || p.Foreground == "" || p.Background == "" {
		return Palette{}, fmt.Errorf("%s is missing accent, foreground, or background", path)
	}
	if p.Muted == "" {
		p.Muted = mix(p.Foreground, p.Background, 0.5)
	}
	return p, nil
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// mix blends two #rrggbb colors; amount 0 is a, 1 is b. Inputs are validated
// by the caller.
func mix(a, b string, amount float64) string {
	ch := func(hex string, i int) float64 {
		v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		return float64(v)
	}
	out := "#"
	for i := 1; i < 7; i += 2 {
		v := ch(a, i)*(1-amount) + ch(b, i)*amount
		out += fmt.Sprintf("%02x", int(v+0.5))
	}
	return out
}
