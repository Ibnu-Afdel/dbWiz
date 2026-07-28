package screens

import (
	"slices"
	"strings"
	"testing"

	"github.com/Ibnu-Afdel/dbwiz/internal/config"
	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

func containsChoice(cs []homeChoice, want homeChoice) bool {
	return slices.Contains(cs, want)
}

// TestApplyConfig covers v2 3.3 wiring: a positive row limit overrides the
// default, and only valid saved targets are kept.
func TestApplyConfig(t *testing.T) {
	t.Cleanup(func() { previewLimit = 200; savedTargets = nil })
	ApplyConfig(config.Config{
		DefaultRowLimit: 42,
		Targets: []config.ManualTarget{
			{Name: "ok", Engine: "postgres", Host: "h", Port: 5432, User: "u"},
			{Name: "bad", Engine: "oracle", Host: "h", Port: 1521, User: "u"}, // invalid → filtered
		},
	})
	if previewLimit != 42 {
		t.Errorf("previewLimit = %d, want 42", previewLimit)
	}
	if got := SavedTargets(); len(got) != 1 || got[0].Name != "ok" {
		t.Errorf("SavedTargets = %v, want [ok]", got)
	}

	// A zero/negative limit leaves the current value untouched.
	ApplyConfig(config.Config{DefaultRowLimit: 0})
	if previewLimit != 42 {
		t.Errorf("a zero limit should not override; previewLimit = %d", previewLimit)
	}
}

// TestHomeShowsSavedRowWhenConfigured covers that the saved-target row appears
// only when config defines one, keeping the Docker-only menu lean.
func TestHomeShowsSavedRowWhenConfigured(t *testing.T) {
	t.Cleanup(func() { savedTargets = nil })

	savedTargets = nil
	if h := NewHome(nil).(homeScreen); containsChoice(h.choices, choiceSaved) {
		t.Error("no saved targets → no saved row")
	}

	savedTargets = []config.ManualTarget{{Name: "prod", Engine: "postgres", Host: "h", Port: 5432, User: "u"}}
	h := NewHome(nil).(homeScreen)
	if !containsChoice(h.choices, choiceSaved) {
		t.Fatal("a saved target should add the saved row")
	}
	if !strings.Contains(h.View(120, 40), "saved target") {
		t.Error("home view should show the saved-target row")
	}
}

// TestSavedRoute covers the 1-vs-many routing: one connects straight through,
// two opens the picker.
func TestSavedRoute(t *testing.T) {
	one := []config.ManualTarget{{Name: "a", Engine: "postgres", Host: "h", Port: 5432, User: "u"}}
	if push := runCmd(t, savedRoute(one)).(PushMsg); !isConnect(push.Screen) {
		t.Errorf("one saved → connectScreen, got %T", push.Screen)
	}
	two := append(one, config.ManualTarget{Name: "b", Engine: "mysql", Host: "h", Port: 3306, User: "u"})
	if push := runCmd(t, savedRoute(two)).(PushMsg); !isSaved(push.Screen) {
		t.Errorf("two saved → savedScreen, got %T", push.Screen)
	}
}

func isConnect(s Screen) bool { _, ok := s.(connectScreen); return ok }
func isSaved(s Screen) bool   { _, ok := s.(savedScreen); return ok }

// TestNewConnectManual covers that a manual connect is built for the target and
// kicks off a connect attempt on Init.
func TestNewConnectManual(t *testing.T) {
	mt := config.ManualTarget{Name: "prod", Engine: "mysql", Host: "db", Port: 3306, User: "root", Database: "app"}
	s := NewConnectManual(mt).(connectScreen)
	if !s.manual || s.manualName != "prod" || s.manualKind != db.KindMySQL {
		t.Fatalf("manual fields wrong: manual=%v name=%q kind=%v", s.manual, s.manualName, s.manualKind)
	}
	if s.manualBase != (db.Target{Host: "db", Port: 3306, User: "root", Database: "app"}) {
		t.Errorf("manualBase = %+v", s.manualBase)
	}
	if s.container.Name != "prod" {
		t.Errorf("synthetic container name = %q, want prod", s.container.Name)
	}
	if s.Init() == nil {
		t.Error("Init should kick off a connect attempt")
	}
}

// TestConnectManualCmd covers the manual connect command: it builds the target
// from the base plus the prompted password and labels the synthetic container.
func TestConnectManualCmd(t *testing.T) {
	old := NewEngineFn
	eng := &fakeEngine{caps: db.Capabilities{MultipleDatabases: true}}
	NewEngineFn = func(db.Kind) (db.Engine, error) { return eng, nil }
	t.Cleanup(func() { NewEngineFn = old })

	base := db.Target{Host: "h", Port: 5432, User: "u", Database: "app"}
	msg := runCmd(t, connectManualCmd("prod", db.KindPostgres, base, nil, db.Target{Password: "pw"}, true))
	c, ok := msg.(connectedMsg)
	if !ok {
		t.Fatalf("want connectedMsg, got %T", msg)
	}
	if c.target.Password != "pw" || c.target.Host != "h" || c.target.User != "u" {
		t.Errorf("target = %+v", c.target)
	}
	if c.container.Name != "prod" || c.container.Engine != docker.EnginePostgres || c.container.HostPort != 5432 {
		t.Errorf("container = %+v", c.container)
	}
}
