package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// typeManual sends a run of characters into whatever field the manual form has
// focused.
func typeManual(s manualConnectScreen, text string) manualConnectScreen {
	for _, r := range text {
		scr, _ := s.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		s = scr.(manualConnectScreen)
	}
	return s
}

func tab(s manualConnectScreen) manualConnectScreen {
	scr, _ := s.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	return scr.(manualConnectScreen)
}

// TestManualConnectRenders shows the form with the engine choice preselected to
// postgres and the fields present.
func TestManualConnectRenders(t *testing.T) {
	s := NewManualConnect()
	got := s.View(100, 30)
	for _, want := range []string{"Connect by host and port", "Engine", "‹ postgres ›", "Host", "Port", "User", "Password", "no Docker"} {
		if !strings.Contains(got, want) {
			t.Errorf("manual form missing %q:\n%s", want, got)
		}
	}
}

// TestManualConnectValidation blocks submit until host/port/user are valid.
func TestManualConnectValidation(t *testing.T) {
	s := NewManualConnect().(manualConnectScreen)

	// Empty: an error, submit inert (enter returns no push command).
	if err, _ := s.form.validate(s.form); err == "" {
		t.Fatal("empty form should report an error")
	}
	if _, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Error("enter on an invalid form should not connect")
	}

	// A bad port is rejected with a numeric-range message.
	s = typeManual(s, "db.example.com") // host (focused first)
	s = tab(s)
	s = typeManual(s, "99999") // port out of range
	if err, _ := s.form.validate(s.form); !strings.Contains(err, "1 and 65535") {
		t.Errorf("out-of-range port not rejected: %q", err)
	}
}

// TestManualConnectSubmitPushesConnect a filled form pushes a manual connect
// carrying the typed engine/host/port/user/password.
func TestManualConnectSubmitPushesConnect(t *testing.T) {
	s := NewManualConnect().(manualConnectScreen)

	// Engine: cycle the choice up to mysql (it's the field above host).
	up, _ := s.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	s = up.(manualConnectScreen)
	right, _ := s.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	s = right.(manualConnectScreen)
	if got := s.form.choice("engine"); got != "mysql" {
		t.Fatalf("engine choice: want mysql, got %q", got)
	}

	// Back to host and fill the fields.
	down, _ := s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	s = down.(manualConnectScreen)
	s = typeManual(s, "db.internal")
	s = tab(s)
	s = typeManual(s, "3306")
	s = tab(s)
	s = typeManual(s, "root")
	s = tab(s)
	s = typeManual(s, "s3cret")

	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("a valid form should connect on enter")
	}
	msg := cmd()
	push, ok := msg.(PushMsg)
	if !ok {
		t.Fatalf("want PushMsg, got %T", msg)
	}
	cs, ok := push.Screen.(connectScreen)
	if !ok {
		t.Fatalf("want connectScreen, got %T", push.Screen)
	}
	if !cs.manual || cs.manualKind != db.KindMySQL {
		t.Errorf("manual=%v kind=%v, want true/MySQL", cs.manual, cs.manualKind)
	}
	want := db.Target{Host: "db.internal", Port: 3306, User: "root", Password: "s3cret"}
	if cs.manualBase != want {
		t.Errorf("manualBase = %+v, want %+v", cs.manualBase, want)
	}
}

// TestManualConnectCancel esc pops back to the home menu.
func TestManualConnectCancel(t *testing.T) {
	s := NewManualConnect()
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should emit a Pop command")
	}
	if _, ok := cmd().(PopMsg); !ok {
		t.Errorf("esc: want PopMsg, got %T", cmd())
	}
}
