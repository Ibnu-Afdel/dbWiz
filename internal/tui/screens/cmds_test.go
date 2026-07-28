package screens

import (
	"testing"
)

// TestExpandTilde covers ~ expansion for the SQLite path input.
func TestExpandTilde(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cases := map[string]string{
		"~":              "/home/tester",
		"~/db.sqlite":    "/home/tester/db.sqlite",
		"/abs/path.db":   "/abs/path.db",
		"relative.db":    "relative.db",
		"~notme/file.db": "~notme/file.db", // only ~ and ~/ expand
	}
	for in, want := range cases {
		if got := expandTilde(in); got != want {
			t.Errorf("expandTilde(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLongestCommonPrefix underpins Tab completion.
func TestLongestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"dev.sqlite", "dev.db"}, "dev."},
		{[]string{"app.db"}, "app.db"},
		{[]string{"foo", "bar"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := longestCommonPrefix(c.in); got != c.want {
			t.Errorf("longestCommonPrefix(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
