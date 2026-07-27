package screens

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/tui/styles"
)

// formResult is what a key press did to a form.
type formResult int

const (
	formPending   formResult = iota // still editing
	formCanceled                    // esc
	formSubmitted                   // enter on a valid form
)

// formFieldKind distinguishes a text input from a boolean toggle.
type formFieldKind int

const (
	fieldText formFieldKind = iota
	fieldToggle
)

// formField is one row of a form: a text input or a toggle. dependsOn names a
// toggle field's key; when set, this field is only shown (and validated) while
// that toggle is on — that's how the create-database form reveals the password
// field only after "also create a user" is switched on.
type formField struct {
	key       string
	label     string
	kind      formFieldKind
	input     textinput.Model
	on        bool
	dependsOn string
}

// formModel is a small keyboard-driven form used for the admin create flows
// (database, user). It owns live validation: validate runs on every keystroke
// and returns an error (blocks submit) and/or a warning (allowed, e.g. an empty
// local-dev password). It is a component embedded by the dashboard, not a
// Screen.
type formModel struct {
	title  string
	fields []formField
	focus  int // index into fields (always a visible one)

	validate func(formModel) (errText, warnText string)
}

// newCreateDBForm builds the create-database form (Step 6.1): a name with live
// identifier validation, plus an optional "also create a matching login user and
// grant it ALL" toggle that reveals a password field.
func newCreateDBForm() formModel {
	name := textinput.New()
	name.Prompt = "› "
	name.Placeholder = "my_app"
	name.Focus()

	pass := textinput.New()
	pass.Prompt = "› "
	pass.Placeholder = "(optional — local dev)"
	pass.EchoMode = textinput.EchoPassword

	f := formModel{
		title: "Create database",
		fields: []formField{
			{key: "name", label: "Database name", kind: fieldText, input: name},
			{key: "withUser", label: "Also create a matching user and grant it ALL", kind: fieldToggle},
			{key: "password", label: "User password", kind: fieldText, input: pass, dependsOn: "withUser"},
		},
		validate: validateCreateDB,
	}
	return f
}

// newCreateUserForm builds the create-user form (Step 6.4): a name with live
// validation, a masked password, and a confirm field that must match. An empty
// password is warned but allowed (this is local dev).
func newCreateUserForm() formModel {
	name := textinput.New()
	name.Prompt = "› "
	name.Placeholder = "app_user"
	name.Focus()

	pass := textinput.New()
	pass.Prompt = "› "
	pass.Placeholder = "(optional — local dev)"
	pass.EchoMode = textinput.EchoPassword

	confirm := textinput.New()
	confirm.Prompt = "› "
	confirm.EchoMode = textinput.EchoPassword

	return formModel{
		title: "Create user",
		fields: []formField{
			{key: "name", label: "User name", kind: fieldText, input: name},
			{key: "password", label: "Password", kind: fieldText, input: pass},
			{key: "confirm", label: "Confirm password", kind: fieldText, input: confirm},
		},
		validate: validateCreateUser,
	}
}

func (f formModel) Init() tea.Cmd { return textinput.Blink }

// update handles one key: esc cancels; enter submits when validation passes;
// tab/↑/↓ move between visible fields; space toggles a focused toggle; anything
// else edits the focused text input.
func (f formModel) update(msg tea.KeyPressMsg) (formModel, formResult, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return f, formCanceled, nil
	case "enter":
		if errText, _ := f.validate(f); errText == "" {
			return f, formSubmitted, nil
		}
		return f, formPending, nil
	case "tab", "down":
		f.focusNext(+1)
		return f, formPending, f.syncFocus()
	case "shift+tab", "up":
		f.focusNext(-1)
		return f, formPending, f.syncFocus()
	case " ", "space":
		if f.fields[f.focus].kind == fieldToggle {
			f.fields[f.focus].on = !f.fields[f.focus].on
			return f, formPending, f.syncFocus()
		}
	}
	if f.fields[f.focus].kind == fieldText {
		var cmd tea.Cmd
		f.fields[f.focus].input, cmd = f.fields[f.focus].input.Update(msg)
		return f, formPending, cmd
	}
	return f, formPending, nil
}

// focusNext moves focus to the next visible field in the given direction,
// skipping fields hidden by an off toggle.
func (f *formModel) focusNext(dir int) {
	n := len(f.fields)
	for range n {
		f.focus = (f.focus + dir + n) % n
		if f.visible(f.focus) {
			return
		}
	}
}

// syncFocus focuses the input under the cursor and blurs the rest, returning the
// blink command for the newly focused input.
func (f *formModel) syncFocus() tea.Cmd {
	var cmd tea.Cmd
	for i := range f.fields {
		if f.fields[i].kind != fieldText {
			continue
		}
		if i == f.focus {
			cmd = f.fields[i].input.Focus()
		} else {
			f.fields[i].input.Blur()
		}
	}
	return cmd
}

// visible reports whether field i is currently shown (its dependsOn toggle, if
// any, is on).
func (f formModel) visible(i int) bool {
	dep := f.fields[i].dependsOn
	if dep == "" {
		return true
	}
	return f.toggle(dep)
}

// value returns the trimmed text of a text field by key.
func (f formModel) value(k string) string {
	for _, fld := range f.fields {
		if fld.key == k && fld.kind == fieldText {
			return strings.TrimSpace(fld.input.Value())
		}
	}
	return ""
}

// toggle returns a toggle field's state by key.
func (f formModel) toggle(k string) bool {
	for _, fld := range f.fields {
		if fld.key == k && fld.kind == fieldToggle {
			return fld.on
		}
	}
	return false
}

func (f formModel) View(width int) string {
	lines := []string{styles.Title.Render(f.title), ""}
	for i, fld := range f.fields {
		if !f.visible(i) {
			continue
		}
		marker := "  "
		label := styles.Hint.Render(fld.label)
		if i == f.focus {
			marker = styles.Selected.Render("▸ ")
			label = styles.Subtitle.Render(fld.label)
		}
		switch fld.kind {
		case fieldText:
			lines = append(lines, marker+label, "  "+fld.input.View())
		case fieldToggle:
			box := "[ ]"
			if fld.on {
				box = styles.SuccessText.Render("[x]")
			}
			lines = append(lines, marker+box+" "+label)
		}
	}

	errText, warnText := f.validate(f)
	lines = append(lines, "")
	switch {
	case errText != "":
		lines = append(lines, styles.DangerText.Render("✗ "+errText))
	case warnText != "":
		lines = append(lines, styles.WarningText.Render("⚠ "+warnText))
	default:
		lines = append(lines, styles.SuccessText.Render("✓ ready"))
	}
	lines = append(lines, "", styles.Hint.Render("tab next · space toggle · enter create · esc cancel"))
	return styles.Screen.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (f formModel) Help() []key.Binding {
	return []key.Binding{Keys.Select, Keys.Back}
}

// --- validators ---

// validateCreateDB checks the database name (and, when the toggle is on, notes a
// weak/empty user password as a warning, not an error).
func validateCreateDB(f formModel) (errText, warnText string) {
	name := f.value("name")
	if name == "" {
		return "enter a database name", ""
	}
	if err := db.ValidateIdent(name); err != nil {
		return identReason(err), ""
	}
	if f.toggle("withUser") && f.value("password") == "" {
		return "", "no password set for the new user (fine for local dev)"
	}
	return "", ""
}

// validateCreateUser checks the user name, that the two password fields agree,
// and warns on an empty password.
func validateCreateUser(f formModel) (errText, warnText string) {
	name := f.value("name")
	if name == "" {
		return "enter a user name", ""
	}
	if err := db.ValidateIdent(name); err != nil {
		return identReason(err), ""
	}
	if f.value("password") != f.value("confirm") {
		return "passwords don't match", ""
	}
	if f.value("password") == "" {
		return "", "empty password (fine for local dev)"
	}
	return "", ""
}

// identReason pulls the plain-language detail out of the db package's typed
// invalid-name error for inline display.
func identReason(err error) string {
	var de *db.DBError
	if de = asDBError(err); de.Detail != "" {
		return de.Detail
	}
	return "invalid name"
}
