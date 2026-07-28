package screens

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// openCreateDBMsg asks the dashboard to open the create-database form. It is how
// the home "Create new database" route (Step 6.7) triggers the form once the
// dashboard is live, without the home screen reaching into dashboard state.
type openCreateDBMsg struct{}

// openCreate opens the create form appropriate to the focused pane: a database
// on the databases pane, a user on the users pane.
func (s dashboardScreen) openCreate() (dashboardScreen, tea.Cmd) {
	switch s.focus {
	case focusDatabases:
		return s.openCreateDB()
	case focusUsers:
		return s.openCreateUser()
	}
	return s, nil
}

// openCreateDB opens the create-database form (Step 6.1). It is a no-op on
// engines without multiple databases.
func (s dashboardScreen) openCreateDB() (dashboardScreen, tea.Cmd) {
	if !s.caps.MultipleDatabases {
		return s, nil
	}
	s.mode, s.formPurpose, s.notice = modeForm, purposeCreateDB, ""
	s.form = newCreateDBForm()
	return s, s.form.Init()
}

// openCreateUser opens the create-user form (Step 6.4). It is a no-op on engines
// without a user concept.
func (s dashboardScreen) openCreateUser() (dashboardScreen, tea.Cmd) {
	if !s.caps.Users {
		return s, nil
	}
	s.mode, s.formPurpose, s.notice = modeForm, purposeCreateUser, ""
	s.form = newCreateUserForm(s.engine.Kind())
	return s, s.form.Init()
}

// openEditUser opens the edit-user form for the selected user (v2 3.2): role
// flags where the engine has them, plus a password change. It is a no-op off the
// users pane or on engines without a user concept.
func (s dashboardScreen) openEditUser() (dashboardScreen, tea.Cmd) {
	if !s.caps.Users || s.focus != focusUsers {
		return s, nil
	}
	u, ok := s.selectedUser()
	if !ok {
		return s, nil
	}
	s.mode, s.formPurpose, s.notice = modeForm, purposeEditUser, ""
	s.editUserName = u.Name
	s.form = newEditUserForm(u, s.caps.RoleFlags, s.engine.Kind())
	return s, s.form.Init()
}

// openDelete opens a type-the-name confirm for the selected database or user
// (Steps 6.3 / 6.5).
func (s dashboardScreen) openDelete() (dashboardScreen, tea.Cmd) {
	switch s.focus {
	case focusDatabases:
		d, ok := s.selectedDatabase()
		if !ok {
			return s, nil
		}
		s.confirmKind, s.confirmTarget = confirmDropDB, d.Name
		s.confirm = newConfirm("Delete database",
			fmt.Sprintf("This permanently drops %q and everything in it. This cannot be undone.", d.Name),
			d.Name)
		s.mode, s.notice = modeConfirm, ""
		return s, s.confirm.Init()
	case focusUsers:
		u, ok := s.selectedUser()
		if !ok {
			return s, nil
		}
		s.confirmKind, s.confirmTarget = confirmDropUser, u.Name
		s.confirm = newConfirm("Delete user",
			fmt.Sprintf("This permanently drops the user %q.", u.Name),
			u.Name)
		s.mode, s.notice = modeConfirm, ""
		return s, s.confirm.Init()
	}
	return s, nil
}

// openGrant opens the grant/revoke picker for the selected user (Step 6.6).
func (s dashboardScreen) openGrant() (dashboardScreen, tea.Cmd) {
	if !s.caps.Grants || s.focus != focusUsers {
		return s, nil
	}
	u, ok := s.selectedUser()
	if !ok {
		return s, nil
	}
	names := make([]string, len(s.databases))
	for i, d := range s.databases {
		names[i] = d.Name
	}
	s.grant, s.mode, s.notice = newGrant(u.Name, s.engine.DatabasePrivileges(), names), modeGrant, ""
	return s, nil
}

// handleOverlayKey routes a key to the live overlay (form/confirm/grant) and
// interprets its result: cancel returns to browsing, accept runs the matching
// async mutation.
func (s dashboardScreen) handleOverlayKey(msg tea.KeyPressMsg) (dashboardScreen, tea.Cmd) {
	switch s.mode {
	case modeForm:
		var res formResult
		var cmd tea.Cmd
		s.form, res, cmd = s.form.update(msg)
		switch res {
		case formCanceled:
			s.mode = modeBrowse
			return s, nil
		case formSubmitted:
			return s.submitForm()
		}
		return s, cmd
	case modeConfirm:
		var res confirmResult
		var cmd tea.Cmd
		s.confirm, res, cmd = s.confirm.update(msg)
		switch res {
		case confirmAborted:
			s.mode = modeBrowse
			return s, nil
		case confirmAccepted:
			return s.runConfirmed()
		}
		return s, cmd
	case modeGrant:
		var res grantResult
		s.grant, res = s.grant.update(msg)
		switch res {
		case grantCanceled:
			s.mode = modeBrowse
			return s, nil
		case grantPickDB:
			// Descended into the matrix — load this user's current grants for the
			// chosen database. The overlay is showing its own loading state.
			return s, loadGrantsCmd(s.engine, s.grant.subject, s.grant.database)
		case grantToggle:
			return s, setGrantCmd(s.engine, s.grant.subject, s.grant.database, s.grant.pendingPriv, s.grant.pendingGrant)
		}
		return s, nil
	case modeExport:
		var res exportResult
		s.export, res = s.export.update(msg)
		switch res {
		case exportCanceled:
			s.mode = modeBrowse
			return s, nil
		case exportChosen:
			s.mode = modeBrowse
			cols, rows := s.currentGrid()
			return s, exportChosenCmd(s.export.choice, s.exportBase(), s.export.dir, cols, rows)
		}
		return s, nil
	case modeComplete:
		var res completeResult
		var cmd tea.Cmd
		s.completeList, res, cmd = s.completeList.update(msg)
		switch res {
		case completeCanceled:
			s.mode = modeBrowse
			return s, s.editor.Focus()
		case completeAccepted:
			if it, ok := s.completeList.selected(); ok {
				return s.acceptCompletion(it.text)
			}
			s.mode = modeBrowse
			return s, s.editor.Focus()
		}
		return s, cmd
	case modeCell:
		return s.updateCell(msg)
	case modeEditCell:
		return s.updateCellEdit(msg)
	case modeDeleteRow:
		return s.updateDeleteRow(msg)
	case modeInsertRow:
		return s.updateInsertRow(msg)
	case modeSaved:
		var res savedResult
		var cmd tea.Cmd
		s.savedList, res, cmd = s.savedList.update(msg)
		switch res {
		case savedCanceled:
			s.mode = modeBrowse
			return s, nil
		case savedPicked:
			s.mode = modeBrowse
			if it, ok := s.savedList.selected(); ok {
				// Load into the editor, focused, so the user tweaks before running —
				// picking never auto-runs, exactly like the history overlay.
				s.editor.SetValue(it.sql)
				s.editor.MoveToEnd()
				s.focus = focusEditor
				return s, s.syncEditorFocus()
			}
			return s, nil
		case savedDelete:
			if it, ok := s.savedList.selected(); ok {
				key := s.historyKey
				if it.scope == scopeGlobal {
					key = ""
				}
				// Delete off the Update goroutine; the reply carries the refreshed list
				// so the row disappears (or the overlay closes if it was the last one).
				return s, deleteSavedCmd(key, it.name, s.historyKey)
			}
			return s, nil
		}
		return s, cmd
	case modeHistory:
		var res historyResult
		var cmd tea.Cmd
		s.historyList, res, cmd = s.historyList.update(msg)
		switch res {
		case historyCanceled:
			s.mode = modeBrowse
			return s, nil
		case historyPicked:
			s.mode = modeBrowse
			if sql, ok := s.historyList.selected(); ok {
				// Load into the editor, focused, so the user tweaks before running —
				// picking never auto-runs (confirmed with Ibnu).
				s.editor.SetValue(sql)
				s.editor.MoveToEnd()
				s.focus = focusEditor
				return s, s.syncEditorFocus()
			}
			return s, nil
		}
		return s, cmd
	}
	return s, nil
}

// submitForm runs the command a validated form represents. Saving a query is a
// local write handled inline (no server round-trip, so no "working…" spinner);
// the create/edit purposes flip working on and dispatch an async mutation.
func (s dashboardScreen) submitForm() (dashboardScreen, tea.Cmd) {
	s.mode = modeBrowse
	if s.formPurpose == purposeSaveQuery {
		return s.saveQueryFromForm()
	}
	s.working = true
	switch s.formPurpose {
	case purposeCreateDB:
		return s, tea.Batch(s.spinner.Tick, createDatabaseCmd(
			s.engine, s.form.value("name"), s.form.toggle("withUser"), s.form.value("password")))
	case purposeCreateUser:
		return s, tea.Batch(s.spinner.Tick, createUserCmd(
			s.engine, s.form.value("name"), s.form.value("password")))
	case purposeEditUser:
		return s, tea.Batch(s.spinner.Tick, alterUserCmd(
			s.engine, s.editUserName, s.caps.RoleFlags,
			s.form.toggle("canLogin"), s.form.toggle("createDB"), s.form.value("password")))
	}
	return s, nil
}

// runConfirmed runs the destructive command an accepted confirm represents.
func (s dashboardScreen) runConfirmed() (dashboardScreen, tea.Cmd) {
	s.mode, s.working = modeBrowse, true
	switch s.confirmKind {
	case confirmDropDB:
		return s, tea.Batch(s.spinner.Tick, dropDatabaseCmd(s.engine, s.confirmTarget))
	case confirmDropUser:
		return s, tea.Batch(s.spinner.Tick, dropUserCmd(s.engine, s.confirmTarget))
	}
	return s, nil
}

// applyAdminDone records the success toast and reloads whichever lists the
// mutation changed.
func (s dashboardScreen) applyAdminDone(msg adminDoneMsg) (dashboardScreen, tea.Cmd) {
	s.working = false
	s.notice, s.noticeErr = msg.notice, false
	s.mode = modeBrowse

	var cmds []tea.Cmd
	if msg.reloadDatabases && s.caps.MultipleDatabases {
		s.dbLoading = true
		cmds = append(cmds, loadDatabasesCmd(s.engine))
	}
	if msg.reloadUsers && s.caps.Users {
		s.usersLoading = true
		cmds = append(cmds, loadUsersCmd(s.engine))
	}
	if len(cmds) == 0 {
		return s, nil
	}
	cmds = append(cmds, s.spinner.Tick)
	return s, tea.Batch(cmds...)
}

// adminErrText condenses a typed failure into one line for the toast — Title,
// plus Detail when it adds something.
func adminErrText(e *db.DBError) string {
	if e == nil {
		return "operation failed"
	}
	if e.Detail != "" {
		return e.Title + " — " + e.Detail
	}
	return e.Title
}
