package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// emptyMenu is the extension file DBWiz creates when Omarchy's is missing.
const emptyMenu = "{\n  // Extend the Omarchy menu with JSONC. See $OMARCHY_PATH/default/omarchy/omarchy-menu.jsonc.\n}\n"

// upsertMenuFile adds DBWiz's block to the menu extension at path, or replaces
// the one already there. The first time it touches an existing file it keeps a
// .dbwiz.bak copy. It reports whether the file changed.
func upsertMenuFile(path, block string) (bool, error) {
	data, err := os.ReadFile(path)
	existed := err == nil
	switch {
	case errors.Is(err, os.ErrNotExist):
		data = []byte(emptyMenu)
	case err != nil:
		return false, err
	}
	src := string(data)
	out, err := upsertMenuBlock(src, block)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if out == src {
		return false, nil
	}
	backup := path + ".dbwiz.bak"
	if existed && !strings.Contains(src, menuBegin) {
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			if err := writeFile(backup, data); err != nil {
				return false, err
			}
		}
	}
	return true, writeFile(path, []byte(out))
}

// upsertMenuBlock returns src with DBWiz's block in place: replacing an
// existing block, or inserted before the object's closing brace. The result is
// checked the way Omarchy's menu loader reads it, so DBWiz never hands the
// shell a menu it can't parse — and refuses to touch one that was already
// broken.
func upsertMenuBlock(src, block string) (string, error) {
	if err := validateMenu(src); err != nil {
		return "", fmt.Errorf("the existing menu doesn't parse (%v); fix it first", err)
	}
	if stripped, found := removeMenuBlock(src); found {
		src = stripped
	}
	end := strings.LastIndex(src, "}")
	if end < 0 {
		return "", errors.New("no closing brace")
	}
	head, tail := strings.TrimRight(src[:end], " \t\n"), src[end:]
	// The block follows the user's last entry, so that entry needs a comma —
	// unless it already has one or the object is empty.
	if last := lastSignificantLine(head); last != "" && !strings.HasSuffix(last, ",") && !strings.HasSuffix(last, "{") {
		head += ","
	}
	out := head + "\n" + block + "\n" + tail
	if err := validateMenu(out); err != nil {
		return "", fmt.Errorf("adding DBWiz would break the menu (%v); left it unchanged", err)
	}
	return out, nil
}

// removeMenuBlock strips DBWiz's marked block, reporting whether there was one.
func removeMenuBlock(src string) (string, bool) {
	start := strings.Index(src, menuBegin)
	if start < 0 {
		return src, false
	}
	end := strings.Index(src[start:], menuEnd)
	if end < 0 {
		return src, false
	}
	end += start + len(menuEnd)
	if end < len(src) && src[end] == '\n' {
		end++
	}
	// Drop the newline before the block too, so remove(install(x)) == x.
	if start > 0 && src[start-1] == '\n' {
		start--
	}
	return src[:start] + "\n" + strings.TrimLeft(src[end:], "\n"), true
}

// lastSignificantLine returns the last line of src that isn't blank or a
// full-line comment, trimmed.
func lastSignificantLine(src string) string {
	lines := strings.Split(src, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" && !strings.HasPrefix(l, "//") {
			return l
		}
	}
	return ""
}

// Omarchy's MenuModel.js stripJsonc: drop full-line // comments, then trailing
// commas before a closing bracket.
var (
	jsoncComment  = regexp.MustCompile(`(?m)^\s*//[^\n]*(\n|$)`)
	jsoncTrailing = regexp.MustCompile(`,(\s*[}\]])`)
)

// validateMenu parses src exactly as Omarchy's shell does and requires an
// object of objects.
func validateMenu(src string) error {
	s := jsoncComment.ReplaceAllString(src, "")
	s = jsoncTrailing.ReplaceAllString(s, "$1")
	var v map[string]map[string]any
	return json.Unmarshal([]byte(s), &v)
}
