package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
	"github.com/Ibnu-Afdel/dbwiz/internal/health"
)

func sampleReport(ok bool) health.Report {
	checks := []health.Check{
		{Name: "Docker daemon", Status: health.OK, Detail: "reachable"},
		{Name: "Container running", Status: health.OK, Detail: "up"},
	}
	if ok {
		checks = append(checks, health.Check{Name: "Query round-trip", Status: health.OK, Detail: "SELECT 1"})
	} else {
		checks = append(checks, health.Check{Name: "Authentication", Status: health.Fail, Detail: "bad password"})
	}
	return health.Report{Checks: checks}
}

// TestDoctorRendersPass shows each check and an all-clear summary.
func TestDoctorRendersPass(t *testing.T) {
	s := NewDoctor(docker.Container{Name: "postgres18"})
	scr, _ := s.Update(doctorDoneMsg{report: sampleReport(true)})
	got := scr.View(100, 30)
	for _, want := range []string{"postgres18", "Docker daemon", "Query round-trip", "All checks passed"} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor view missing %q:\n%s", want, got)
		}
	}
}

// TestDoctorRendersFail surfaces a failing check and a problem summary.
func TestDoctorRendersFail(t *testing.T) {
	scr, _ := NewDoctor(docker.Container{Name: "pg"}).Update(doctorDoneMsg{report: sampleReport(false)})
	got := scr.View(100, 30)
	if !strings.Contains(got, "Authentication") || !strings.Contains(got, "Something's wrong") {
		t.Errorf("expected a failure summary:\n%s", got)
	}
}

// TestDoctorRetry re-runs the check after it finished.
func TestDoctorRetry(t *testing.T) {
	scr, _ := NewDoctor(docker.Container{Name: "pg"}).Update(doctorDoneMsg{report: sampleReport(true)})
	scr2, cmd := scr.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if !scr2.(doctorScreen).running {
		t.Error("r should restart the checks")
	}
	if cmd == nil {
		t.Error("r should fire the check command")
	}
}

// TestDoctorBack pops.
func TestDoctorBack(t *testing.T) {
	scr, _ := NewDoctor(docker.Container{Name: "pg"}).Update(doctorDoneMsg{report: sampleReport(true)})
	_, cmd := scr.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if _, ok := runCmd(t, cmd).(PopMsg); !ok {
		t.Error("esc should pop")
	}
}

// TestDoctorRouteEmpty routes to an error screen when nothing is detected.
func TestDoctorRouteEmpty(t *testing.T) {
	msg := runCmd(t, doctorRoute(nil))
	push, ok := msg.(PushMsg)
	if !ok {
		t.Fatalf("want PushMsg, got %T", msg)
	}
	if _, ok := push.Screen.(errorScreen); !ok {
		t.Errorf("want errorScreen, got %T", push.Screen)
	}
}

// TestDoctorRoutePicksContainer routes to the doctor for a detected container.
func TestDoctorRoutePicksContainer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // no pinned target
	msg := runCmd(t, doctorRoute([]docker.Container{stopped("pg"), running("my")}))
	push := msg.(PushMsg)
	d, ok := push.Screen.(doctorScreen)
	if !ok {
		t.Fatalf("want doctorScreen, got %T", push.Screen)
	}
	// Prefers a running container when nothing is pinned.
	if d.container.Name != "my" {
		t.Errorf("expected the running container, got %q", d.container.Name)
	}
}
