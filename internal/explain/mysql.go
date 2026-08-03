package explain

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// parseMySQL fills the plan from whichever shape MySQL or MariaDB answered in.
// MySQL's measured plan (`EXPLAIN ANALYZE`) is an indented text tree, not JSON,
// so the raw output decides the parser rather than the flag: MariaDB's ANALYZE
// stays JSON, and both are handled without asking which server this is.
func parseMySQL(p *Plan) error {
	raw := strings.TrimSpace(p.Raw)
	if strings.HasPrefix(raw, "{") {
		root, err := parseMySQLJSON(raw)
		if err != nil {
			return err
		}
		p.Root = root
		return nil
	}
	root, err := parseMySQLTree(raw)
	if err != nil {
		return err
	}
	p.Root = root
	return nil
}

// --- FORMAT=JSON ---

// mysqlChildKeys are the sub-structure keys walked out of a query block, in a
// fixed order so that a plan renders the same way every time.
var mysqlChildKeys = []string{
	"table",
	"nested_loop",
	"ordering_operation",
	"grouping_operation",
	"duplicates_removal",
	"materialized_from_subquery",
	"union_result",
	"query_specifications",
	"attached_subqueries",
	"subqueries",
}

// mysqlOps names the block-shaped keys the way a person would read them.
var mysqlOps = map[string]string{
	"query_block":                "Query block",
	"ordering_operation":         "Ordering",
	"grouping_operation":         "Grouping",
	"duplicates_removal":         "Duplicate removal",
	"materialized_from_subquery": "Materialised subquery",
	"union_result":               "Union",
}

// parseMySQLJSON reads `EXPLAIN FORMAT=JSON` (MySQL) or `ANALYZE FORMAT=JSON`
// (MariaDB, which adds the measured r_* fields to the same shape).
func parseMySQLJSON(raw string) (*Node, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, errors.New("reading the plan the server returned: " + err.Error())
	}
	block, ok := doc["query_block"].(map[string]any)
	if !ok {
		return nil, errors.New("the server's plan had no query block in it")
	}
	return mysqlBlock("Query block", block), nil
}

// mysqlBlock converts one block-shaped object (a query block, an ordering step,
// a grouping step) and everything nested inside it.
func mysqlBlock(op string, m map[string]any) *Node {
	n := newNode(op)
	if cost, ok := mysqlCost(m); ok {
		n.Cost = cost
	}
	// MySQL flags a sort it had to do itself, and an intermediate table it had to
	// materialise, on the operation rather than on the table.
	if b, _ := m["using_filesort"].(bool); b {
		n.note(NoteFilesort)
	}
	if b, _ := m["using_temporary_table"].(bool); b {
		n.note(NoteTempTable)
	}
	n.Children = mysqlChildren(m)
	return n
}

// mysqlChildren walks the recognised sub-structures of a block. Unknown keys are
// skipped rather than guessed at: a plan with a step DBWiz doesn't model still
// renders, just without that step.
func mysqlChildren(m map[string]any) []*Node {
	var out []*Node
	for _, key := range mysqlChildKeys {
		v, ok := m[key]
		if !ok {
			continue
		}
		switch key {
		case "table":
			if t, ok := v.(map[string]any); ok {
				out = append(out, mysqlTable(t))
			}
		case "nested_loop", "query_specifications", "attached_subqueries", "subqueries":
			// Arrays of single-key wrappers ({"table": …} or {"query_block": …}).
			items, _ := v.([]any)
			for _, item := range items {
				im, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if qb, ok := im["query_block"].(map[string]any); ok {
					out = append(out, mysqlBlock("Query block", qb))
					continue
				}
				out = append(out, mysqlChildren(im)...)
			}
		default:
			if sm, ok := v.(map[string]any); ok {
				out = append(out, mysqlBlock(mysqlOps[key], sm))
			}
		}
	}
	return out
}

// mysqlAccessOps translates MySQL's access_type into words that say what actually
// happens. "ALL" in particular reads as harmless and means the opposite.
var mysqlAccessOps = map[string]string{
	"ALL":             "Full scan",
	"index":           "Full index scan",
	"range":           "Index range scan",
	"ref":             "Index lookup",
	"eq_ref":          "Unique index lookup",
	"ref_or_null":     "Index lookup",
	"const":           "Constant row",
	"system":          "Constant row",
	"index_merge":     "Index merge",
	"fulltext":        "Fulltext search",
	"unique_subquery": "Unique subquery",
	"index_subquery":  "Index subquery",
}

// mysqlTable converts a table access node. MySQL and MariaDB spell the estimated
// row count differently (rows_examined_per_scan vs rows), so both are read.
func mysqlTable(m map[string]any) *Node {
	access, _ := m["access_type"].(string)
	op, ok := mysqlAccessOps[access]
	if !ok {
		op = "Table access"
	}
	n := newNode(op)
	n.On, _ = m["table_name"].(string)

	if access == "ALL" {
		n.note(NoteFullScan)
	}
	if key, _ := m["key"].(string); key != "" {
		n.Detail = "using " + key
	}
	if cond, _ := m["attached_condition"].(string); cond != "" {
		n.Detail = joinDetail(n.Detail, cond)
	}
	for _, field := range []string{"rows_examined_per_scan", "rows"} {
		if v, ok := m[field]; ok {
			n.EstRows = mysqlInt(v)
			break
		}
	}
	// MariaDB's ANALYZE adds the measured counterparts to the same object.
	if v, ok := m["r_rows"]; ok {
		n.ActRows = mysqlInt(v)
	}
	if v, ok := mysqlFloat(m["r_total_time_ms"]); ok {
		n.Time = millis(v)
	}
	if cost, ok := mysqlCost(m); ok {
		n.Cost = cost
	}
	return n
}

// mysqlCost reads cost_info.query_cost / cost_info.read_cost, which MySQL emits
// as a quoted number.
func mysqlCost(m map[string]any) (float64, bool) {
	info, ok := m["cost_info"].(map[string]any)
	if !ok {
		return 0, false
	}
	for _, field := range []string{"query_cost", "prefix_cost", "read_cost"} {
		if v, ok := mysqlFloat(info[field]); ok {
			return v, true
		}
	}
	return 0, false
}

// mysqlInt reads a JSON number that may have arrived as a quoted string.
func mysqlInt(v any) int64 {
	if f, ok := mysqlFloat(v); ok {
		return int64(f)
	}
	return Unknown
}

// mysqlFloat reads a JSON number or a numeric string.
func mysqlFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// --- EXPLAIN ANALYZE (tree text) ---

// MySQL's measured plan looks like:
//
//	-> Filter: (users.age > 30)  (cost=1.05 rows=3) (actual time=0.04..0.05 rows=2 loops=1)
//	    -> Table scan on users  (cost=1.05 rows=8) (actual time=0.03..0.04 rows=8 loops=1)
//
// Indentation is the tree, and the two parenthesised groups carry the estimate
// and the measurement.
var (
	mysqlEstRe    = regexp.MustCompile(`\(cost=([0-9.eE+-]+) rows=([0-9.eE+-]+)\)`)
	mysqlActualRe = regexp.MustCompile(`\(actual time=([0-9.]+)\.\.([0-9.]+) rows=([0-9.]+) loops=([0-9]+)\)`)
	mysqlNeverRe  = regexp.MustCompile(`\(never executed\)`)
)

// parseMySQLTree reads the indented text tree from EXPLAIN ANALYZE.
func parseMySQLTree(raw string) (*Node, error) {
	type frame struct {
		indent int
		node   *Node
	}
	var (
		stack []frame
		root  *Node
	)
	for line := range strings.SplitSeq(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		node := mysqlTreeNode(strings.TrimPrefix(strings.TrimSpace(line), "-> "))

		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		switch {
		case len(stack) > 0:
			parent := stack[len(stack)-1].node
			parent.Children = append(parent.Children, node)
		case root == nil:
			root = node
		default:
			// A second top-level line (a second statement's plan) hangs off the
			// first rather than being dropped.
			root.Children = append(root.Children, node)
		}
		stack = append(stack, frame{indent: indent, node: node})
	}
	if root == nil {
		return nil, errors.New("the server's measured plan had no steps in it")
	}
	return root, nil
}

// mysqlTreeNode parses one line of the text tree: the label, then the estimate
// and measurement groups.
func mysqlTreeNode(text string) *Node {
	n := newNode("")

	if m := mysqlEstRe.FindStringSubmatch(text); m != nil {
		if cost, err := strconv.ParseFloat(m[1], 64); err == nil {
			n.Cost = cost
		}
		if rows, err := strconv.ParseFloat(m[2], 64); err == nil {
			n.EstRows = int64(rows)
		}
		text = strings.Replace(text, m[0], "", 1)
	}
	if m := mysqlActualRe.FindStringSubmatch(text); m != nil {
		// The two times are per-loop first-row and last-row; the last-row time is
		// what "how long did this step take" means, with Loops kept alongside so a
		// step run a thousand times still reads honestly.
		if ms, err := strconv.ParseFloat(m[2], 64); err == nil {
			n.Time = millis(ms)
		}
		if rows, err := strconv.ParseFloat(m[3], 64); err == nil {
			n.ActRows = int64(rows)
		}
		if loops, err := strconv.ParseInt(m[4], 10, 64); err == nil {
			n.Loops = loops
		}
		text = strings.Replace(text, m[0], "", 1)
	}
	if m := mysqlNeverRe.FindString(text); m != "" {
		n.ActRows = 0
		n.Loops = 0
		text = strings.Replace(text, m, "", 1)
	}

	label := strings.TrimSpace(text)
	n.Op, n.On, n.Detail = mysqlSplitLabel(label)
	if strings.HasPrefix(strings.ToLower(n.Op), "table scan") {
		n.note(NoteFullScan)
	}
	if strings.Contains(strings.ToLower(label), "temporary table") {
		n.note(NoteTempTable)
	}
	return n
}

// mysqlSplitLabel breaks "Index lookup on u using PRIMARY (id=1)" into its
// operation, relation and detail, and "Filter: (x > 1)" into an operation and
// its condition.
func mysqlSplitLabel(label string) (op, on, detail string) {
	if i := strings.Index(label, ": "); i > 0 && !strings.Contains(label[:i], " on ") {
		return label[:i], "", strings.TrimSpace(label[i+2:])
	}
	if i := strings.Index(label, " on "); i > 0 {
		rest := strings.TrimSpace(label[i+4:])
		name, tail, _ := strings.Cut(rest, " ")
		return label[:i], name, strings.TrimSpace(tail)
	}
	if label == "" {
		return "Step", "", ""
	}
	return label, "", ""
}
