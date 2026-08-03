package explain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// indentStep is one level of the tree. Four spaces plus an arrow is the shape
// MySQL's own tree output uses, and it reads the same in a terminal, a pipe and a
// pasted bug report.
const indentStep = "    "

// Render turns a plan into the text report. Like schema.Render it stays ASCII and
// unstyled: the output has to survive being piped to a file or pasted into a
// message, so structure is carried by indentation rather than colour.
func Render(p Plan) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Plan for: %s\n", singleLine(p.Statement))
	fmt.Fprintf(&b, "%s, %s\n\n", p.Kind, measurement(p))

	renderNode(&b, p.Root, 0)

	if summary := timingSummary(p); summary != "" {
		fmt.Fprintf(&b, "\n%s\n", summary)
	}

	hints := Hints(p)
	if len(hints) == 0 {
		return b.String()
	}
	b.WriteString("\nWhat stands out\n")
	for _, h := range hints {
		fmt.Fprintf(&b, "  ! %s: %s\n", h.Step, h.Issue)
		fmt.Fprintf(&b, "    %s\n", h.Advice)
	}
	return b.String()
}

// measurement says which kind of numbers the reader is looking at. Estimates and
// measurements look identical on the page and mean completely different things,
// so the distinction goes in the header rather than a footnote.
func measurement(p Plan) string {
	if p.Analyzed {
		return "measured (the statement was run)"
	}
	return "estimates only (nothing was executed)"
}

// renderNode writes one step and everything below it.
func renderNode(b *strings.Builder, n *Node, depth int) {
	if n == nil {
		return
	}
	indent := strings.Repeat(indentStep, depth)
	fmt.Fprintf(b, "%s-> %s%s\n", indent, n.Label(), nodeStats(n))
	if n.Detail != "" {
		fmt.Fprintf(b, "%s   %s\n", indent, singleLine(n.Detail))
	}
	for _, child := range n.Children {
		renderNode(b, child, depth+1)
	}
}

// nodeStats is the parenthesised measurement group after a step's label, holding
// only what the engine actually reported.
func nodeStats(n *Node) string {
	var parts []string
	if n.EstRows >= 0 {
		parts = append(parts, "est "+rowCount(n.EstRows))
	}
	if n.Cost >= 0 {
		parts = append(parts, "cost "+strconv.FormatFloat(n.Cost, 'f', 2, 64))
	}
	if n.ActRows >= 0 {
		actual := "actual " + rowCount(n.ActRows)
		if n.Time > 0 {
			actual += " in " + formatDuration(n.Time)
		}
		parts = append(parts, actual)
	}
	if n.Loops > 1 {
		parts = append(parts, fmt.Sprintf("%s loops", humanInt(n.Loops)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  (" + strings.Join(parts, ", ") + ")"
}

// timingSummary is the whole-statement footer, present only where the engine
// reported it.
func timingSummary(p Plan) string {
	var parts []string
	if p.Planning > 0 {
		parts = append(parts, "planning "+formatDuration(p.Planning))
	}
	if p.Execution > 0 {
		parts = append(parts, "execution "+formatDuration(p.Execution))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Total: " + strings.Join(parts, ", ")
}

// rowCount renders a row count with its unit, singular where it should be.
func rowCount(n int64) string {
	if n == 1 {
		return "1 row"
	}
	return humanInt(n) + " rows"
}

// humanInt groups a number in threes, so a plan's big numbers can be compared at
// a glance instead of counting digits.
func humanInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	if len(s) <= 3 {
		return sign + s
	}
	var out []byte
	for i, digit := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digit)
	}
	return sign + string(out)
}

// formatDuration renders a measured time in milliseconds, which is the unit every
// engine reports plans in.
func formatDuration(d time.Duration) string {
	ms := math.Round(float64(d)/float64(time.Millisecond)*1000) / 1000
	return strconv.FormatFloat(ms, 'f', -1, 64) + " ms"
}

// singleLine flattens a statement or a condition onto one line, so a multi-line
// query doesn't break the report's shape.
func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
