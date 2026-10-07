package screens

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/state"
)

// Continuing to a remembered SQLite file must land on its dashboard. The open
// runs as a command whose result comes back to the home screen, which used to
// drop it: the file was opened but the menu just sat there.
func TestHomeContinueOpensSQLiteDashboard(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "shop.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.SetLastSQLite(path); err != nil {
		t.Fatal(err)
	}

	home := NewHomeContinuing(nil)
	if h := home.(homeScreen); h.cont == nil || h.choices[h.cursor] != choiceContinue {
		t.Fatal("expected the continue row, selected")
	}

	_, open := home.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if open == nil {
		t.Fatal("enter on continue returned no command")
	}
	opened, ok := open().(sqliteOpenedMsg)
	if !ok {
		t.Fatalf("open returned %T, want sqliteOpenedMsg", open())
	}
	t.Cleanup(func() { _ = opened.engine.Close() })

	_, next := home.Update(opened)
	if next == nil {
		t.Fatal("home ignored the opened file")
	}
	push, ok := next().(PushMsg)
	if !ok {
		t.Fatalf("got %T, want a push to the dashboard", next())
	}
	if _, ok := push.Screen.(dashboardScreen); !ok {
		t.Fatalf("pushed %T, want dashboardScreen", push.Screen)
	}
}

// A file that fails to open shows an error instead of silently doing nothing.
func TestHomeContinueSQLiteErrorShowsScreen(t *testing.T) {
	home := NewHomeContinuing(nil)
	_, next := home.Update(sqliteErrMsg{err: asDBError(os.ErrPermission)})
	if next == nil {
		t.Fatal("home ignored the open error")
	}
	if _, ok := next().(PushMsg).Screen.(errorScreen); !ok {
		t.Fatal("want an error screen")
	}
}
