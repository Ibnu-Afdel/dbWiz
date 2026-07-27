package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// typeInto presses each rune of s into the dashboard in turn.
func typeInto(s dashboardScreen, text string) dashboardScreen {
	for _, r := range text {
		s, _ = press(s, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return s
}

// TestCreateDBFormOpensAndSubmits covers Step 6.1: [c] on the databases pane
// opens the create form; a valid name submits and puts a mutation in flight.
func TestCreateDBFormOpensAndSubmits(t *testing.T) {
	s, _ := newPGDashboard(t) // focus starts on databases

	s, _ = press(s, tea.KeyPressMsg{Code: 'c', Text: "c"})
	if s.mode != modeForm || s.formPurpose != purposeCreateDB {
		t.Fatalf("[c] should open the create-database form; mode=%d purpose=%d", s.mode, s.formPurpose)
	}
	if !strings.Contains(s.View(120, 40), "Create database") {
		t.Error("form view should show its title")
	}

	s = typeInto(s, "shop")
	if errText, _ := s.form.validate(s.form); errText != "" {
		t.Fatalf("valid name should clear the error, got %q", errText)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.mode != modeBrowse || !s.working {
		t.Fatalf("submit should return to browse with a mutation in flight; mode=%d working=%v", s.mode, s.working)
	}
}

// TestCreateDBFormValidation covers the live validation: empty and invalid names
// block submit.
func TestCreateDBFormValidation(t *testing.T) {
	f := newCreateDBForm()
	if errText, _ := f.validate(f); errText == "" {
		t.Error("empty name should be an error")
	}
	f = typeInto2(f, "ok_name")
	if errText, _ := f.validate(f); errText != "" {
		t.Errorf("valid name should pass, got %q", errText)
	}
}

// typeInto2 types into a bare form (not the dashboard).
func typeInto2(f formModel, text string) formModel {
	for _, r := range text {
		f, _, _ = f.update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return f
}

// TestCreateDatabaseCmdWithUser covers the create-and-continue command: it
// creates the database, a matching user, and grants it, asking for all three
// reloads.
func TestCreateDatabaseCmdWithUser(t *testing.T) {
	eng := pgEngine()
	msg := runCmd(t, createDatabaseCmd(eng, "shop", true, "pw")).(adminDoneMsg)
	if eng.lastCreateDB != "shop" || eng.lastCreateUser != "shop" {
		t.Errorf("expected db+user 'shop', got db=%q user=%q", eng.lastCreateDB, eng.lastCreateUser)
	}
	if eng.lastGrantUser != "shop" || eng.lastGrantDB != "shop" || !eng.lastGranted {
		t.Errorf("expected grant shop on shop, got user=%q db=%q granted=%v",
			eng.lastGrantUser, eng.lastGrantDB, eng.lastGranted)
	}
	if !msg.reloadDatabases || !msg.reloadUsers {
		t.Error("with-user create should reload both databases and users")
	}
}

// TestDeleteDatabaseConfirmGating covers Steps 6.2/6.3: [D] opens the confirm,
// and the drop only fires after the exact name is typed.
func TestDeleteDatabaseConfirmGating(t *testing.T) {
	s, _ := newPGDashboard(t)
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown}) // move to appdb
	s, _ = press(s, tea.KeyPressMsg{Code: 'D', Text: "D"})
	if s.mode != modeConfirm || s.confirmTarget != "appdb" {
		t.Fatalf("[D] should confirm dropping appdb; mode=%d target=%q", s.mode, s.confirmTarget)
	}

	// A wrong name keeps the confirm open and does nothing.
	s = typeInto(s, "nope")
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.mode != modeConfirm || s.working {
		t.Fatal("mismatched name must not drop anything")
	}

	// Clear and type the exact name → enter drops.
	for range "nope" {
		s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	s = typeInto(s, "appdb")
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.mode != modeBrowse || !s.working {
		t.Fatalf("exact name should fire the drop; mode=%d working=%v", s.mode, s.working)
	}
}

// TestDropDatabaseCmd checks the drop command hits the engine and reports a
// databases reload.
func TestDropDatabaseCmd(t *testing.T) {
	eng := pgEngine()
	msg := runCmd(t, dropDatabaseCmd(eng, "appdb")).(adminDoneMsg)
	if eng.lastDropDB != "appdb" {
		t.Errorf("dropped %q, want appdb", eng.lastDropDB)
	}
	if !msg.reloadDatabases {
		t.Error("drop should reload databases")
	}
}

// TestAdminErrorShowsInlineToast covers 6.3/6.5 error handling: a typed failure
// becomes an inline danger toast, not a full-screen error, and stays a dashboard.
func TestAdminErrorShowsInlineToast(t *testing.T) {
	s, _ := newPGDashboard(t)
	s = feed(s, adminErrMsg{err: &db.DBError{Title: "In use", Detail: "close other connections"}})
	if s.mode != modeBrowse || !s.noticeErr {
		t.Fatal("admin error should return to browse with an error notice")
	}
	view := s.View(120, 40)
	if !strings.Contains(view, "In use") {
		t.Errorf("error toast not shown:\n%s", view)
	}
	if _, ok := Screen(s).(dashboardScreen); !ok {
		t.Error("admin error must not replace the dashboard")
	}
}

// TestCreateUserOpens covers Step 6.4: [c] on the users pane opens the
// create-user form.
func TestCreateUserOpens(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.focus = focusUsers
	s, _ = press(s, tea.KeyPressMsg{Code: 'c', Text: "c"})
	if s.mode != modeForm || s.formPurpose != purposeCreateUser {
		t.Fatalf("[c] on users should open create-user form; mode=%d purpose=%d", s.mode, s.formPurpose)
	}
}

// TestCreateUserPasswordMismatch covers the confirm-field validation.
func TestCreateUserPasswordMismatch(t *testing.T) {
	f := newCreateUserForm()
	f = typeInto2(f, "bob")                               // name
	f, _, _ = f.update(tea.KeyPressMsg{Code: tea.KeyTab}) // → password
	f = typeInto2(f, "secret")                            // password
	f, _, _ = f.update(tea.KeyPressMsg{Code: tea.KeyTab}) // → confirm
	f = typeInto2(f, "different")                         // confirm
	if errText, _ := f.validate(f); !strings.Contains(errText, "match") {
		t.Errorf("mismatched passwords should be flagged, got %q", errText)
	}
}

// TestGrantFlow covers Step 6.6: [g] on a user opens the picker; enter grants
// the chosen database.
func TestGrantFlow(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.focus = focusUsers
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown}) // select bob
	s, _ = press(s, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if s.mode != modeGrant || s.grant.subject != "bob" {
		t.Fatalf("[g] should open grant for bob; mode=%d subject=%q", s.mode, s.grant.subject)
	}
	// The picker offers the databases.
	if len(s.grant.items) != 2 {
		t.Fatalf("grant picker should list 2 databases, got %d", len(s.grant.items))
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter}) // grant on the first db
	if s.mode != modeBrowse || !s.working {
		t.Fatalf("choosing should fire the grant; mode=%d working=%v", s.mode, s.working)
	}
}

// TestGrantCmd checks the grant command hits the engine with the right pair.
func TestGrantCmd(t *testing.T) {
	eng := pgEngine()
	runCmd(t, grantCmd(eng, "bob", "appdb", true))
	if eng.lastGrantUser != "bob" || eng.lastGrantDB != "appdb" || !eng.lastGranted {
		t.Errorf("grant recorded user=%q db=%q granted=%v", eng.lastGrantUser, eng.lastGrantDB, eng.lastGranted)
	}
	runCmd(t, grantCmd(eng, "bob", "appdb", false))
	if eng.lastGranted {
		t.Error("revoke should record granted=false")
	}
}

// TestUsersSectionRendersAndSQLiteHidesAdmin covers 6.4's Capabilities gating:
// the server engine shows a Users section; SQLite shows none and ignores admin
// keys.
func TestUsersSectionRendersAndSQLiteHidesAdmin(t *testing.T) {
	s, _ := newPGDashboard(t)
	if !strings.Contains(s.View(120, 40), "Users") {
		t.Error("server engine should render a Users section")
	}

	sq := sqliteDashboard(t)
	if strings.Contains(sq.View(120, 40), "Users") {
		t.Error("SQLite must not render a Users section")
	}
	// Admin keys are inert on SQLite.
	sq, _ = press(sq, tea.KeyPressMsg{Code: 'c', Text: "c"})
	if sq.mode != modeBrowse {
		t.Error("SQLite should ignore [c] (no databases/users to create)")
	}
}

func sqliteDashboard(t *testing.T) dashboardScreen {
	t.Helper()
	eng := &fakeEngine{
		caps:   db.Capabilities{},
		tables: map[string][]db.Table{"": {{Name: "notes", Rows: 5}}},
	}
	s := NewDashboard(eng, db.Target{Path: "/tmp/dev.sqlite"},
		docker.Container{Name: "dev.sqlite", Engine: docker.EngineUnknown}).(dashboardScreen)
	s = sized(s)
	return feed(s, tablesLoadedMsg{database: "", tables: eng.tables[""]})
}

// TestHomeCreateRoute covers Step 6.7: the home "create" route lands on a
// create-flavoured connect (1 running) or picker (2+).
func TestHomeCreateRoute(t *testing.T) {
	msg := runCmd(t, createRoute([]docker.Container{running("pg"), stopped("old")}))
	push := msg.(PushMsg)
	cs, ok := push.Screen.(connectScreen)
	if !ok || !cs.create {
		t.Fatalf("one running: want connectScreen{create:true}, got %T create=%v", push.Screen, ok && cs.create)
	}

	msg = runCmd(t, createRoute([]docker.Container{running("pg"), running("my")}))
	push = msg.(PushMsg)
	ps, ok := push.Screen.(pickerScreen)
	if !ok || !ps.create {
		t.Fatalf("two running: want pickerScreen{create:true}, got %T", push.Screen)
	}

	msg = runCmd(t, createRoute([]docker.Container{stopped("pg")}))
	if _, ok := msg.(PushMsg).Screen.(errorScreen); !ok {
		t.Error("none running: want errorScreen")
	}
}

// TestDashboardCreatingOpensForm covers the create-on-open path used by 6.7: the
// dashboard emits openCreateDBMsg, which opens the form.
func TestDashboardCreatingOpensForm(t *testing.T) {
	eng := pgEngine()
	s := NewDashboardCreating(eng, db.Target{Database: "postgres"},
		docker.Container{Name: "pg", Engine: docker.EnginePostgres}).(dashboardScreen)
	s = sized(s)
	s = feed(s, openCreateDBMsg{})
	if s.mode != modeForm || s.formPurpose != purposeCreateDB {
		t.Fatalf("create dashboard should open the create form; mode=%d", s.mode)
	}
}
