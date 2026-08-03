// Package explain turns an engine's query plan into one readable, engine-neutral
// tree (v4 Phase 2). It answers "why is this query slow?" without the user having
// to learn three different EXPLAIN dialects.
//
// Like internal/schema, it is a consumer of the engine abstraction rather than
// part of it: an EXPLAIN is just a statement, so Explain runs through the
// Query method every engine already has and adds no interface method. The
// dependency direction is tui/cmd → explain → db; it never imports docker or tui.
//
// What differs from schema is that the output is engine-shaped as well as the
// input — Postgres answers in JSON, MySQL in JSON or its own indented text,
// SQLite in a flat row set with parent pointers. So this package is one neutral
// model plus one parser per dialect, each filling the same Node.
package explain

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// Unknown marks a measurement the engine didn't report. Row counts and costs use
// it rather than zero, because "zero rows" and "the engine never said" are
// different facts — SQLite, for instance, reports no numbers at all.
const Unknown = -1

// Notes are engine-neutral phrases a parser attaches to a node when the plan says
// something a hint can act on. Normalising them here is what keeps Hints a pure
// function over the model instead of a fourth parser: "Seq Scan" on Postgres,
// access_type ALL on MySQL and SCAN on SQLite all become NoteFullScan.
const (
	NoteFullScan  = "full scan"       // reads every row of the relation
	NoteDiskSort  = "sorted on disk"  // the sort didn't fit in memory
	NoteFilesort  = "filesort"        // MySQL sorted without using an index
	NoteTempTable = "temporary table" // an intermediate table was materialised
)

// Node is one step of a query plan. Op is what the engine does ("Full scan",
// "Index lookup", "Hash Join"), On is the relation it does it to, and Detail is
// the one-line specific (the index used, the condition applied).
//
// EstRows is the planner's guess and ActRows what actually came back; the gap
// between them is the single most useful number in a measured plan, which is why
// both are kept rather than collapsed.
type Node struct {
	Op       string
	On       string
	Detail   string
	EstRows  int64         // planner estimate, or Unknown
	ActRows  int64         // measured, or Unknown outside --analyze
	Cost     float64       // planner cost in engine units, or Unknown
	Time     time.Duration // measured time for one loop of this step
	Loops    int64         // how many times the step ran, or Unknown
	Notes    []string
	Children []*Node
}

// Label is the node as a person would say it: "Full scan on users".
func (n *Node) Label() string {
	if n == nil {
		return ""
	}
	if n.On == "" {
		return n.Op
	}
	return n.Op + " on " + n.On
}

// Has reports whether the parser attached this note.
func (n *Node) Has(note string) bool {
	if n == nil {
		return false
	}
	return slices.Contains(n.Notes, note)
}

// Walk calls fn on this node and every descendant, parents before children.
func (n *Node) Walk(fn func(*Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// Plan is a whole captured plan. Analyzed records whether the statement was
// actually run (measured numbers) or merely planned (estimates), because every
// number in the tree means something different depending on which it was.
type Plan struct {
	Kind      db.Kind
	Statement string // the user's statement, not the EXPLAIN wrapper
	Command   string // the EXPLAIN statement DBWiz actually ran
	Analyzed  bool
	Root      *Node
	Planning  time.Duration
	Execution time.Duration
	Raw       string // the engine's own output, verbatim, for --raw
}

// Nodes returns every node in the tree, parents before children.
func (p Plan) Nodes() []*Node {
	var out []*Node
	p.Root.Walk(func(n *Node) { out = append(out, n) })
	return out
}

// --- JSON ---
//
// The model marshals through shadow structs so that unknown measurements are
// omitted rather than emitted as -1, and durations read as milliseconds rather
// than as a nanosecond integer. The JSON report is read by people about as often
// as by scripts.

type nodeJSON struct {
	Op       string   `json:"op"`
	On       string   `json:"on,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	EstRows  *int64   `json:"est_rows,omitempty"`
	ActRows  *int64   `json:"actual_rows,omitempty"`
	Cost     *float64 `json:"cost,omitempty"`
	TimeMS   *float64 `json:"time_ms,omitempty"`
	Loops    *int64   `json:"loops,omitempty"`
	Notes    []string `json:"notes,omitempty"`
	Children []*Node  `json:"children,omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (n *Node) MarshalJSON() ([]byte, error) {
	if n == nil {
		return []byte("null"), nil
	}
	return json.Marshal(nodeJSON{
		Op:       n.Op,
		On:       n.On,
		Detail:   n.Detail,
		EstRows:  optInt(n.EstRows),
		ActRows:  optInt(n.ActRows),
		Cost:     optFloat(n.Cost),
		TimeMS:   optMillis(n.Time),
		Loops:    optInt(n.Loops),
		Notes:    n.Notes,
		Children: n.Children,
	})
}

type planJSON struct {
	Engine      string   `json:"engine"`
	Statement   string   `json:"statement"`
	Command     string   `json:"command"`
	Analyzed    bool     `json:"analyzed"`
	PlanningMS  *float64 `json:"planning_ms,omitempty"`
	ExecutionMS *float64 `json:"execution_ms,omitempty"`
	Hints       []Hint   `json:"hints,omitempty"`
	Plan        *Node    `json:"plan"`
}

// MarshalJSON implements json.Marshaler. The hints are included so that a script
// reading --json sees exactly what a person reading the text report sees.
func (p Plan) MarshalJSON() ([]byte, error) {
	return json.Marshal(planJSON{
		Engine:      p.Kind.String(),
		Statement:   p.Statement,
		Command:     p.Command,
		Analyzed:    p.Analyzed,
		PlanningMS:  optMillis(p.Planning),
		ExecutionMS: optMillis(p.Execution),
		Hints:       Hints(p),
		Plan:        p.Root,
	})
}

// optInt returns a pointer to v, or nil when the engine didn't report it.
func optInt(v int64) *int64 {
	if v < 0 {
		return nil
	}
	return &v
}

// optFloat returns a pointer to v, or nil when the engine didn't report it.
func optFloat(v float64) *float64 {
	if v < 0 {
		return nil
	}
	return &v
}

// optMillis renders a duration as milliseconds, or nil when it wasn't measured.
func optMillis(d time.Duration) *float64 {
	if d <= 0 {
		return nil
	}
	ms := float64(d) / float64(time.Millisecond)
	return &ms
}

// newNode starts a node with every measurement marked Unknown, so a parser only
// has to set what its engine actually reports.
func newNode(op string) *Node {
	return &Node{Op: op, EstRows: Unknown, ActRows: Unknown, Cost: Unknown, Loops: Unknown}
}

// note attaches a note once; parsers can call it without checking first.
func (n *Node) note(s string) {
	if !n.Has(s) {
		n.Notes = append(n.Notes, s)
	}
}

// joinDetail appends a detail fragment to an existing one, skipping empties.
func joinDetail(existing, add string) string {
	add = strings.TrimSpace(add)
	switch {
	case add == "":
		return existing
	case existing == "":
		return add
	default:
		return existing + ", " + add
	}
}

// millis converts a float count of milliseconds into a Duration.
func millis(v float64) time.Duration {
	if v <= 0 {
		return 0
	}
	return time.Duration(v * float64(time.Millisecond))
}
