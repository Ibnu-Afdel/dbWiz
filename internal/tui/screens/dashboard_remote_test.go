package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/remote"
)

// TestManualIsRemote classifies which manual targets count as non-local.
func TestManualIsRemote(t *testing.T) {
	spec := &remote.SSHSpec{User: "u", Host: "h", Port: 22}
	cases := []struct {
		host string
		ssh  *remote.SSHSpec
		want bool
	}{
		{"127.0.0.1", nil, false},
		{"localhost", nil, false},
		{"::1", nil, false},
		{"", nil, false},
		{"10.0.0.5", nil, true},
		{"db.example.com", nil, true},
		{"127.0.0.1", spec, true}, // an SSH tunnel is always remote
	}
	for _, c := range cases {
		if got := manualIsRemote(c.host, c.ssh); got != c.want {
			t.Errorf("manualIsRemote(%q, ssh=%v) = %v, want %v", c.host, c.ssh != nil, got, c.want)
		}
	}
}

// TestRemoteBadgeInStatusBar a remote dashboard wears the REMOTE badge; a local
// one does not.
func TestRemoteBadgeInStatusBar(t *testing.T) {
	s, _ := newPGDashboard(t)
	if strings.Contains(s.View(120, 40), "REMOTE") {
		t.Error("a local dashboard should not show REMOTE")
	}
	s.remote = true
	if !strings.Contains(s.View(120, 40), "REMOTE") {
		t.Error("a remote dashboard should show the REMOTE badge")
	}
}

// TestRemoteConfirmSQLRequiresYes on a remote target, the truncate/delete confirm
// is inert on Enter until "yes" is typed; a local target runs on a single Enter.
func TestRemoteConfirmSQLRequiresYes(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.remote = true
	s.confirmSQL = confirmSQLState{
		title:    "Truncate t?",
		database: "postgres",
		sql:      `TRUNCATE TABLE "t"`,
		notice:   "Truncated t",
		remote:   true,
	}
	s.mode = modeConfirmSQL

	// Enter with no acknowledgement is inert.
	s2, cmd := s.updateConfirmSQL(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || s2.mode != modeConfirmSQL {
		t.Fatal("enter should be inert on a remote target before typing yes")
	}
	if !strings.Contains(s2.confirmSQLView(120), "type yes") {
		t.Error("the view should tell the user to type yes")
	}

	// Type y-e-s, then Enter runs.
	for _, r := range "yes" {
		s2, _ = s2.updateConfirmSQL(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if s2.confirmSQL.ack != "yes" {
		t.Fatalf("ack = %q, want yes", s2.confirmSQL.ack)
	}
	s3, cmd := s2.updateConfirmSQL(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || s3.mode != modeBrowse || !s3.working {
		t.Fatalf("enter after yes should run; mode=%d working=%v cmd=%v", s3.mode, s3.working, cmd != nil)
	}
}

// TestLocalConfirmSQLRunsOnEnter a local confirm still fires on a single Enter (no
// regression from the remote gate).
func TestLocalConfirmSQLRunsOnEnter(t *testing.T) {
	s, _ := newPGDashboard(t)
	s.confirmSQL = confirmSQLState{title: "Truncate t?", database: "postgres", sql: `TRUNCATE TABLE "t"`, notice: "ok"}
	s.mode = modeConfirmSQL
	s2, cmd := s.updateConfirmSQL(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || s2.mode != modeBrowse || !s2.working {
		t.Fatalf("a local confirm should run on enter; mode=%d working=%v", s2.mode, s2.working)
	}
}

// TestRemoteDropConfirmShowsBanner the type-the-name drop confirm carries the
// REMOTE banner when the target is non-local.
func TestRemoteDropConfirmShowsBanner(t *testing.T) {
	c := newConfirm("Delete database", "drops it", "app").onRemote(true)
	if !strings.Contains(c.View(80), "REMOTE") {
		t.Error("a remote drop confirm should show the REMOTE banner")
	}
	if strings.Contains(newConfirm("Delete database", "drops it", "app").View(80), "REMOTE") {
		t.Error("a local drop confirm should not show REMOTE")
	}
}
