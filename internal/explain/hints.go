package explain

import (
	"fmt"
	"strings"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// Hint is one plain-language observation about a plan: what the engine is doing
// that costs time, and what a person can actually do about it. Step names the
// plan step it came from, so a hint can be matched back to the tree above it.
type Hint struct {
	Step   string `json:"step"`
	Issue  string `json:"issue"`
	Advice string `json:"advice"`
}

// hintScanRows is how big a table has to be before a full scan is worth
// mentioning. Scanning a twelve-row lookup table is the right plan, and warning
// about it would teach the wrong lesson.
const hintScanRows = 1000

// hintEstimateFactor is how far off an estimate has to be before it counts as
// stale statistics rather than ordinary imprecision.
const hintEstimateFactor = 10

// hintEstimateFloor keeps the estimate rule off small numbers, where a 10×
// difference is 3 rows versus 30 and means nothing.
const hintEstimateFloor = 100

// Hints reads a plan and reports what is expensive about it.
//
// It is a pure function over the model — every engine-specific fact it needs was
// normalised into a Node.Notes phrase by the parser, which is what keeps this
// from becoming a fourth dialect parser. Hints come back in tree order so they
// line up with the rendered plan above them.
func Hints(p Plan) []Hint {
	var hints []Hint
	p.Root.Walk(func(n *Node) {
		hints = append(hints, nodeHints(p, n)...)
	})
	return hints
}

// nodeHints applies every rule to one step.
func nodeHints(p Plan, n *Node) []Hint {
	var hints []Hint

	if h, ok := fullScanHint(n); ok {
		hints = append(hints, h)
	}
	if h, ok := estimateHint(p, n); ok {
		hints = append(hints, h)
	}
	if n.Has(NoteDiskSort) {
		hints = append(hints, Hint{
			Step:   n.Label(),
			Issue:  "The sort didn't fit in memory, so the engine spilled it to disk.",
			Advice: strings.TrimSpace("Sort fewer rows (a tighter WHERE, or a LIMIT), or add an index in the sort order so the rows arrive sorted. " + sortMemoryKnob(p.Kind)),
		})
	}
	if n.Has(NoteFilesort) {
		hints = append(hints, Hint{
			Step:   n.Label(),
			Issue:  "The rows are sorted after being read, rather than arriving in order.",
			Advice: "An index whose leading columns match the ORDER BY lets the engine read them already sorted and skip this step.",
		})
	}
	if n.Has(NoteTempTable) {
		hints = append(hints, Hint{
			Step:   n.Label(),
			Issue:  "An intermediate table is built to hold the results while they're ordered or grouped.",
			Advice: "An index covering the GROUP BY or ORDER BY columns usually removes the need for it.",
		})
	}
	return hints
}

// fullScanHint fires when a step reads every row of a relation big enough for it
// to matter. Row counts are unknown on SQLite — its plan carries no numbers at
// all — so there the shape of the plan is the whole signal and the hint fires on
// the scan alone.
func fullScanHint(n *Node) (Hint, bool) {
	if !n.Has(NoteFullScan) {
		return Hint{}, false
	}
	rows := n.ActRows
	if rows < 0 {
		rows = n.EstRows
	}
	if rows >= 0 && rows < hintScanRows {
		return Hint{}, false
	}

	what := "the table"
	if n.On != "" {
		what = n.On
	}
	issue := fmt.Sprintf("Every row of %s is read.", what)
	if rows >= 0 {
		issue = fmt.Sprintf("Every row of %s is read (%s rows).", what, humanInt(rows))
	}
	return Hint{
		Step:   n.Label(),
		Issue:  issue,
		Advice: "If this step filters or joins on a column, an index on that column lets the engine jump straight to the matching rows instead of reading all of them.",
	}, true
}

// estimateHint fires when a measured plan shows the planner was badly wrong about
// how many rows it would get. That is the usual sign of stale statistics, and it
// matters more than it looks: every choice above this step was made from the
// wrong number.
func estimateHint(p Plan, n *Node) (Hint, bool) {
	if !p.Analyzed || n.EstRows < 0 || n.ActRows < 0 {
		return Hint{}, false
	}
	est, act := n.EstRows, n.ActRows
	if act < hintEstimateFloor && est < hintEstimateFloor {
		return Hint{}, false
	}
	high, low := est, act
	if act > est {
		high, low = act, est
	}
	if low <= 0 {
		low = 1
	}
	if high/low < hintEstimateFactor {
		return Hint{}, false
	}

	return Hint{
		Step:   n.Label(),
		Issue:  fmt.Sprintf("The planner expected %s rows here but got %s.", humanInt(est), humanInt(act)),
		Advice: "The table's statistics are out of date, so every choice made above this step was based on the wrong number. " + analyzeAdvice(p.Kind, n.On),
	}, true
}

// analyzeAdvice spells the statistics-refresh command for the engine in hand.
func analyzeAdvice(kind db.Kind, table string) string {
	if table == "" {
		table = "the table"
	}
	switch kind {
	case db.KindMySQL, db.KindMariaDB:
		return fmt.Sprintf("Run `ANALYZE TABLE %s` to refresh them.", table)
	case db.KindSQLite:
		return "Run `ANALYZE` to refresh them."
	default:
		return fmt.Sprintf("Run `ANALYZE %s` to refresh them.", table)
	}
}

// sortMemoryKnob names the server setting that governs sort memory, which differs
// per engine and is the one lever that isn't a query change.
func sortMemoryKnob(kind db.Kind) string {
	switch kind {
	case db.KindMySQL, db.KindMariaDB:
		return "On the server side, sort_buffer_size is the setting that governs it."
	case db.KindSQLite:
		return ""
	default:
		return "On the server side, work_mem is the setting that governs it."
	}
}
