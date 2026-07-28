package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestBackupFlow covers v3 2.4: [B] opens the dump/restore screen, the choose →
// file → running phases advance, and the file is prefilled for a dump.
func TestBackupFlow(t *testing.T) {
	s, _ := previewing(t) // pg engine, container "fawz-postgres", currentDB "postgres"

	s, _ = press(s, tea.KeyPressMsg{Code: 'B', Text: "B"})
	if s.mode != modeBackup || s.backup.phase != backupChoose {
		t.Fatalf("B should open the backup screen at the choose step; mode=%d phase=%d", s.mode, s.backup.phase)
	}
	if s.backup.database != "postgres" {
		t.Errorf("backup targets %q, want postgres", s.backup.database)
	}

	// Default is dump; enter advances to the file step with a prefilled name.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.backup.phase != backupFile {
		t.Fatalf("enter should go to the file step, phase=%d", s.backup.phase)
	}
	if !strings.HasPrefix(s.backup.input.Value(), "postgres-") || !strings.HasSuffix(s.backup.input.Value(), ".sql") {
		t.Errorf("dump filename should be prefilled, got %q", s.backup.input.Value())
	}

	// Running the dump enters the running phase with work in flight.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.backup.phase != backupRunning || !s.working {
		t.Fatalf("enter should start the dump; phase=%d working=%v", s.backup.phase, s.working)
	}

	// The outcome is shown until dismissed.
	s = feed(s, backupDoneMsg{summary: "Dumped postgres → /tmp/x.sql"})
	if s.backup.phase != backupDone || s.working {
		t.Fatalf("done should end the run; phase=%d working=%v", s.backup.phase, s.working)
	}
	if !strings.Contains(s.View(120, 40), "Dumped postgres") {
		t.Error("the result should be shown")
	}
	// Any key closes it.
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.mode != modeBrowse {
		t.Errorf("a key should close the result, mode=%d", s.mode)
	}
}

// TestBackupRestoreToggle covers switching to the restore action.
func TestBackupRestoreToggle(t *testing.T) {
	s, _ := previewing(t)
	s, _ = press(s, tea.KeyPressMsg{Code: 'B', Text: "B"})
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyDown}) // toggle to restore
	if !s.backup.restore {
		t.Fatal("down should select restore")
	}
	s, _ = press(s, tea.KeyPressMsg{Code: tea.KeyEnter})
	if s.backup.phase != backupFile || s.backup.input.Value() != "" {
		t.Errorf("restore file step should start empty, got %q", s.backup.input.Value())
	}
}

// TestBackupUnsupportedTarget covers the guard: no container to exec into.
func TestBackupUnsupportedTarget(t *testing.T) {
	s, _ := previewing(t)
	s.container.Name = "" // e.g. a manual/SQLite target

	s, _ = press(s, tea.KeyPressMsg{Code: 'B', Text: "B"})
	if s.mode == modeBackup {
		t.Fatal("backup must not open without a container")
	}
	if !s.noticeErr || !strings.Contains(s.notice, "Docker") {
		t.Errorf("expected a Docker-only notice, got %q", s.notice)
	}
}
