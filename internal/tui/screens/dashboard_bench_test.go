package screens

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
	"github.com/Ibnu-Afdel/dbwiz/internal/docker"
)

// bigResult builds an n-row, 5-column result for the rendering benchmarks.
func bigResult(n int) db.Result {
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{
			fmt.Sprintf("%d", i),
			fmt.Sprintf("user_%d@example.com", i),
			fmt.Sprintf("Name %d", i),
			"2026-07-26T00:00:00Z",
			"some medium-length descriptive text value",
		}
	}
	return db.Result{
		Columns:  []string{"id", "email", "name", "created_at", "note"},
		Rows:     rows,
		Duration: 12 * time.Millisecond,
	}
}

// BenchmarkRenderQueryResult10k measures one frame of the results grid for a
// 10k-row query result — the Phase 9.3 "renders/scrolls smoothly" target. Each
// op is what the UI does per keypress while scrolling.
func BenchmarkRenderQueryResult10k(b *testing.B) {
	s, _ := newBenchDashboard()
	s.results = resultsQuery
	s.queryResult = bigResult(10_000)
	s.resultOffset = 5_000 // mid-scroll, worst case for the window slice

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = s.renderQueryResult(80, 30)
	}
}

// newBenchDashboard builds a sized dashboard without the test-only helpers that
// take a *testing.T.
func newBenchDashboard() (dashboardScreen, *fakeEngine) {
	eng := pgEngine()
	s := NewDashboard(eng, db.Target{Host: "127.0.0.1", Port: 5432, Database: "postgres"},
		docker.Container{Name: "bench-pg", Engine: docker.EnginePostgres}).(dashboardScreen)
	next, _ := s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(dashboardScreen), eng
}
