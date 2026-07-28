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
	f := newCreateUserForm(db.KindPostgres)
	f = typeInto2(f, "bob")                               // name
	f, _, _ = f.update(tea.KeyPressMsg{Code: tea.KeyTab}) // → password
	f = typeInto2(f, "secret")                            // password
	f, _, _ = f.update(tea.KeyPressMsg{Code: tea.KeyTab}) // → confirm
	f = typeInto2(f, "different")                         // confirm
	if errText, _ := f.validate(f); !strings.Contains(errText, "match") {
		t.Errorf("mismatched passwords should be flagged, got %q", errText)
	}
}

// TestEditUserOpensWithCurrentFlags covers v2 3.2: [a] on a user opens the edit
// form, seeded with the user's current LOGIN flag and titled with its name.
func TestEditUserOpensWithCurrentFlags(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.focus = focusUsers // alice is first, CanLogin true
	s, cmd := press(s, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if s.mode != modeForm || s.formPurpose != purposeEditUser {
		t.Fatalf("[a] should open the edit-user form; mode=%d purpose=%d", s.mode, s.formPurpose)
	}
	if s.editUserName != "alice" {
		t.Errorf("edit subject should be alice, got %q", s.editUserName)
	}
	if !s.form.toggle("canLogin") {
		t.Error("form should preset LOGIN from the user's current flag")
	}
	if cmd == nil {
		t.Error("opening the form should return its Init command")
	}
	if !strings.Contains(s.form.View(120), "Edit user alice") {
		t.Error("form should be titled for the user")
	}
}

// TestEditUserIsUsersPaneOnly covers the gating: [a] does nothing off the users
// pane and is inert on SQLite (no users at all).
func TestEditUserIsUsersPaneOnly(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.focus = focusDatabases
	s, _ = press(s, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if s.mode != modeBrowse {
		t.Error("[a] on the databases pane should be inert")
	}

	sq := sqliteDashboard(t)
	sq, _ = press(sq, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if sq.mode != modeBrowse {
		t.Error("SQLite should ignore [a] (no users)")
	}
}

// TestAlterUserCmd covers the edit command's two independent effects: role flags
// only when the engine has them, and a password change only when one was typed.
func TestAlterUserCmd(t *testing.T) {
	eng := pgEngine()
	msg := runCmd(t, alterUserCmd(eng, "alice", true, false, true, "newpw"))
	done, ok := msg.(adminDoneMsg)
	if !ok || !done.reloadUsers {
		t.Fatalf("alterUserCmd should report a users reload, got %#v", msg)
	}
	if eng.lastAlterUser != "alice" || eng.lastAlterLogin || !eng.lastAlterCDB {
		t.Errorf("flags recorded user=%q login=%v createdb=%v", eng.lastAlterUser, eng.lastAlterLogin, eng.lastAlterCDB)
	}
	if eng.lastPwUser != "alice" || eng.lastPwValue != "newpw" {
		t.Errorf("password recorded user=%q value=%q", eng.lastPwUser, eng.lastPwValue)
	}

	// No password typed → SetPassword is not called; no flags → AlterUser skipped.
	eng2 := pgEngine()
	runCmd(t, alterUserCmd(eng2, "bob", false, false, false, ""))
	if eng2.lastPwUser != "" {
		t.Error("a blank password should not call SetPassword")
	}
	if eng2.lastAlterUser != "" {
		t.Error("hasFlags=false should not call AlterUser")
	}
}

// TestEditUserFormMySQLNote covers the plain-language host explainer (v2 3.2):
// MySQL forms carry it and offer no role toggles; Postgres forms omit it.
func TestEditUserFormMySQLNote(t *testing.T) {
	my := newEditUserForm(db.User{Name: "app"}, false, db.KindMySQL).View(120)
	if !strings.Contains(my, "user@'%'") {
		t.Error("MySQL edit form should explain the host part")
	}
	if strings.Contains(my, "LOGIN") {
		t.Error("MySQL has no role flags — the edit form should show no LOGIN toggle")
	}
	create := newCreateUserForm(db.KindMySQL).View(120)
	if !strings.Contains(create, "user@'%'") {
		t.Error("MySQL create form should explain the host part too")
	}
	pg := newEditUserForm(db.User{Name: "app", CanLogin: true}, true, db.KindPostgres).View(120)
	if strings.Contains(pg, "user@'%'") {
		t.Error("Postgres has no host part — the note should be absent")
	}
	if !strings.Contains(pg, "LOGIN") {
		t.Error("Postgres edit form should offer the LOGIN toggle")
	}
}

// TestGrantMatrixFlow covers v2 3.1: [g] on a user opens the matrix on its
// database picker; opening a database loads that user's current grants; space
// toggles a privilege, applying it through the engine and refreshing the view.
func TestGrantMatrixFlow(t *testing.T) {
	s, eng := newPGDashboard(t)
	// bob already holds CONNECT on appdb.
	eng.grants = map[string][]db.Privilege{
		eng.grantKey("bob", "appdb"): {db.PrivConnect},
	}

	s.focus = focusUsers
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown}) // select bob
	s, _ = press(s, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if s.mode != modeGrant || s.grant.subject != "bob" {
		t.Fatalf("[g] should open the matrix for bob; mode=%d subject=%q", s.mode, s.grant.subject)
	}
	if s.grant.step != grantStepDB || len(s.grant.items) != 2 {
		t.Fatalf("matrix should start on the db picker with 2 dbs; step=%d items=%d", s.grant.step, len(s.grant.items))
	}

	// Move to appdb (2nd db) and open it.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown})
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.grant.step != grantStepMatrix || s.grant.database != "appdb" || !s.grant.loading {
		t.Fatalf("enter should descend into the matrix for appdb, loading; step=%d db=%q loading=%v",
			s.grant.step, s.grant.database, s.grant.loading)
	}
	s = feed(s, runCmd(t, cmd)) // grantsLoadedMsg
	if s.grant.loading || !s.grant.held[db.PrivConnect] {
		t.Fatalf("loaded matrix should show CONNECT held and not be loading; held=%v", s.grant.held)
	}

	// Highlight CREATE (2nd column) and grant it with space.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown})
	s, cmd = press(s, tea.KeyPressMsg{Code: ' ', Text: " "})
	if !s.grant.loading {
		t.Fatal("toggling should mark the overlay loading")
	}
	setMsg, ok := runCmd(t, cmd).(grantSetMsg)
	if !ok || setMsg.priv != db.PrivCreate || !setMsg.grant {
		t.Fatalf("space should grant CREATE, got %#v", setMsg)
	}
	if eng.lastSetPriv != db.PrivCreate || !eng.lastSetGrant || eng.lastSetUser != "bob" || eng.lastSetDB != "appdb" {
		t.Fatalf("engine should record GRANT CREATE for bob on appdb; got user=%q db=%q priv=%q grant=%v",
			eng.lastSetUser, eng.lastSetDB, eng.lastSetPriv, eng.lastSetGrant)
	}
	// The ack toasts and refreshes the matrix from the server.
	next, reload := s.Update(setMsg)
	s = next.(dashboardScreen)
	if !strings.Contains(s.notice, "Granted CREATE") {
		t.Errorf("a granted toggle should toast; notice=%q", s.notice)
	}
	s = feed(s, runCmd(t, reload))
	if !s.grant.held[db.PrivCreate] {
		t.Errorf("after refresh CREATE should be held; held=%v", s.grant.held)
	}
}

// TestGrantMatrixEscStepsBack covers the two-level esc: from the matrix, esc
// returns to the database picker (keeping the overlay open); a second esc closes
// it entirely.
func TestGrantMatrixEscStepsBack(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.focus = focusUsers
	s, _ = press(s, tea.KeyPressMsg{Code: 'g', Text: "g"})
	s, cmd := press(s, tea.KeyPressMsg{Code: tea.KeyEnter}) // open first db
	s = feed(s, runCmd(t, cmd))
	if s.grant.step != grantStepMatrix {
		t.Fatalf("should be in the matrix; step=%d", s.grant.step)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc}) // back to picker
	if s.mode != modeGrant || s.grant.step != grantStepDB {
		t.Fatalf("esc from matrix should return to the db picker; mode=%d step=%d", s.mode, s.grant.step)
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEsc}) // close overlay
	if s.mode != modeBrowse {
		t.Errorf("esc from the picker should close the overlay; mode=%d", s.mode)
	}
}

// TestGrantCommands checks the load/apply commands hit the engine correctly.
func TestGrantCommands(t *testing.T) {
	eng := pgEngine()
	setMsg, ok := runCmd(t, setGrantCmd(eng, "bob", "appdb", db.PrivConnect, true)).(grantSetMsg)
	if !ok || !setMsg.grant || setMsg.priv != db.PrivConnect {
		t.Fatalf("setGrantCmd should ack a grant, got %#v", setMsg)
	}
	if eng.lastSetUser != "bob" || eng.lastSetDB != "appdb" || eng.lastSetPriv != db.PrivConnect || !eng.lastSetGrant {
		t.Errorf("engine recorded user=%q db=%q priv=%q grant=%v",
			eng.lastSetUser, eng.lastSetDB, eng.lastSetPriv, eng.lastSetGrant)
	}
	// loadGrantsCmd reads it back.
	lm := runCmd(t, loadGrantsCmd(eng, "bob", "appdb")).(grantsLoadedMsg)
	if len(lm.held) != 1 || lm.held[0] != db.PrivConnect {
		t.Fatalf("loadGrantsCmd should report the held privilege, got %v", lm.held)
	}
	// A revoke removes it.
	runCmd(t, setGrantCmd(eng, "bob", "appdb", db.PrivConnect, false))
	if lm := runCmd(t, loadGrantsCmd(eng, "bob", "appdb")).(grantsLoadedMsg); len(lm.held) != 0 {
		t.Errorf("after revoke nothing should be held, got %v", lm.held)
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
