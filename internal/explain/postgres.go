package explain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// pgPlan is one element of Postgres's `EXPLAIN (FORMAT JSON)` array. The array
// has one element per statement, and DBWiz explains one statement at a time.
type pgPlan struct {
	Plan          pgNode  `json:"Plan"`
	PlanningTime  float64 `json:"Planning Time"`
	ExecutionTime float64 `json:"Execution Time"`
}

// pgNode is a Postgres plan node. Only the fields DBWiz shows are declared;
// EXPLAIN emits many more and the rest are ignored, which is also what keeps this
// working across server versions.
type pgNode struct {
	NodeType     string `json:"Node Type"`
	JoinType     string `json:"Join Type"`
	RelationName string `json:"Relation Name"`
	IndexName    string `json:"Index Name"`
	CTEName      string `json:"CTE Name"`
	FunctionName string `json:"Function Name"`
	SubplanName  string `json:"Subplan Name"`

	TotalCost float64 `json:"Total Cost"`
	PlanRows  float64 `json:"Plan Rows"`

	// Postgres 18 reports Actual Rows with decimals, earlier versions as an
	// integer; float64 reads both.
	ActualRows      float64 `json:"Actual Rows"`
	ActualLoops     float64 `json:"Actual Loops"`
	ActualTotalTime float64 `json:"Actual Total Time"`

	Filter      string   `json:"Filter"`
	IndexCond   string   `json:"Index Cond"`
	HashCond    string   `json:"Hash Cond"`
	JoinFilter  string   `json:"Join Filter"`
	RecheckCond string   `json:"Recheck Cond"`
	SortKey     []string `json:"Sort Key"`
	GroupKey    []string `json:"Group Key"`

	SortMethod    string `json:"Sort Method"`
	SortSpaceType string `json:"Sort Space Type"`

	Plans []pgNode `json:"Plans"`
}

// parsePostgres fills the plan from `EXPLAIN (FORMAT JSON)` output.
func parsePostgres(p *Plan) error {
	var docs []pgPlan
	if err := json.Unmarshal([]byte(p.Raw), &docs); err != nil {
		return fmt.Errorf("reading the plan Postgres returned: %w", err)
	}
	if len(docs) == 0 {
		return errors.New("Postgres returned an empty plan")
	}
	doc := docs[0]
	p.Root = pgConvert(doc.Plan, p.Analyzed)
	p.Planning = millis(doc.PlanningTime)
	p.Execution = millis(doc.ExecutionTime)
	return nil
}

// pgConvert turns one Postgres node (and its children) into the neutral model.
func pgConvert(src pgNode, analyzed bool) *Node {
	n := newNode(pgOp(src))
	n.On = pgRelation(src)
	n.Detail = pgDetail(src)

	n.EstRows = int64(src.PlanRows)
	if src.TotalCost > 0 {
		n.Cost = src.TotalCost
	}
	if analyzed {
		n.ActRows = int64(src.ActualRows)
		n.Time = millis(src.ActualTotalTime)
		if src.ActualLoops > 0 {
			n.Loops = int64(src.ActualLoops)
		}
	}

	// A sequential scan reads every row of the relation; that is the fact hints
	// are built on, so it is recorded as a note rather than re-derived from Op.
	if src.NodeType == "Seq Scan" {
		n.note(NoteFullScan)
	}
	if strings.Contains(strings.ToLower(src.SortMethod), "external") || src.SortSpaceType == "Disk" {
		n.note(NoteDiskSort)
	}

	for _, child := range src.Plans {
		n.Children = append(n.Children, pgConvert(child, analyzed))
	}
	return n
}

// pgOp names the step. A non-inner join carries its join type, because "Left Join"
// and "Join" behave differently enough that the distinction is worth the word.
func pgOp(src pgNode) string {
	op := src.NodeType
	if op == "" {
		op = "Step"
	}
	if src.JoinType != "" && src.JoinType != "Inner" && !strings.Contains(op, src.JoinType) {
		op = src.JoinType + " " + op
	}
	return op
}

// pgRelation is what the step operates on, in the order Postgres itself would
// name it.
func pgRelation(src pgNode) string {
	for _, name := range []string{src.RelationName, src.CTEName, src.FunctionName, src.SubplanName} {
		if name != "" {
			return name
		}
	}
	if src.IndexName != "" {
		return src.IndexName
	}
	return ""
}

// pgDetail is the one-line specific: which index, and the condition applied.
func pgDetail(src pgNode) string {
	detail := ""
	if src.IndexName != "" && src.RelationName != "" {
		detail = "using " + src.IndexName
	}
	for _, cond := range []string{src.IndexCond, src.RecheckCond, src.HashCond, src.JoinFilter, src.Filter} {
		if cond != "" {
			detail = joinDetail(detail, cond)
			break
		}
	}
	if len(src.SortKey) > 0 {
		detail = joinDetail(detail, "sort key: "+strings.Join(src.SortKey, ", "))
	}
	if len(src.GroupKey) > 0 {
		detail = joinDetail(detail, "group key: "+strings.Join(src.GroupKey, ", "))
	}
	if src.SortMethod != "" {
		detail = joinDetail(detail, src.SortMethod)
	}
	return detail
}
