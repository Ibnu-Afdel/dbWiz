package explain

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Ibnu-Afdel/dbwiz/internal/db"
)

// parseSQLite fills the plan from `EXPLAIN QUERY PLAN`, which is the odd one out:
// not a document but a flat row set of (id, parent, notused, detail), where the
// tree lives in the parent pointers.
//
// SQLite reports no row counts, costs or timings at all — every measurement stays
// Unknown, and the shape of the plan is the whole signal.
func parseSQLite(p *Plan, res db.Result) error {
	if len(res.Rows) == 0 {
		return errors.New("SQLite returned no plan for this statement")
	}

	root := newNode("Query plan")
	byID := map[int64]*Node{0: root}

	var details []string
	for _, row := range res.Rows {
		if len(row) < 4 {
			continue
		}
		detail := cellText(row[3])
		details = append(details, detail)

		node := sqliteNode(detail)
		id, parent := cellInt(row[0]), cellInt(row[1])
		if id >= 0 {
			byID[id] = node
		}
		owner, ok := byID[parent]
		if !ok {
			owner = root
		}
		owner.Children = append(owner.Children, node)
	}

	// The detail column is the readable form of the plan, so it stands in for the
	// raw output the other engines hand back as text.
	p.Raw = strings.Join(details, "\n")

	switch len(root.Children) {
	case 0:
		return errors.New("SQLite returned no plan steps for this statement")
	case 1:
		// A single top-level step is the plan; the synthetic root would just be a
		// line of noise above it.
		p.Root = root.Children[0]
	default:
		p.Root = root
	}
	return nil
}

// sqliteNode reads one detail string. SQLite writes them as sentences:
//
//	SCAN users
//	SEARCH users USING INDEX idx_users_email (email=?)
//	USE TEMP B-TREE FOR ORDER BY
//
// SCAN means every row is read — whether or not an index is named, because
// "SCAN t USING COVERING INDEX i" still walks the whole index.
func sqliteNode(detail string) *Node {
	fields := strings.Fields(detail)
	if len(fields) == 0 {
		return newNode("Step")
	}

	switch fields[0] {
	case "SCAN", "SEARCH":
		op := "Full scan"
		if fields[0] == "SEARCH" {
			op = "Index search"
		}
		n := newNode(op)
		if len(fields) > 1 && fields[1] != "CONSTANT" {
			n.On = fields[1]
			n.Detail = strings.ToLower(strings.Join(fields[2:], " "))
		} else {
			n.Detail = strings.ToLower(strings.Join(fields[1:], " "))
		}
		if fields[0] == "SCAN" {
			n.note(NoteFullScan)
		}
		return n
	default:
		n := newNode(detail)
		if strings.Contains(detail, "TEMP B-TREE") {
			n.Op = "Temporary b-tree"
			n.Detail = strings.ToLower(strings.TrimPrefix(detail, "USE TEMP B-TREE "))
			n.note(NoteTempTable)
		}
		return n
	}
}

// parseInt reads an integer that arrived as text, returning Unknown when it isn't
// one.
func parseInt(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return Unknown
	}
	return v
}
